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

const (
	notesPathKey contextKey = "notes_path"
	mapFileKey   contextKey = "map_file"
	appVersion   string     = "0.1.0"
)

func setupConfig(ctx context.Context) (context.Context, error) {
	notesPath, err := config.GetNotesPath()
	if err != nil {
		return ctx, err
	}

	mapFile, err := config.GetMapFile(notesPath)
	if err != nil {
		return ctx, err
	}

	if err := ensureNotesStructure(notesPath, mapFile); err != nil {
		return ctx, err
	}

	ctx = context.WithValue(ctx, notesPathKey, notesPath)
	ctx = context.WithValue(ctx, mapFileKey, mapFile)
	return ctx, nil
}

func ensureNotesStructure(notesPath string, mapFile string) error {
	notesDir := filepath.Join(notesPath, "notes")
	imagesDir := filepath.Join(notesPath, "images")
	notesMapFile := filepath.Join(notesPath, mapFile)

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

func GetMapFile(ctx context.Context) string {
	if v := ctx.Value(mapFileKey); v != nil {
		return v.(string)
	}
	return ""
}

func generateNoteFilename() string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	return fmt.Sprintf("%s.md", timestamp)
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

func expandNestedTags(tag string) []string {
	var tags []string
	parts := strings.Split(tag, "/")
	
	for i := range parts {
		nestedTag := strings.Join(parts[:i+1], "/")
		tags = append(tags, nestedTag)
	}
	
	return tags
}

func addNoteToMap(notesPath, mapFile, filename string, tags []string) error {
	notesMapFile := filepath.Join(notesPath, mapFile)

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
		fmt.Print("Create new note? (Enter to create, 'Q' to quit): ")
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

			// Check if file is empty after editing
			info, err := os.Stat(filePath)
			if err != nil {
				fmt.Printf("Failed to stat file: %v\n", err)
				continue
			}

			if info.Size() == 0 {
				os.Remove(filePath)
				fmt.Println("Note is empty, deleted.")
				fmt.Println()
				continue
			}

			tags, err := promptForTags(reader)
			if err != nil {
				fmt.Printf("Failed to read tags: %v\n", err)
			}

			mapFile := GetMapFile(ctx)
			if err := addNoteToMap(notesPath, mapFile, filename, tags); err != nil {
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

func listNotes(notesPath, mapFile string) ([]noteEntry, error) {
	notesMapFile := filepath.Join(notesPath, mapFile)

	data, err := os.ReadFile(notesMapFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []noteEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read notes.map: %w", err)
	}

	var entries []noteEntry
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			entry := parseNoteLine(line)
			if entry.filename != "" {
				entries = append(entries, entry)
			}
		}
	}

	return entries, nil
}

func updateNoteTags(notesPath, mapFile, filename string, tags []string) error {
	notesMapFile := filepath.Join(notesPath, mapFile)

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
				if entry.filename == filename {
					entry.tags = tags
					entry.firstTag = getFirstTag(tags)
				}
				entries = append(entries, entry)
			}
		}
	}

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

