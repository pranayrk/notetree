package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	configFileName    = "notetree.conf"
	notesPathKey      = "notes_path"
	markdownReaderKey = "markdown_reader"
	mapFileKey        = "map_file"
)

// Config holds the application configuration
type Config struct {
	NotesPath      string
	MarkdownReader string
	MapFile        string
}

// getConfigPath returns the path to the config file
func getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	configDir := filepath.Join(homeDir, ".config", "notetree")
	return filepath.Join(configDir, configFileName), nil
}

// ensureConfigDir creates the config directory if it doesn't exist
func ensureConfigDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	configDir := filepath.Join(homeDir, ".config", "notetree")
	return os.MkdirAll(configDir, 0755)
}

// Load reads the config file and returns a Config struct
func Load() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	config := &Config{}

	file, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist, return empty config
			return config, nil
		}
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == notesPathKey {
			config.NotesPath = value
		} else if key == markdownReaderKey {
			config.MarkdownReader = value
		} else if key == mapFileKey {
			config.MapFile = value
		}
	}

	return config, scanner.Err()
}

// Save writes the config to the config file
func (c *Config) Save() error {
	if err := ensureConfigDir(); err != nil {
		return err
	}

	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	if c.NotesPath != "" {
		_, err = fmt.Fprintf(file, "%s=%s\n", notesPathKey, c.NotesPath)
		if err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	if c.MarkdownReader != "" {
		_, err = fmt.Fprintf(file, "%s=%s\n", markdownReaderKey, c.MarkdownReader)
		if err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	if c.MapFile != "" {
		_, err = fmt.Fprintf(file, "%s=%s\n", mapFileKey, c.MapFile)
		if err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	return nil
}

// GetNotesPath returns the notes path, prompting the user if not configured
func GetNotesPath() (string, error) {
	config, err := Load()
	if err != nil {
		return "", err
	}

	if config.NotesPath != "" {
		return config.NotesPath, nil
	}

	// Notes path not configured, prompt user
	fmt.Println("Notes path not configured.")
	fmt.Print("Enter the directory path where notes should be stored: ")

	reader := bufio.NewReader(os.Stdin)
	notesPath, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	notesPath = strings.TrimSpace(notesPath)
	if notesPath == "" {
		return "", fmt.Errorf("notes path cannot be empty")
	}

	// Expand tilde if present
	if strings.HasPrefix(notesPath, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		notesPath = filepath.Join(homeDir, notesPath[1:])
	}

	// Check if directory exists, offer to create if not
	if _, err := os.Stat(notesPath); os.IsNotExist(err) {
		fmt.Printf("Directory %s does not exist. Create it? (y/n): ", notesPath)
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response == "y" || response == "yes" {
			if err := os.MkdirAll(notesPath, 0755); err != nil {
				return "", fmt.Errorf("failed to create notes directory: %w", err)
			}
			fmt.Printf("Created directory: %s\n", notesPath)
		} else {
			return "", fmt.Errorf("notes directory not created")
		}
	}

	// Save the notes path to config
	config.NotesPath = notesPath
	if err := config.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Notes path configured: %s\n", notesPath)

	return notesPath, nil
}

// GetMarkdownReader returns the markdown reader command, prompting the user if not configured
func GetMarkdownReader() (string, error) {
	config, err := Load()
	if err != nil {
		return "", err
	}

	if config.MarkdownReader != "" {
		return config.MarkdownReader, nil
	}

	// Markdown reader not configured, prompt user
	fmt.Println("Markdown reader not configured.")
	fmt.Print("Enter the CLI command to open markdown files (e.g., 'glow', 'mdv', 'cat'): ")

	reader := bufio.NewReader(os.Stdin)
	markdownReader, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	markdownReader = strings.TrimSpace(markdownReader)
	if markdownReader == "" {
		return "", fmt.Errorf("markdown reader command cannot be empty")
	}

	// Save the markdown reader to config
	config.MarkdownReader = markdownReader
	if err := config.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Markdown reader configured: %s\n", markdownReader)

	return markdownReader, nil
}

// GetMapFile returns the map file name, prompting the user if not configured
func GetMapFile(notesPath string) (string, error) {
	config, err := Load()
	if err != nil {
		return "", err
	}

	if config.MapFile != "" {
		return config.MapFile, nil
	}

	// Map file not configured, prompt user
	fmt.Println("Map file not configured.")
	fmt.Print("Enter the map file name (default: notes.map): ")

	reader := bufio.NewReader(os.Stdin)
	mapFile, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	mapFile = strings.TrimSpace(mapFile)
	if mapFile == "" {
		mapFile = "notes.map"
	}

	// Save the map file to config
	config.MapFile = mapFile
	if err := config.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Map file configured: %s\n", mapFile)

	return mapFile, nil
}

// ListMapFiles returns a list of existing map files in the notes directory
func ListMapFiles(notesPath string) ([]string, error) {
	pattern := filepath.Join(notesPath, "*.map")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list map files: %w", err)
	}

	var mapFiles []string
	for _, match := range matches {
		mapFiles = append(mapFiles, filepath.Base(match))
	}

	return mapFiles, nil
}

// SetMapFile sets the map file in the config
func SetMapFile(mapFile string) error {
	config, err := Load()
	if err != nil {
		return err
	}

	config.MapFile = mapFile
	return config.Save()
}
