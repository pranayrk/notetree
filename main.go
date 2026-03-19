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

// Application constants
const (
	appVersion = "0.1.0"
)

// Context keys for storing configuration in context
type contextKey string

const (
	notesPathKey contextKey = "notes_path"
	mapFileKey   contextKey = "map_file"
)

// noteEntry represents a note with its metadata
type noteEntry struct {
	filename string
	tags     []string
	firstTag string
}

// ============================================================================
// Context Helpers
// ============================================================================

func getNotesPath(ctx context.Context) string {
	if v := ctx.Value(notesPathKey); v != nil {
		return v.(string)
	}
	return ""
}

func getMapFile(ctx context.Context) string {
	if v := ctx.Value(mapFileKey); v != nil {
		return v.(string)
	}
	return ""
}

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

// ============================================================================
// Data Layer - Map File Operations
// ============================================================================

func loadNotes(notesPath, mapFile string) ([]noteEntry, error) {
	notesMapFile := filepath.Join(notesPath, mapFile)
	data, err := os.ReadFile(notesMapFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []noteEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read notes map: %w", err)
	}

	var entries []noteEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if entry := parseNoteLine(line); entry.filename != "" {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func saveNotes(notesPath, mapFile string, entries []noteEntry) error {
	notesMapFile := filepath.Join(notesPath, mapFile)

	file, err := os.Create(notesMapFile)
	if err != nil {
		return fmt.Errorf("failed to open notes map: %w", err)
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
			return fmt.Errorf("failed to write to notes map: %w", err)
		}
	}
	return nil
}

func addNoteToMap(notesPath, mapFile, filename string, tags []string) error {
	entries, err := loadNotes(notesPath, mapFile)
	if err != nil {
		return err
	}

	entries = append(entries, noteEntry{
		filename: filename,
		tags:     tags,
		firstTag: getFirstTag(tags),
	})
	return saveNotes(notesPath, mapFile, entries)
}

func updateNoteTags(notesPath, mapFile, filename string, tags []string) error {
	entries, err := loadNotes(notesPath, mapFile)
	if err != nil {
		return err
	}

	for i := range entries {
		if entries[i].filename == filename {
			entries[i].tags = tags
			entries[i].firstTag = getFirstTag(tags)
			break
		}
	}
	return saveNotes(notesPath, mapFile, entries)
}

func removeNoteFromMap(notesPath, mapFile, filename string) error {
	entries, err := loadNotes(notesPath, mapFile)
	if err != nil {
		return err
	}

	var newEntries []noteEntry
	for _, entry := range entries {
		if entry.filename != filename {
			newEntries = append(newEntries, entry)
		}
	}
	return saveNotes(notesPath, mapFile, newEntries)
}

func collectNotesByTag(ctx context.Context) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	mapFile := getMapFile(ctx)
	notesMapFile := filepath.Join(notesPath, mapFile)

	data, err := os.ReadFile(notesMapFile)
	if err != nil {
		return fmt.Errorf("failed to read notes map: %w", err)
	}

	// Group entries by their first tag
	tagEntries := make(map[string][]noteEntry)
	var tagOrder []string
	seenTags := make(map[string]bool)

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseNoteLine(line)
		if entry.filename == "" {
			continue
		}

		tag := entry.firstTag
		if tag == "" {
			tag = "_untagged"
		}

		if !seenTags[tag] {
			seenTags[tag] = true
			tagOrder = append(tagOrder, tag)
		}
		tagEntries[tag] = append(tagEntries[tag], entry)
	}

	// Rewrite map file with entries grouped by tag
	file, err := os.Create(notesMapFile)
	if err != nil {
		return fmt.Errorf("failed to open notes map for writing: %w", err)
	}
	defer file.Close()

	for _, tag := range tagOrder {
		if _, err := fmt.Fprintf(file, "# Tag: %s\n", tag); err != nil {
			return fmt.Errorf("failed to write to notes map: %w", err)
		}

		for _, entry := range tagEntries[tag] {
			var line string
			if len(entry.tags) > 0 {
				line = fmt.Sprintf("%s [%s]\n", entry.filename, strings.Join(entry.tags, ","))
			} else {
				line = fmt.Sprintf("%s\n", entry.filename)
			}
			if _, err := file.WriteString(line); err != nil {
				return fmt.Errorf("failed to write to notes map: %w", err)
			}
		}

		if _, err := file.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to notes map: %w", err)
		}
	}

	fmt.Println("Notes organized by tag successfully.")
	return nil
}

// ============================================================================
// Setup and Utilities
// ============================================================================

func ensureNotesStructure(notesPath, mapFile string) error {
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
			return fmt.Errorf("failed to create map file: %w", err)
		}
	}
	return nil
}

