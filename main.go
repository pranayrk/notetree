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
	vaultFileKey   contextKey = "vault_file"
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

func getVaultFile(ctx context.Context) string {
	if v := ctx.Value(vaultFileKey); v != nil {
		return v.(string)
	}
	return ""
}

func setupConfig(ctx context.Context) (context.Context, error) {
	notesPath, err := config.GetNotesPath()
	if err != nil {
		return ctx, err
	}

	vaultFile, err := config.GetVaultFile(notesPath)
	if err != nil {
		return ctx, err
	}

	if err := ensureNotesStructure(notesPath, vaultFile); err != nil {
		return ctx, err
	}

	ctx = context.WithValue(ctx, notesPathKey, notesPath)
	ctx = context.WithValue(ctx, vaultFileKey, vaultFile)
	return ctx, nil
}

// ============================================================================
// Data Layer - Vault File Operations
// ============================================================================

func loadNotes(notesPath, vaultFile string) ([]noteEntry, error) {
	notesVaultFile := filepath.Join(notesPath, vaultFile)
	data, err := os.ReadFile(notesVaultFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []noteEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read notes vault: %w", err)
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

func saveNotes(notesPath, vaultFile string, entries []noteEntry) error {
	notesVaultFile := filepath.Join(notesPath, vaultFile)

	file, err := os.Create(notesVaultFile)
	if err != nil {
		return fmt.Errorf("failed to open notes vault: %w", err)
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
			return fmt.Errorf("failed to write to notes vault: %w", err)
		}
	}
	return nil
}

func addNoteToVault(notesPath, vaultFile, filename string, tags []string) error {
	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	entries = append(entries, noteEntry{
		filename: filename,
		tags:     tags,
		firstTag: getFirstTag(tags),
	})
	return saveNotes(notesPath, vaultFile, entries)
}

func updateNoteTags(notesPath, vaultFile, filename string, tags []string) error {
	entries, err := loadNotes(notesPath, vaultFile)
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
	return saveNotes(notesPath, vaultFile, entries)
}

func removeNoteFromVault(notesPath, vaultFile, filename string) error {
	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	var newEntries []noteEntry
	for _, entry := range entries {
		if entry.filename != filename {
			newEntries = append(newEntries, entry)
		}
	}
	return saveNotes(notesPath, vaultFile, newEntries)
}

func collectNotesByTag(ctx context.Context) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	vaultFile := getVaultFile(ctx)
	notesVaultFile := filepath.Join(notesPath, vaultFile)

	data, err := os.ReadFile(notesVaultFile)
	if err != nil {
		return fmt.Errorf("failed to read notes vault: %w", err)
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

	// If no entries found, preserve the empty file
	if len(tagOrder) == 0 {
		fmt.Println("Notes organized by tag successfully.")
		return nil
	}

	// Rewrite vault file with entries grouped by tag
	file, err := os.Create(notesVaultFile)
	if err != nil {
		return fmt.Errorf("failed to open notes vault for writing: %w", err)
	}
	defer file.Close()

	for _, tag := range tagOrder {
		if _, err := fmt.Fprintf(file, "# Tag: %s\n", tag); err != nil {
			return fmt.Errorf("failed to write to notes vault: %w", err)
		}

		for _, entry := range tagEntries[tag] {
			var line string
			if len(entry.tags) > 0 {
				line = fmt.Sprintf("%s [%s]\n", entry.filename, strings.Join(entry.tags, ","))
			} else {
				line = fmt.Sprintf("%s\n", entry.filename)
			}
			if _, err := file.WriteString(line); err != nil {
				return fmt.Errorf("failed to write to notes vault: %w", err)
			}
		}

		if _, err := file.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to notes vault: %w", err)
		}
	}

	fmt.Println("Notes organized by tag successfully.")
	return nil
}

// ============================================================================
// Setup and Utilities
// ============================================================================

