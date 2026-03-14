package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

type noteEntry struct {
	filename string
	tags     []string
	firstTag string
}

func getFirstTag(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	tag := tags[0]
	if idx := strings.Index(tag, "/"); idx != -1 {
		return tag[:idx]
	}
	return tag
}

func parseNoteLine(line string) noteEntry {
	line = strings.TrimSpace(line)
	if line == "" {
		return noteEntry{}
	}

	entry := noteEntry{}

	bracketIdx := strings.Index(line, "[")
	if bracketIdx != -1 && strings.HasSuffix(line, "]") {
		entry.filename = strings.TrimSpace(line[:bracketIdx])
		tagsStr := strings.TrimSuffix(line[bracketIdx+1:], "]")
		entry.tags = strings.Split(tagsStr, ",")
		entry.firstTag = getFirstTag(entry.tags)
	} else {
		entry.filename = line
		entry.firstTag = ""
	}

	return entry
}

func addNoteToMap(notesPath, filename string, tags []string) error {
	notesMapFile := filepath.Join(notesPath, "notes.map")

	var entries []noteEntry

	if _, err := os.Stat(notesMapFile); err == nil {
		data, err := os.ReadFile(notesMapFile)
		if err != nil {
			return fmt.Errorf("failed to read notes.map: %w", err)
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if line = strings.TrimSpace(line); line != "" {
				entries = append(entries, parseNoteLine(line))
			}
		}
	}

	newEntry := noteEntry{
		filename: filename,
		tags:     tags,
		firstTag: getFirstTag(tags),
	}
	entries = append(entries, newEntry)

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].firstTag != entries[j].firstTag {
			return entries[i].firstTag < entries[j].firstTag
		}
		return entries[i].filename < entries[j].filename
	})

	file, err := os.Create(notesMapFile)
	if err != nil {
		return fmt.Errorf("failed to open notes.map: %w", err)
	}
	defer file.Close()

	for _, entry := range entries {
		var line string
		if len(entry.tags) > 0 {
			line = fmt.Sprintf("%s [%s]\n", entry.filename, strings.Join(entry.tags, ","))
		} else {
			line = fmt.Sprintf("%s\n", entry.filename)
		}
		if _, err := file.WriteString(line); err != nil {
			return fmt.Errorf("failed to write to notes.map: %w", err)
		}
	}

	return nil
}

func promptForTags(reader *bufio.Reader) ([]string, error) {
	fmt.Print("Enter tags (comma-separated, or press Enter to skip): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return []string{}, nil
	}

	parts := strings.Split(input, ",")
	var tags []string
	for _, tag := range parts {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}

	return tags, nil
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

			fmt.Printf("Created note: %s\n\n\n", filepath.Base(filePath))

			if err := openEditor(filePath); err != nil {
				fmt.Printf("Failed to open editor: %v\n", err)
			}

			tags, err := promptForTags(reader)
			if err != nil {
				fmt.Printf("Failed to read tags: %v\n", err)
			}

			if err := addNoteToMap(notesPath, filename, tags); err != nil {
				fmt.Printf("Failed to add note to map: %v\n", err)
			}

			fmt.Println()
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

func hasTag(entryTags []string, targetTag string) bool {
	for _, tag := range entryTags {
		if tag == targetTag {
			return true
		}
	}
	return false
}

func readNotes(ctx context.Context, filterTag string) error {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	notesMapFile := filepath.Join(notesPath, "notes.map")

	data, err := os.ReadFile(notesMapFile)
	if err != nil {
		return fmt.Errorf("failed to read notes.map: %w", err)
	}

	var entries []noteEntry
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			entry := parseNoteLine(line)
			if entry.filename != "" {
				if filterTag == "" || hasTag(entry.tags, filterTag) {
					entries = append(entries, entry)
				}
			}
		}
	}

	if len(entries) == 0 {
		if filterTag != "" {
			fmt.Printf("No notes found with tag: %s\n", filterTag)
		} else {
			fmt.Println("No notes found.")
		}
		return nil
	}

	tmpFile, err := os.CreateTemp(notesDir, "combined_*.md")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	for _, entry := range entries {
		notePath := filepath.Join(notesDir, entry.filename)
		content, err := os.ReadFile(notePath)
		if err != nil {
			fmt.Printf("Warning: could not read %s: %v\n", entry.filename, err)
			continue
		}

		separator := fmt.Sprintf("\n\n--- %s ---\n\n", entry.filename)
		if _, err := tmpFile.WriteString(separator); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write to temporary file: %w", err)
		}
		if _, err := tmpFile.Write(content); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write to temporary file: %w", err)
		}
	}
	tmpFile.Close()

	markdownReader, err := config.GetMarkdownReader()
	if err != nil {
		return fmt.Errorf("failed to get markdown reader: %w", err)
	}

	cmd := exec.Command(markdownReader, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run markdown reader: %w", err)
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
				Name:    "image",
				Aliases: []string{"img"},
				Usage:   "Add images to the images folder",
				Action: func(ctx context.Context, c *cli.Command) error {
					return addImages(ctx)
				},
			},
			{
				Name:    "read",
				Usage:   "Read and concatenate notes",
				ArgsUsage: "[tag]",
				Description: "Concatenates all notes into a temporary file and displays them.\nIf a tag is provided, only notes with that tag are included.",
				Action: func(ctx context.Context, c *cli.Command) error {
					filterTag := c.Args().First()
					return readNotes(ctx, filterTag)
				},
			},
		},
	}

	err := app.Run(context.Background(), os.Args);
	crash(err, "Error running notetree")
}