func editNotesInteractive(ctx context.Context) error {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	mapFile := GetMapFile(ctx)
	notesDir := filepath.Join(notesPath, "notes")

	entries, err := listNotes(notesPath, mapFile)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No notes found.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Interactive note edit mode. Enter note filename to edit.")
	fmt.Println("Press 'Q' to quit.")
	fmt.Println()

	for {
		fmt.Print("Enter note filename to edit (or 'Q' to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.ToLower(input) == "q" {
			fmt.Println("Exiting note edit mode.")
			break
		}

		// Find the note
		var foundEntry *noteEntry
		for i := range entries {
			if entries[i].filename == input {
				foundEntry = &entries[i]
				break
			}
		}

		if foundEntry == nil {
			fmt.Printf("Note not found: %s\n", input)
			fmt.Println()
			continue
		}

		filePath := filepath.Join(notesDir, foundEntry.filename)

		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Printf("Note file does not exist: %s\n", foundEntry.filename)
			fmt.Println()
			continue
		}

		if err := openEditor(filePath); err != nil {
			fmt.Printf("Failed to open editor: %v\n", err)
		}

		// Check if file is empty after editing
		info, err := os.Stat(filePath)
		if err != nil {
			fmt.Printf("Failed to stat file: %v\n", err)
			continue
		}

		if info.Size() == 0 {
			os.Remove(filePath)
			// Remove from map
			notesMapFile := filepath.Join(notesPath, mapFile)
			data, err := os.ReadFile(notesMapFile)
			if err == nil {
				var newEntries []noteEntry
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					if line = strings.TrimSpace(line); line != "" {
						entry := parseNoteLine(line)
						if entry.filename != foundEntry.filename {
							newEntries = append(newEntries, entry)
						}
					}
				}
				file, err := os.Create(notesMapFile)
				if err == nil {
					defer file.Close()
					for _, entry := range newEntries {
						var line string
						if len(entry.tags) > 0 {
							line = fmt.Sprintf("%s [%s]\n", entry.filename, strings.Join(entry.tags, ","))
						} else {
							line = fmt.Sprintf("%s\n", entry.filename)
						}
						file.WriteString(line)
					}
				}
			}
			fmt.Println("Note is empty, deleted and removed from map.")
			// Refresh entries
			entries, _ = listNotes(notesPath, mapFile)
			fmt.Println()
			continue
		}

		// Prompt to edit tags
		currentTags := strings.Join(foundEntry.tags, ",")
		fmt.Printf("Current tags: %s\n", currentTags)
		fmt.Print("Enter new tags (comma-separated, or press Enter to keep current): ")
		tagsInput, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Failed to read tags: %v\n", err)
			continue
		}

		tagsInput = strings.TrimSpace(tagsInput)
		var newTags []string
		if tagsInput == "" {
			newTags = foundEntry.tags
		} else {
			parts := strings.Split(tagsInput, ",")
			for _, tag := range parts {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					expandedTags := expandNestedTags(tag)
					newTags = append(newTags, expandedTags...)
				}
			}
		}

		if err := updateNoteTags(notesPath, mapFile, foundEntry.filename, newTags); err != nil {
			fmt.Printf("Failed to update tags: %v\n", err)
		} else {
			fmt.Println("Tags updated.")
			// Refresh entries
			entries, _ = listNotes(notesPath, mapFile)
		}

		fmt.Println()
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
	fmt.Println("Press 'Q' to quit.")
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