func ensureNotesStructure(notesPath, vaultFile string) error {
	if notesPath == "" {
		return fmt.Errorf("notes path is empty")
	}

	notesDir := filepath.Join(notesPath, "notes")
	imagesDir := filepath.Join(notesPath, "images")
	notesVaultFile := filepath.Join(notesPath, vaultFile)

	if err := os.MkdirAll(notesDir, 0755); err != nil {
		return fmt.Errorf("failed to create notes directory: %w", err)
	}
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory: %w", err)
	}
	if _, err := os.Stat(notesVaultFile); os.IsNotExist(err) {
		if err := os.WriteFile(notesVaultFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to create vault file: %w", err)
		}
	}
	return nil
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
	if idx := strings.Index(tag, "/"); idx != -1 && idx > 0 {
		return tag[:idx]
	}
	return tag
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
		for _, tag := range strings.Split(tagsStr, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				entry.tags = append(entry.tags, tag)
			}
		}
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
	vaultFile := getVaultFile(ctx)
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
			filename = time.Now().Format("2006-01-02_15-04-05") + ".md"
		} else if !strings.HasSuffix(filename, ".md") {
			filename += ".md"
		}

		filePath := filepath.Join(notesDir, filename)

		if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
			fmt.Printf("Failed to create note file: %v\n", err)
			continue
		}

		fmt.Printf("Created note: %s\n\n\n", filepath.Base(filePath))

		editorErr := openEditor(filePath)
		if editorErr != nil {
			fmt.Printf("Failed to open editor: %v\n", editorErr)
			fmt.Print("Continue without editing? (y to continue, Enter to retry): ")
			choice, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Failed to read input: %v\n", err)
				continue
			}
			if strings.ToLower(strings.TrimSpace(choice)) != "y" {
				continue
			}
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

		if err := addNoteToVault(notesPath, vaultFile, filename, tags); err != nil {
			fmt.Printf("Failed to add note to vault: %v\n", err)
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

// moveNoteToVault moves a note entry from the current vault file to another vault file
func moveNoteToVault(ctx context.Context, reader *bufio.Reader, filename string) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	currentVaultFile := getVaultFile(ctx)

	// List available vault files
	vaultFiles, err := config.ListVaultFiles(notesPath)
	if err != nil {
		return err
	}

	fmt.Println("\nAvailable vault files:")
	for i, vf := range vaultFiles {
		current := ""
		if vf == currentVaultFile {
			current = " (current)"
		}
		fmt.Printf("  %d. %s%s\n", i+1, vf, current)
	}
	fmt.Println()

	fmt.Print("Enter vault file number to move note to (or Enter to cancel): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		fmt.Println("Cancelled.")
		return nil
	}

	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(vaultFiles) {
		fmt.Println("Invalid option.")
		return nil
	}

	targetVaultFile := vaultFiles[idx-1]
	if targetVaultFile == currentVaultFile {
		fmt.Println("Note is already in this vault file.")
		return nil
	}

	// Load current entries and find the note
	entries, err := loadNotes(notesPath, currentVaultFile)
	if err != nil {
		return err
	}

	var noteEntry *noteEntry
	var entryIdx int
	for i, e := range entries {
		if e.filename == filename {
			noteEntry = &e
			entryIdx = i
			break
		}
	}

	if noteEntry == nil {
		fmt.Printf("Note not found: %s\n", filename)
		return nil
	}

	// Remove from current vault file
	newEntries := append(entries[:entryIdx], entries[entryIdx+1:]...)
	if err := saveNotes(notesPath, currentVaultFile, newEntries); err != nil {
		return err
	}

	// Add to target vault file
	targetEntries, err := loadNotes(notesPath, targetVaultFile)
	if err != nil {
		return err
	}
	targetEntries = append(targetEntries, *noteEntry)
	if err := saveNotes(notesPath, targetVaultFile, targetEntries); err != nil {
		return err
	}

	fmt.Printf("Moved '%s' from '%s' to '%s'.\n", filename, currentVaultFile, targetVaultFile)
	return nil
}

