package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/pranay/notetree/config"
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

type contextKey string

const notesPathKey contextKey = "notes_path"

func setupConfig(ctx context.Context) (context.Context, error) {
	notesPath, err := config.GetNotesPath()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, notesPathKey, notesPath), nil
}

func GetNotesPath(ctx context.Context) string {
	if v := ctx.Value(notesPathKey); v != nil {
		return v.(string)
	}
	return ""
}

func main() {
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	app := &cli.Command{
		Name:    "notetree",
		Version: "0.1.0",
		Usage:   "A simple CLI app for managing notes",
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			newCtx, err := setupConfig(ctx)
			return newCtx, err
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