func readNotes(ctx context.Context, filterTag string, includeFilenames bool) error {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	mapFile := GetMapFile(ctx)
	notesMapFile := filepath.Join(notesPath, mapFile)

	data, err := os.ReadFile(notesMapFile)
	if err != nil {
		return fmt.Errorf("failed to read notes.map: %w", err)
	}

	// Use LinkedHashMap to preserve insertion order by first tag
	taggedNotes := New[string, []noteEntry]()

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			entry := parseNoteLine(line)
			if entry.filename != "" {
				if filterTag == "" || hasTag(entry.tags, filterTag) {
					firstTag := entry.firstTag
					if firstTag == "" {
						firstTag = "_untagged"
					}
					existing, _ := taggedNotes.Get(firstTag)
					existing = append(existing, entry)
					taggedNotes.Put(firstTag, existing)
				}
			}
		}
	}

	if taggedNotes.Empty() {
		if filterTag != "" {
			fmt.Printf("No notes found with tag: %s\n", filterTag)
		} else {
			fmt.Println("No notes found.")
		}
		return nil
	}

	tmpFile, err := os.CreateTemp(notesPath, "temp_*.md")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Iterate through tags in insertion order
	for _, tag := range taggedNotes.Keys() {
		entries, _ := taggedNotes.Get(tag)
		for _, entry := range entries {
			notePath := filepath.Join(notesDir, entry.filename)
			content, err := os.ReadFile(notePath)
			if err != nil {
				fmt.Printf("Warning: could not read %s: %v\n", entry.filename, err)
				continue
			}

			if includeFilenames {
				separator := fmt.Sprintf("\n\n--- %s ---\n\n", entry.filename)
				if _, err := tmpFile.WriteString(separator); err != nil {
					tmpFile.Close()
					return fmt.Errorf("failed to write to temporary file: %w", err)
				}
			}
			if _, err := tmpFile.Write(content); err != nil {
				tmpFile.Close()
				return fmt.Errorf("failed to write to temporary file: %w", err)
			}
			// Add extra newline between files
			if _, err := tmpFile.WriteString("\n\n"); err != nil {
				tmpFile.Close()
				return fmt.Errorf("failed to write to temporary file: %w", err)
			}
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

func mainMenu(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		currentMapFile := GetMapFile(ctx)
		fmt.Printf("notetree version %s\n", appVersion)
		fmt.Printf("Current map file: \033[1m%s\033[0m\n", currentMapFile)
		fmt.Println("  (A)dd notes")
		fmt.Println("  (E)dit notes")
		fmt.Println("  (R)ead notes")
		fmt.Println("  (I)mage copy")
		fmt.Println("  (M)ap files")
		fmt.Println("  (Q)uit")
		fmt.Println()

		fmt.Print("Select option: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)

		switch strings.ToLower(input) {
		case "a":
			if err := createNotesInteractive(ctx); err != nil {
				fmt.Printf("Error in add mode: %v\n", err)
			}
		case "e":
			if err := editNotesInteractive(ctx); err != nil {
				fmt.Printf("Error in edit mode: %v\n", err)
			}
		case "r":
			fmt.Print("Enter tag to filter (or Enter to read all): ")
			tagInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else {
				filterTag := strings.TrimSpace(tagInput)
				if err := readNotes(ctx, filterTag, false); err != nil {
					fmt.Printf("Error reading notes: %v\n", err)
				}
			}
		case "i":
			if err := addImages(ctx); err != nil {
				fmt.Printf("Error adding images: %v\n", err)
			}
		case "m":
			newMapFile, err := manageMapFiles(ctx, reader)
			if err != nil {
				fmt.Printf("Error managing map files: %v\n", err)
			} else {
				// Update context with new map file
				ctx = context.WithValue(ctx, mapFileKey, newMapFile)
			}
		case "q":
			fmt.Println("Goodbye!")
			return nil
		default:
			fmt.Println("Invalid option. Please try again.")
		}

		fmt.Println()
	}
}

func manageMapFiles(ctx context.Context, reader *bufio.Reader) (string, error) {
	notesPath := GetNotesPath(ctx)
	if notesPath == "" {
		return "", fmt.Errorf("notes path not configured")
	}

	for {
		mapFiles, err := config.ListMapFiles(notesPath)
		if err != nil {
			return "", err
		}

		currentMapFile := GetMapFile(ctx)

		fmt.Println("\nMap Files:")
		fmt.Printf("Current: \033[1m%s\033[0m\n", currentMapFile)
		if len(mapFiles) == 0 {
			fmt.Println("  No map files found.")
		} else {
			for i, mf := range mapFiles {
				current := ""
				if mf == currentMapFile {
					current = " (current)"
				}
				fmt.Printf("  %d. %s%s\n", i+1, mf, current)
			}
		}
		fmt.Println()
		fmt.Println("  (N)ew map file")
		fmt.Println("  (Q)uit")
		fmt.Println()

		fmt.Print("Select option: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)

		if strings.ToLower(input) == "q" {
			return currentMapFile, nil
		}

		if strings.ToLower(input) == "n" {
			fmt.Print("Enter new map file name: ")
			newName, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read input: %w", err)
			}
			newName = strings.TrimSpace(newName)
			if newName == "" {
				fmt.Println("Map file name cannot be empty.")
				continue
			}
			if !strings.HasSuffix(newName, ".map") {
				newName = newName + ".map"
			}

			// Create the new map file
			mapFilePath := filepath.Join(notesPath, newName)
			if _, err := os.Stat(mapFilePath); os.IsNotExist(err) {
				if err := os.WriteFile(mapFilePath, []byte{}, 0644); err != nil {
					fmt.Printf("Failed to create map file: %v\n", err)
					continue
				}
			}

			// Set as current
			if err := config.SetMapFile(newName); err != nil {
				fmt.Printf("Failed to set map file: %v\n", err)
				continue
			}

			fmt.Printf("Created and switched to map file: %s\n", newName)
			return newName, nil
		}

		// Try to parse as number
		idx := -1
		fmt.Sscanf(input, "%d", &idx)
		if idx < 1 || idx > len(mapFiles) {
			fmt.Println("Invalid option.")
			continue
		}

		selectedMapFile := mapFiles[idx-1]
		if err := config.SetMapFile(selectedMapFile); err != nil {
			fmt.Printf("Failed to set map file: %v\n", err)
			continue
		}

		fmt.Printf("Switched to map file: %s\n", selectedMapFile)
		return selectedMapFile, nil
	}
}

func main() {
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	app := &cli.Command{
		Name:    "notetree",
		Version: appVersion,
		Usage:   "A simple CLI app for managing notes",
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			newCtx, err := setupConfig(ctx)
			return newCtx, err
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return mainMenu(ctx)
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
				Name:    "edit",
				Usage:   "Edit an existing note",
				Action: func(ctx context.Context, c *cli.Command) error {
					return editNotesInteractive(ctx)
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
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "filenames",
						Aliases: []string{"f"},
						Usage:   "Include filenames as separators in the output",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					filterTag := c.Args().First()
					includeFilenames := c.Bool("filenames")
					return readNotes(ctx, filterTag, includeFilenames)
				},
			},
		},
	}

	err := app.Run(context.Background(), os.Args);
	crash(err, "Error running notetree")
}