// browseNotesInteractive displays notes one by one with action options
func browseNotesInteractive(ctx context.Context, filterTag string, untaggedOnly bool) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	vaultFile := getVaultFile(ctx)
	reader := bufio.NewReader(os.Stdin)

	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	// Filter entries
	var filteredEntries []noteEntry
	for _, entry := range entries {
		if untaggedOnly {
			if len(entry.tags) == 0 || (len(entry.tags) == 1 && entry.tags[0] == "") {
				filteredEntries = append(filteredEntries, entry)
			}
		} else if filterTag == "" {
			filteredEntries = append(filteredEntries, entry)
		} else {
			for _, tag := range entry.tags {
				if tag == filterTag {
					filteredEntries = append(filteredEntries, entry)
					break
				}
			}
		}
	}

	if len(filteredEntries) == 0 {
		if untaggedOnly {
			fmt.Println("No untagged notes found.")
		} else if filterTag != "" {
			fmt.Printf("No notes found with tag: %s\n", filterTag)
		} else {
			fmt.Println("No notes found.")
		}
		return nil
	}

	fmt.Printf("\nBrowsing %d note(s)", len(filteredEntries))
	if untaggedOnly {
		fmt.Println(" (untagged)")
	} else if filterTag != "" {
		fmt.Printf(" (tag: %s)\n", filterTag)
	} else {
		fmt.Println()
	}
	fmt.Println()

	i := 0
	for i < len(filteredEntries) {
		entry := filteredEntries[i]
		fmt.Printf("\n\033[1m[%d/%d] %s\033[0m\n", i+1, len(filteredEntries), entry.filename)
		if len(entry.tags) > 0 {
			fmt.Printf("Tags: \033[36m%s\033[0m\n", strings.Join(entry.tags, ", "))
		} else {
			fmt.Println("Tags: \033[33m(none)\033[0m")
		}
		fmt.Println(strings.Repeat("-", 50))

		// Read and display note content
		filePath := filepath.Join(notesDir, entry.filename)
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			i++
			continue
		}

		// Display content (truncate if too long)
		contentStr := string(content)
		if len(contentStr) > 2000 {
			fmt.Println(contentStr[:2000])
			fmt.Println("\n... (content truncated, use (V) to view full content)")
		} else {
			fmt.Println(contentStr)
		}
		fmt.Println()

		// Show action menu
		fmt.Println("Actions:")
		fmt.Println("  (E)dit note")
		fmt.Println("  (T)ags update")
		fmt.Println("  (D)elete note")
		fmt.Println("  (M)ove to another map file")
		fmt.Println("  (V)iew full content in markdown reader")
		fmt.Println("  (N)ext / (P)revious / (Q)uit")
		fmt.Println()

		fmt.Print("Select action: ")
		action, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read action: %w", err)
		}
		action = strings.ToLower(strings.TrimSpace(action))

		switch action {
		case "e", "edit":
			if err := openEditor(filePath); err != nil {
				fmt.Printf("Failed to open editor: %v\n", err)
			} else {
				fmt.Println("Note edited.")
			}
		case "t", "tags":
			fmt.Printf("Current tags: %s\n", strings.Join(entry.tags, ", "))
			fmt.Print("Enter new tags (comma-separated, or Enter to keep current): ")
			tagsInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Failed to read tags: %v\n", err)
			} else {
				tagsInput = strings.TrimSpace(tagsInput)
				var newTags []string
				if tagsInput == "" {
					newTags = entry.tags
				} else {
					for _, tag := range strings.Split(tagsInput, ",") {
						tag = strings.TrimSpace(tag)
						if tag != "" {
							newTags = append(newTags, tag)
						}
					}
				}
				if err := updateNoteTags(notesPath, vaultFile, entry.filename, newTags); err != nil {
					fmt.Printf("Failed to update tags: %v\n", err)
				} else {
					fmt.Println("Tags updated.")
					if err := collectNotesByTag(ctx); err != nil {
						fmt.Printf("Error organizing notes by tag: %v\n", err)
					}
					// Reload entries to reflect changes
					if entries, err = loadNotes(notesPath, vaultFile); err != nil {
						fmt.Printf("Failed to reload notes: %v\n", err)
						return err
					}
					filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				}
			}
		case "d", "delete":
			fmt.Printf("Are you sure you want to delete '%s'? (y/n): ", entry.filename)
			confirm, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Failed to read confirmation: %v\n", err)
			} else if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
				if err := os.Remove(filePath); err != nil {
					fmt.Printf("Failed to delete file: %v\n", err)
				} else {
					fmt.Printf("Deleted file: %s\n", filePath)
					if err := removeNoteFromVault(notesPath, vaultFile, entry.filename); err != nil {
						fmt.Printf("Failed to remove from vault: %v\n", err)
					} else {
						fmt.Println("Removed from vault.")
						if err := collectNotesByTag(ctx); err != nil {
							fmt.Printf("Error organizing notes by tag: %v\n", err)
						}
						// Reload entries
						if entries, err = loadNotes(notesPath, vaultFile); err != nil {
							fmt.Printf("Failed to reload notes: %v\n", err)
							return err
						}
						filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
						// Don't increment i, stay at same position
						if len(filteredEntries) == 0 {
							fmt.Println("\nNo more notes to browse.")
							return nil
						}
						if i >= len(filteredEntries) {
							i = len(filteredEntries) - 1 // Move to last available entry
						}
					}
				}
			}
		case "m", "move":
			if err := moveNoteToVault(ctx, reader, entry.filename); err != nil {
				fmt.Printf("Failed to move note: %v\n", err)
			} else {
				if err := collectNotesByTag(ctx); err != nil {
					fmt.Printf("Error organizing notes by tag: %v\n", err)
				}
				// Reload entries
				if entries, err = loadNotes(notesPath, vaultFile); err != nil {
					fmt.Printf("Failed to reload notes: %v\n", err)
					return err
				}
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				// Don't increment i, stay at same position
				if len(filteredEntries) == 0 {
					fmt.Println("\nNo more notes to browse.")
					return nil
				}
				if i >= len(filteredEntries) {
					i = len(filteredEntries) - 1 // Move to last available entry
				}
			}
		case "v", "view":
			markdownReader, err := config.GetMarkdownReader()
			if err != nil {
				fmt.Printf("Failed to get markdown reader: %v\n", err)
			} else {
				cmd := exec.Command(markdownReader, filePath)
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					fmt.Printf("Failed to open markdown reader: %v\n", err)
				}
			}
		case "n", "next":
			i++
			if i >= len(filteredEntries) {
				fmt.Println("\nEnd of notes.")
				return nil
			}
		case "p", "prev", "previous":
			if i > 0 {
				i--
			} else {
				fmt.Println("Already at first note.")
			}
		case "q", "quit", "exit":
			fmt.Println("Exiting browse mode.")
			return nil
		}

		fmt.Println()
	}

	fmt.Println("\nEnd of notes.")
	return nil
}

