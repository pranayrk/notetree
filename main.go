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
	crash(err, "failed to get home directory")

	configDir := filepath.Join(homeDir, ".config", "notetree")
	configFile := filepath.Join(configDir, "notetree.conf")

	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		return
	}
	err := os.MkdirAll(configDir, 0755);
	crash(err, "failed to create config directory")

	f, err := os.Create(configFile)
	crash(err, "failed to create config file")
	defer f.Close()

	fmt.Printf("Created config file at %s\n", configFile)

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