func generateNoteFilename() string {
	return time.Now().Format("2006-01-02_15-04-05") + ".md"
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

	tags := make([]string, 0)
	for _, tag := range strings.Split(input, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, nil
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

func expandNestedTags(tag string) []string {
	parts := strings.Split(tag, "/")
	tags := make([]string, 0, len(parts))
	for i := range parts {
		tags = append(tags, strings.Join(parts[:i+1], "/"))
	}
	return tags
}

func hasTag(entryTags []string, targetTag string) bool {
	for _, tag := range entryTags {
		if tag == targetTag {
			return true
		}
	}
	return false
}

func parseNoteLine(line string) noteEntry {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
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

// ============================================================================
// Interactive Commands
// ============================================================================

func createNotesInteractive(ctx context.Context) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	mapFile := getMapFile(ctx)
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Create new note? (Enter to create, 'Q' to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		if strings.ToLower(strings.TrimSpace(input)) == "q" {
			fmt.Println("Exiting note add mode.")
			break
		}

		fmt.Print("Enter note filename (or press Enter for auto-generated): ")
		customFilename, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Failed to read filename: %v\n", err)
			continue
		}
		customFilename = strings.TrimSpace(customFilename)

		filename := customFilename
		if filename == "" {
			filename = generateNoteFilename()
		} else if !strings.HasSuffix(filename, ".md") {
			filename += ".md"
		}

		filePath := filepath.Join(notesDir, filename)

		if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
			fmt.Printf("Failed to create note file: %v\n", err)
			continue
		}

		fmt.Printf("Created note: %s\n\n\n", filepath.Base(filePath))

		if err := openEditor(filePath); err != nil {
			fmt.Printf("Failed to open editor: %v\n", err)
		}

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

		if err := addNoteToMap(notesPath, mapFile, filename, tags); err != nil {
			fmt.Printf("Failed to add note to map: %v\n", err)
		}
		fmt.Println()
	}
	return nil
}

func editNotesInteractive(ctx context.Context) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	mapFile := getMapFile(ctx)
	notesDir := filepath.Join(notesPath, "notes")
	reader := bufio.NewReader(os.Stdin)

	entries, err := loadNotes(notesPath, mapFile)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No notes found.")
		return nil
	}

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

		info, err := os.Stat(filePath)
		if err != nil {
			fmt.Printf("Failed to stat file: %v\n", err)
			continue
		}

		if info.Size() == 0 {
			os.Remove(filePath)
			if err := removeNoteFromMap(notesPath, mapFile, foundEntry.filename); err != nil {
				fmt.Printf("Failed to remove from map: %v\n", err)
			}
			fmt.Println("Note is empty, deleted and removed from map.")
			entries, _ = loadNotes(notesPath, mapFile)
			fmt.Println()
			continue
		}

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
			for _, tag := range strings.Split(tagsInput, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					newTags = append(newTags, expandNestedTags(tag)...)
				}
			}
		}

		if err := updateNoteTags(notesPath, mapFile, foundEntry.filename, newTags); err != nil {
			fmt.Printf("Failed to update tags: %v\n", err)
		} else {
			fmt.Println("Tags updated.")
			entries, _ = loadNotes(notesPath, mapFile)
		}
		fmt.Println()
	}
	return nil
}

func addImages(ctx context.Context) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	imagesDir := filepath.Join(notesPath, "images")
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Interactive image add mode. Enter image paths one at a time.")
	fmt.Println("Press 'Q' to quit.")
	fmt.Println()

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
			ext := filepath.Ext(filename)
			name := filename[:len(filename)-len(ext)]
			filename = fmt.Sprintf("%s_%s%s", name, time.Now().Format("2006-01-02_15-04-05"), ext)
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

