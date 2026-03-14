package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

func crash(err error, text string) {
	if err != nil {
		if text != "" {
			err = fmt.Errorf("error: %q:\n %w", text, err)
		}
		log.Fatal(err)
	}
}

func ensureConfigFile() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "notetree")
	configFile := filepath.Join(configDir, "notetree.conf")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		f, err := os.Create(configFile)
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
		defer f.Close()

		fmt.Printf("Created config file at %s\n", configFile)
	}

	return nil
}

func main() {
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	app := &cli.Command{
		Name:    "notetree",
		Version: "0.1.0",
		Usage:   "A simple CLI app for managing notes",
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			return ctx, ensureConfigFile()
		},
		Commands: []*cli.Command{
			{
				Name:    "add",
				Aliases: []string{"a"},
				Usage:   "Add a new note",
				Action: func(ctx context.Context, c *cli.Command) error {
					fmt.Println("Adding a new note...")
					return nil
				},
			},
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "List all notes",
				Action: func(ctx context.Context, c *cli.Command) error {
					fmt.Println("Listing all notes...")
					return nil
				},
			},
		},
	}

	err := app.Run(context.Background(), os.Args); 
	crash(err, "Error running notetree")
}