// filterEntries filters entries by tag or untagged status
func filterEntries(entries []noteEntry, filterTag string, untaggedOnly bool) []noteEntry {
	var filtered []noteEntry
	for _, entry := range entries {
		if untaggedOnly {
			if len(entry.tags) == 0 {
				filtered = append(filtered, entry)
			}
		} else if filterTag == "" {
			filtered = append(filtered, entry)
		} else {
			for _, tag := range entry.tags {
				if tag == filterTag {
					filtered = append(filtered, entry)
					break
				}
			}
		}
	}
	return filtered
}

func manageVaultFiles(ctx context.Context, reader *bufio.Reader) (string, error) {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return "", fmt.Errorf("notes path not configured")
	}

	for {
		vaultFiles, err := config.ListVaultFiles(notesPath)
		if err != nil {
			return "", err
		}

		currentVaultFile := getVaultFile(ctx)

		fmt.Println("\nVault Files:")
		fmt.Printf("Current: \033[1m%s\033[0m\n", currentVaultFile)
		if len(vaultFiles) == 0 {
			fmt.Println("  No vault files found.")
		} else {
			for i, vf := range vaultFiles {
				current := ""
				if vf == currentVaultFile {
					current = " (current)"
				}
				fmt.Printf("  %d. %s%s\n", i+1, vf, current)
			}
		}
		fmt.Println()
		fmt.Println("  (N)ew vault file")
		fmt.Println("  (Q)uit")
		fmt.Println()

		fmt.Print("Select option: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(input)) {
		case "q":
			return currentVaultFile, nil
		case "n":
			fmt.Print("Enter new vault file name: ")
			newName, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read input: %w", err)
			}
			newName = strings.TrimSpace(newName)
			if newName == "" {
				fmt.Println("Vault file name cannot be empty.")
				continue
			}
			if !strings.HasSuffix(newName, ".vault") {
				newName += ".vault"
			}

			vaultFilePath := filepath.Join(notesPath, newName)
			if _, err := os.Stat(vaultFilePath); os.IsNotExist(err) {
				if err := os.WriteFile(vaultFilePath, []byte{}, 0644); err != nil {
					fmt.Printf("Failed to create vault file: %v\n", err)
					continue
				}
			}

			if err := config.SetVaultFile(newName); err != nil {
				fmt.Printf("Failed to set vault file: %v\n", err)
				continue
			}
			fmt.Printf("Created and switched to vault file: %s\n", newName)
			return newName, nil
		default:
			var idx int
			if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(vaultFiles) {
				fmt.Println("Invalid option.")
				continue
			}

			selectedVaultFile := vaultFiles[idx-1]
			if err := config.SetVaultFile(selectedVaultFile); err != nil {
				fmt.Printf("Failed to set vault file: %v\n", err)
				continue
			}
			fmt.Printf("Switched to vault file: %s\n", selectedVaultFile)
			return selectedVaultFile, nil
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
	vaultFile := getVaultFile(ctx)
	notesVaultFile := filepath.Join(notesPath, vaultFile)

	data, err := os.ReadFile(notesVaultFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read notes vault: %w", err)
	}

	var entries []noteEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseNoteLine(line)
		if entry.filename != "" {
			if filterTag == "" {
				entries = append(entries, entry)
			} else {
				for _, tag := range entry.tags {
					if tag == filterTag {
						entries = append(entries, entry)
						break
					}
				}
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
	vaultFile := getVaultFile(ctx)
	notesVaultFile := filepath.Join(notesPath, vaultFile)

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

	// Generate PDF in same folder as vault file so relative image paths work
	if _, err := exec.LookPath("pandoc"); err != nil {
		return fmt.Errorf("pandoc is not installed: %w", err)
	}

	tempPDF := filepath.Join(filepath.Dir(notesVaultFile), filepath.Base(tmpPath)+".pdf")
	cmd := exec.Command("pandoc", tmpPath, "--pdf-engine=typst", "-o", tempPDF)
	cmd.Dir = filepath.Dir(notesVaultFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tempPDF)
		return fmt.Errorf("failed to export PDF (ensure pandoc and typst are installed): %w", err)
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
		fmt.Printf("Current vault file: \033[1m%s\033[0m\n", getVaultFile(ctx))
		fmt.Println("  (A)dd notes")
		fmt.Println("  (B)rowse notes")
		fmt.Println("  (R)ead notes")
		fmt.Println("  (E)xport note PDF")
		fmt.Println("  (I)mage copy")
		fmt.Println("  (M)anage vault files")
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
		case "b":
			fmt.Print("Enter tag to filter (or Enter for all, 'u' for untagged only): ")
			tagInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else {
				tagInput = strings.TrimSpace(tagInput)
				filterTag := ""
				untaggedOnly := false
				if tagInput == "u" || tagInput == "U" {
					untaggedOnly = true
				} else if tagInput != "" {
					filterTag = tagInput
				}
				if err := browseNotesInteractive(ctx, filterTag, untaggedOnly); err != nil {
					fmt.Printf("Error browsing notes: %v\n", err)
				}
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
		case "e":
			fmt.Print("Enter tag to filter (or Enter to export all): ")
			tagInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else if err := exportNotes(ctx, strings.TrimSpace(tagInput)); err != nil {
				fmt.Printf("Error exporting notes: %v\n", err)
			}
		case "i":
			if err := addImages(ctx); err != nil {
				fmt.Printf("Error adding images: %v\n", err)
			}
		case "m":
			if newVaultFile, err := manageVaultFiles(ctx, reader); err != nil {
				fmt.Printf("Error managing vault files: %v\n", err)
			} else {
				ctx = context.WithValue(ctx, vaultFileKey, newVaultFile)
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
				Name:        "browse",
				Aliases:     []string{"b"},
				Usage:       "Browse notes interactively",
				ArgsUsage:   "[tag]",
				Description: "Browse notes one by one with options to edit, delete, update tags, move to another map file, or view in markdown reader.\nUse 'u' or 'untagged' as the tag to browse untagged notes only.",
				Action: func(ctx context.Context, c *cli.Command) error {
					filterTag := c.Args().First()
					untaggedOnly := filterTag == "u" || filterTag == "untagged"
					if untaggedOnly {
						filterTag = ""
					}
					return browseNotesInteractive(ctx, filterTag, untaggedOnly)
				},
			},
			{
				Name:        "export",
				Aliases:     []string{"e"},
				Usage:       "Export notes to PDF",
				ArgsUsage:   "[tag]",
				Description: "Exports all notes to a PDF file.\nIf a tag is provided, only notes with that tag are included.",
				Action: func(ctx context.Context, c *cli.Command) error {
					return exportNotes(ctx, c.Args().First())
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
			{
				Name:        "vault",
				Aliases:     []string{"v"},
				Usage:       "Manage vault files",
				Description: "Create, switch between, or list vault files.",
				Action: func(ctx context.Context, c *cli.Command) error {
					reader := bufio.NewReader(os.Stdin)
					newVaultFile, err := manageVaultFiles(ctx, reader)
					if err != nil {
						fmt.Printf("Error managing vault files: %v\n", err)
					} else {
						fmt.Printf("Current vault file: %s\n", newVaultFile)
					}
					return nil
				},
			},
			{
				Name:    "pwd",
				Usage:   "Print the notes directory path",
				Action: func(ctx context.Context, c *cli.Command) error {
					notesPath, err := config.GetNotesPathWithoutPrompt()
					if err != nil {
						return fmt.Errorf("failed to get notes path: %w", err)
					}
					if notesPath == "" {
						return fmt.Errorf("notes path not configured, run 'notetree' to configure")
					}
					fmt.Println(notesPath)
					return nil
				},
			},
			{
				Name:    "config",
				Usage:   "Edit the configuration file",
				Action: func(ctx context.Context, c *cli.Command) error {
					configPath, err := config.GetConfigPath()
					if err != nil {
						return fmt.Errorf("failed to get config path: %w", err)
					}

					// Check if config file exists, create if not
					if _, err := os.Stat(configPath); os.IsNotExist(err) {
						if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
							return fmt.Errorf("failed to create config directory: %w", err)
						}
						if err := os.WriteFile(configPath, []byte{}, 0644); err != nil {
							return fmt.Errorf("failed to create config file: %w", err)
						}
						fmt.Printf("Created config file: %s\n", configPath)
					}

					editor := os.Getenv("EDITOR")
					if editor == "" {
						editor = "vim"
					}
					cmd := exec.Command(editor, configPath)
					cmd.Stdin = os.Stdin
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					return cmd.Run()
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