func deleteNotes(ctx context.Context, reader *bufio.Reader) error {
	notesPath := getNotesPath(ctx)
	mapFile := getMapFile(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")

	fmt.Println("\nDelete notes mode. Enter note filenames to delete one at a time.")
	fmt.Println("Press 'Q' to quit.")
	fmt.Println()

	for {
		entries, err := loadNotes(notesPath, mapFile)
		if err != nil {
			return err
		}

		fmt.Print("Enter note filename to delete (or 'Q' to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.ToLower(input) == "q" {
			fmt.Println("Exiting delete mode.")
			break
		}

		var foundEntry *noteEntry
		for i := range entries {
			if entries[i].filename == input {
				foundEntry = &entries[i]
				break
			}
		}

		if foundEntry == nil {
			fmt.Printf("Note not found in map: %s\n", input)
			fmt.Println()
			continue
		}

		filePath := filepath.Join(notesDir, foundEntry.filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Printf("Note file does not exist: %s\n", foundEntry.filename)
		} else if err := os.Remove(filePath); err != nil {
			fmt.Printf("Failed to delete file: %v\n", err)
			continue
		} else {
			fmt.Printf("Deleted file: %s\n", filePath)
		}

		if err := removeNoteFromMap(notesPath, mapFile, foundEntry.filename); err != nil {
			fmt.Printf("Failed to remove from map: %v\n", err)
		} else {
			fmt.Println("Removed from map.")
		}
		fmt.Println()
	}
	return nil
}

func manageMapFiles(ctx context.Context, reader *bufio.Reader) (string, error) {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return "", fmt.Errorf("notes path not configured")
	}

	for {
		mapFiles, err := config.ListMapFiles(notesPath)
		if err != nil {
			return "", err
		}

		currentMapFile := getMapFile(ctx)

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

		switch strings.ToLower(strings.TrimSpace(input)) {
		case "q":
			return currentMapFile, nil
		case "n":
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
				newName += ".map"
			}

			mapFilePath := filepath.Join(notesPath, newName)
			if _, err := os.Stat(mapFilePath); os.IsNotExist(err) {
				if err := os.WriteFile(mapFilePath, []byte{}, 0644); err != nil {
					fmt.Printf("Failed to create map file: %v\n", err)
					continue
				}
			}

			if err := config.SetMapFile(newName); err != nil {
				fmt.Printf("Failed to set map file: %v\n", err)
				continue
			}
			fmt.Printf("Created and switched to map file: %s\n", newName)
			return newName, nil
		default:
			var idx int
			if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(mapFiles) {
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
}

// ============================================================================
// Read and Export Functions
// ============================================================================

func buildNotesFile(ctx context.Context, filterTag string, includeFilenames bool) (string, func(), error) {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return "", nil, fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	mapFile := getMapFile(ctx)
	notesMapFile := filepath.Join(notesPath, mapFile)

	data, err := os.ReadFile(notesMapFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read notes map: %w", err)
	}

	var entries []noteEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseNoteLine(line)
		if entry.filename != "" {
			if filterTag == "" || hasTag(entry.tags, filterTag) {
				entries = append(entries, entry)
			}
		}
	}

	if len(entries) == 0 {
		return "", nil, nil
	}

	tmpFile, err := os.CreateTemp(notesPath, "temp_*.md")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()

	for _, entry := range entries {
		notePath := filepath.Join(notesDir, entry.filename)
		content, err := os.ReadFile(notePath)
		if err != nil {
			fmt.Printf("Warning: could not read %s: %v\n", entry.filename, err)
			continue
		}

		if includeFilenames {
			if _, err := tmpFile.WriteString(fmt.Sprintf("\n\n--- %s ---\n\n", entry.filename)); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				return "", nil, fmt.Errorf("failed to write to temporary file: %w", err)
			}
		}
		if _, err := tmpFile.Write(content); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", nil, fmt.Errorf("failed to write to temporary file: %w", err)
		}
		if _, err := tmpFile.WriteString("\n\n"); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", nil, fmt.Errorf("failed to write to temporary file: %w", err)
		}
	}
	tmpFile.Close()

	return tmpPath, func() { os.Remove(tmpPath) }, nil
}

func readNotes(ctx context.Context, filterTag string, includeFilenames bool) error {
	tmpPath, cleanup, err := buildNotesFile(ctx, filterTag, includeFilenames)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	if tmpPath == "" {
		if filterTag != "" {
			fmt.Printf("No notes found with tag: %s\n", filterTag)
		} else {
			fmt.Println("No notes found.")
		}
		return nil
	}

	markdownReader, err := config.GetMarkdownReader()
	if err != nil {
		return fmt.Errorf("failed to get markdown reader: %w", err)
	}

	cmd := exec.Command(markdownReader, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func exportNotes(ctx context.Context, filterTag string) error {
	tmpPath, cleanup, err := buildNotesFile(ctx, filterTag, false)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	if tmpPath == "" {
		if filterTag != "" {
			fmt.Printf("No notes found with tag: %s\n", filterTag)
		} else {
			fmt.Println("No notes found.")
		}
		return nil
	}

	notesPath := getNotesPath(ctx)
	mapFile := getMapFile(ctx)
	notesMapFile := filepath.Join(notesPath, mapFile)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter output PDF path: ")
	outputPath, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read output path: %w", err)
	}
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	// Generate PDF in same folder as map file so relative image paths work
	tempPDF := filepath.Join(filepath.Dir(notesMapFile), filepath.Base(tmpPath)+".pdf")
	cmd := exec.Command("pandoc", tmpPath, "--pdf-engine=typst", "-o", tempPDF)
	cmd.Dir = filepath.Dir(notesMapFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tempPDF)
		return fmt.Errorf("failed to export PDF: %w", err)
	}

	// Move the PDF to the final output path
	if err := os.Rename(tempPDF, outputPath); err != nil {
		// If rename fails (e.g., cross-device), copy and delete
		srcData, readErr := os.ReadFile(tempPDF)
		if readErr != nil {
			os.Remove(tempPDF)
			return fmt.Errorf("failed to export PDF: %w", err)
		}
		if writeErr := os.WriteFile(outputPath, srcData, 0644); writeErr != nil {
			os.Remove(tempPDF)
			return fmt.Errorf("failed to export PDF: %w", err)
		}
		os.Remove(tempPDF)
	}

	fmt.Printf("Exported PDF to: %s\n", outputPath)
	return nil
}

