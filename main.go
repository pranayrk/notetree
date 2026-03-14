package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

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

func generateNoteFilename() string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	return fmt.Sprintf("note_%s.md", timestamp)
}

func openEditor(filePath string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	cmd := exec.Command(editor, filePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createNote(ctx context.Context) error {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		return fmt.Errorf("failed to create notes directory: %w", err)
	}

	filename := generateNoteFilename()
	filePath := filepath.Join(notesDir, filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create note file: %w", err)
	}
	file.Close()

	fmt.Printf("Created note: %s\n", filePath)

	if err := openEditor(filePath); err != nil {
		return fmt.Errorf("failed to open editor: %w", err)
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
			newCtx, err := setupConfig(ctx)
			return newCtx, err
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return createNote(ctx)
		},
		Commands: []*cli.Command{
			{
				Name:    "add",
				Aliases: []string{"a"},
				Usage:   "Add a new note",
				Action: func(ctx context.Context, c *cli.Command) error {
					return createNote(ctx)
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
