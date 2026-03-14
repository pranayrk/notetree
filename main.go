package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	if err := ensureNotesStructure(notesPath); err != nil {
		return ctx, err
	}

	return context.WithValue(ctx, notesPathKey, notesPath), nil
}

func ensureNotesStructure(notesPath string) error {
	notesDir := filepath.Join(notesPath, "notes")
	imagesDir := filepath.Join(notesPath, "images")
	notesMapFile := filepath.Join(notesPath, "notes.map")

	if err := os.MkdirAll(notesDir, 0755); err != nil {
		return fmt.Errorf("failed to create notes directory: %w", err)
	}

	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory: %w", err)
	}

	if _, err := os.Stat(notesMapFile); os.IsNotExist(err) {
		if err := os.WriteFile(notesMapFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to create notes.map file: %w", err)
		}
	}

	return nil
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

func addNoteToMap(notesPath, filename string) error {
	notesMapFile := filepath.Join(notesPath, "notes.map")

	file, err := os.OpenFile(notesMapFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open notes.map: %w", err)
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "%s\n", filename)
	if err != nil {
		return fmt.Errorf("failed to write to notes.map: %w", err)
	}

	return nil
}

func createNotesInteractive(ctx context.Context) error {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Create new note? (Enter to create, 'q' to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			filename := generateNoteFilename()
			filePath := filepath.Join(notesDir, filename)

			file, err := os.Create(filePath)
			if err != nil {
				fmt.Printf("Failed to create note file: %v\n", err)
				continue
			}
			file.Close()

			if err := addNoteToMap(notesPath, filename); err != nil {
				fmt.Printf("Failed to add note to map: %v\n", err)
			}

			fmt.Printf("Created note: %s\n\n\n", filepath.Base(filePath))

			if err := openEditor(filePath); err != nil {
				fmt.Printf("Failed to open editor: %v\n", err)
			}
			continue
		}

		if strings.ToLower(input) == "q" {
			fmt.Println("Exiting note add mode.")
			break
		}
	}

	return nil
}

func addImages(ctx context.Context) error {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	imagesDir := filepath.Join(notesPath, "images")

	fmt.Println("Interactive image add mode. Enter image paths one at a time.")
	fmt.Println("Press 'q' to quit.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Enter image path: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.ToLower(input) == "q" {
			fmt.Println("Exiting image add mode.")
			break
		}

		if _, err := os.Stat(input); os.IsNotExist(err) {
			fmt.Printf("Image file does not exist: %s\n", input)
			continue
		}

		filename := filepath.Base(input)
		destPath := filepath.Join(imagesDir, filename)

		if _, err := os.Stat(destPath); err == nil {
			timestamp := time.Now().Format("2006-01-02_15-04-05")
			ext := filepath.Ext(filename)
			name := filename[:len(filename)-len(ext)]
			filename = fmt.Sprintf("%s_%s%s", name, timestamp, ext)
			destPath = filepath.Join(imagesDir, filename)
		}

		if err := os.Rename(input, destPath); err != nil {
			fmt.Printf("Failed to move image: %v\n", err)
			continue
		}

		fmt.Printf("Added image: %s\n", destPath)
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
			return createNotesInteractive(ctx)
		},
		Commands: []*cli.Command{
			{
				Name:    "add",
				Aliases: []string{"a"},
				Usage:   "Add a new note",
				Action: func(ctx context.Context, c *cli.Command) error {
					return createNotesInteractive(ctx)
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
			{
				Name:    "image",
				Aliases: []string{"img"},
				Usage:   "Add images to the images folder",
				Action: func(ctx context.Context, c *cli.Command) error {
					return addImages(ctx)
				},
			},
		},
	}

	err := app.Run(context.Background(), os.Args);
	crash(err, "Error running notetree")
}