// ============================================================================
// Main Menu
// ============================================================================

func mainMenu(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("notetree version %s\n", appVersion)
		fmt.Printf("Current map file: \033[1m%s\033[0m\n", getMapFile(ctx))
		fmt.Println("  (A)dd notes")
		fmt.Println("  (R)ead notes")
		fmt.Println("  (X)port note PDF")
		fmt.Println("  (E)dit notes")
		fmt.Println("  (D)elete notes")
		fmt.Println("  (I)mage copy")
		fmt.Println("  (M)ap files")
		fmt.Println("  (Q)uit")
		fmt.Println()

		fmt.Print("Select option: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(input)) {
		case "a":
			if err := createNotesInteractive(ctx); err != nil {
				fmt.Printf("Error in add mode: %v\n", err)
			} else if err := collectNotesByTag(ctx); err != nil {
				fmt.Printf("Error organizing notes by tag: %v\n", err)
			}
		case "r":
			fmt.Print("Enter tag to filter (or Enter to read all): ")
			tagInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else {
				fmt.Print("Include filenames in output? (y for yes, Enter for no): ")
				includeInput, err := reader.ReadString('\n')
				if err != nil {
					fmt.Printf("Error reading option: %v\n", err)
				} else {
					includeFilenames := strings.ToLower(strings.TrimSpace(includeInput)) == "y"
					if err := readNotes(ctx, strings.TrimSpace(tagInput), includeFilenames); err != nil {
						fmt.Printf("Error reading notes: %v\n", err)
					}
				}
			}
		case "x":
			fmt.Print("Enter tag to filter (or Enter to export all): ")
			tagInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else if err := exportNotes(ctx, strings.TrimSpace(tagInput)); err != nil {
				fmt.Printf("Error exporting notes: %v\n", err)
			}
		case "e":
			if err := editNotesInteractive(ctx); err != nil {
				fmt.Printf("Error in edit mode: %v\n", err)
			} else if err := collectNotesByTag(ctx); err != nil {
				fmt.Printf("Error organizing notes by tag: %v\n", err)
			}
		case "i":
			if err := addImages(ctx); err != nil {
				fmt.Printf("Error adding images: %v\n", err)
			}
		case "m":
			if newMapFile, err := manageMapFiles(ctx, reader); err != nil {
				fmt.Printf("Error managing map files: %v\n", err)
			} else {
				ctx = context.WithValue(ctx, mapFileKey, newMapFile)
			}
		case "d":
			if err := deleteNotes(ctx, reader); err != nil {
				fmt.Printf("Error deleting notes: %v\n", err)
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

// ============================================================================
// Entry Point
// ============================================================================

func crash(err error, text string) {
	if err != nil {
		if text != "" {
			err = fmt.Errorf("error: %q:\n %w", text, err)
		}
		log.Fatal(err)
	}
}

func main() {
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	app := &cli.Command{
		Name:      "notetree",
		Version:   appVersion,
		Usage:     "A simple CLI app for managing notes",
		UsageText: "notetree [command] [options]",
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			return setupConfig(ctx)
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
				After: func(ctx context.Context, cmd *cli.Command) error {
					return collectNotesByTag(ctx)
				},
			},
			{
				Name:    "edit",
				Usage:   "Edit an existing note",
				Action: func(ctx context.Context, c *cli.Command) error {
					return editNotesInteractive(ctx)
				},
				After: func(ctx context.Context, cmd *cli.Command) error {
					return collectNotesByTag(ctx)
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
				Name:        "read",
				Usage:       "Read and concatenate notes",
				ArgsUsage:   "[tag]",
				Description: "Concatenates all notes into a temporary file and displays them.\nIf a tag is provided, only notes with that tag are included.",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "filenames",
						Aliases: []string{"f"},
						Usage:   "Include filenames as separators in the output",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return readNotes(ctx, c.Args().First(), c.Bool("filenames"))
				},
			},
		},
	}

	crash(app.Run(context.Background(), os.Args), "Error running notetree")
}
