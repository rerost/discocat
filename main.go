package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	configure  bool
	helpFlag   bool
	version    bool
	username   string
	channel    string
	files      []string
	webhookURL string
)

const (
	Version         = "0.1.0"
	ConfigFileName  = ".discocat_config"
	DefaultFileName = "no_name"
)

type Config struct {
	WebhookURL string `json:"webhook_url"`
	Username   string `json:"username"`
}

func init() {
	flag.BoolVar(&configure, "configure", false, "Configure settings")
	flag.BoolVar(&helpFlag, "help", false, "Display help information")
	flag.BoolVar(&helpFlag, "h", false, "Display help information")
	flag.BoolVar(&version, "version", false, "Display version information")
	flag.BoolVar(&version, "V", false, "Display version information")

	flag.StringVar(&username, "username", "", "Set the username")
	flag.StringVar(&channel, "channel", "", "Set the channel (not applicable for webhooks)")
	flag.StringVar(&channel, "c", "", "Set the channel (shorthand, not applicable for webhooks)")
	flag.Func("file", "Specify the file to send. --file=hoge.txt or --file=foo.txt --file=bar.txt", func(v string) error {
		files = append(files, v)
		return nil
	})
	flag.Func("f", "Specify the file to send (short hand). --file=hoge.txt or --file=foo.txt --file=bar.txt", func(v string) error {
		files = append(files, v)
		return nil
	})
	flag.StringVar(&webhookURL, "webhook", "", "Specify the webhook URL")
}

func main() {
	flag.Parse()

	if helpFlag {
		usage()
		return
	}

	if version {
		fmt.Printf("discocat %s\n", Version)
		return
	}

	// Get the path to the configuration file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: Unable to determine the home directory:", err)
		os.Exit(1)
	}
	configPath := filepath.Join(homeDir, ConfigFileName)

	if configure {
		// Configuration mode
		if err := runConfigure(configPath); err != nil {
			fmt.Fprintln(os.Stderr, "Error during configuration:", err)
			os.Exit(1)
		}
		fmt.Println("Configuration saved:", configPath)
		return
	}

	// Load configuration file
	config, err := loadConfig(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "Error: Failed to load the configuration file:", err)
		os.Exit(1)
	}

	// Use the webhook URL specified in the command line options or the configuration file
	if webhookURL == "" {
		webhookURL = config.WebhookURL
	}

	if webhookURL == "" {
		fmt.Fprintln(os.Stderr, "Error: Webhook URL is not specified. Use the --webhook option or run --configure.")
		os.Exit(1)
	}

	// Use the username specified in the command line options or the configuration file
	if username == "" {
		username = config.Username
	}

	// Prepare the payload and send the message
	if len(files) != 0 {
		err = sendFile(webhookURL, files, username)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	} else {
		// Read content from stdin
		content, err := getContent()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		// Send the message content
		err = sendMessage(webhookURL, content, username)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}

	fmt.Println("Notification sent successfully.")
}

func runConfigure(configPath string) error {
	config := Config{}
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter the Webhook URL: ")
	webhookInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read the webhook URL: %w", err)
	}
	config.WebhookURL = strings.TrimSpace(webhookInput)

	// Validate the webhook URL
	if config.WebhookURL == "" {
		return errors.New("webhook URL cannot be empty")
	}

	// Save the configuration
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the configuration: %w", err)
	}

	err = os.WriteFile(configPath, configData, 0600)
	if err != nil {
		return fmt.Errorf("failed to write the configuration file: %w", err)
	}

	return nil
}

func loadConfig(configPath string) (Config, error) {
	config := Config{}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(configData, &config)
	if err != nil {
		return config, fmt.Errorf("failed to parse the configuration file: %w", err)
	}
	return config, nil
}

func getContent() (string, error) {
	var content string
	// Read from standard input
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to get stdin information: %w", err)
	}
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Input from a pipe
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		content = string(data)
	} else {
		// No input provided
		return "", errors.New("message content is not specified. Provide input via stdin")
	}
	return content, nil
}

func sendMessage(webhookURL, content, username string) error {
	// Discord's maximum message length is 2000 characters
	maxLength := 2000
	contents := splitMessage(content, maxLength)

	for _, msgContent := range contents {
		message := map[string]interface{}{
			"content": msgContent,
		}

		if username != "" {
			message["username"] = username
		}

		messageBytes, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("failed to encode the message to JSON: %w", err)
		}

		req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(messageBytes))
		if err != nil {
			return fmt.Errorf("failed to create the HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send the HTTP request: %w", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "Error closing response body:", err)
			}
		}()

		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("received an error from Discord. Status Code: %d, Response Body: %s", resp.StatusCode, string(body))
		}

		// Optional: Add a short delay between messages to avoid hitting rate limits
		// time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func splitMessage(content string, maxLength int) []string {
	var contents []string
	for len(content) > maxLength {
		splitIndex := strings.LastIndex(content[:maxLength], "\n")
		if splitIndex == -1 {
			splitIndex = maxLength
		}
		contents = append(contents, strings.TrimSpace(content[:splitIndex]))
		content = content[splitIndex:]
	}
	if len(strings.TrimSpace(content)) > 0 {
		contents = append(contents, strings.TrimSpace(content))
	}
	return contents
}

func sendFile(webhookURL string, filePaths []string, username string) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	for i, filePath := range filePaths {
		_, filename := path.Split(filePath)
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open the file: %w", err)
		}
		defer func() {
			if err := file.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "Error closing file:", err)
			}
		}()

		// Add the file part
		fw, err := w.CreateFormFile(
			fmt.Sprintf("file[%d]", i),
			filename,
		)
		if err != nil {
			return fmt.Errorf("failed to create form file: %w", err)
		}
		if _, err = io.Copy(fw, file); err != nil {
			return fmt.Errorf("failed to copy file content: %w", err)
		}
	}

	// Add the payload part
	payload := map[string]interface{}{}
	if username != "" {
		payload["username"] = username
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode payload to JSON: %w", err)
	}
	fw, err := w.CreateFormField("payload_json")
	if err != nil {
		return fmt.Errorf("failed to create payload field: %w", err)
	}
	if _, err = fw.Write(payloadBytes); err != nil {
		return fmt.Errorf("failed to write payload: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", webhookURL, &b)
	if err != nil {
		return fmt.Errorf("failed to create the HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send the HTTP request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "Error closing response body:", err)
		}
	}()

	// Accept both 200 and 204 as success
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("received an error from Discord. Status Code: %d, Response Body: %s", resp.StatusCode, string(body))
	}

	return nil
}

func usage() {
	fmt.Printf("discocat %s\n\n", Version)
	fmt.Println("USAGE:")
	fmt.Println("    discocat [FLAGS] [OPTIONS]")
	fmt.Println("\nFLAGS:")
	fmt.Println("        --configure        Configure settings")
	fmt.Println("    -h, --help             Display help information")
	fmt.Println("    -V, --version          Display version information")
	fmt.Println("\nOPTIONS:")
	fmt.Println("        --username <username>       Set the username")
	fmt.Println("    -c, --channel <channel>         Set the channel (not applicable for webhooks)")
	fmt.Println("    -f, --file <file>               Specify the file to send")
	fmt.Println("        --webhook <webhook_url>     Specify the webhook URL")
}
