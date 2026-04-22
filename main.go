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
	"golang.org/x/term"
)

// Application constants
const (
	appVersion = "0.1.0"
)

// Context keys for storing configuration in context
type contextKey string

const (
	notesPathKey contextKey = "notes_path"
	vaultFileKey contextKey = "vault_file"
)

// noteEntry represents a note with its metadata
type noteEntry struct {
	filename string
	tags     []string
	hidden   bool
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
		filename := entry.filename
		if entry.hidden {
			filename = "! " + filename
		}
		if len(entry.tags) > 0 {
			line = fmt.Sprintf("%s [%s]\n", filename, strings.Join(entry.tags, ","))
		} else {
			line = fmt.Sprintf("%s\n", filename)
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

// renameNoteFile renames a note file and updates the vault file
func renameNoteFile(ctx context.Context, reader *bufio.Reader, oldFilename string) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	vaultFile := getVaultFile(ctx)

	fmt.Print("Enter new filename (or Enter to cancel): ")
	newFilename, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	newFilename = strings.TrimSpace(newFilename)
	if newFilename == "" {
		fmt.Println("\033[33mCancelled.\033[0m")
		return nil
	}

	if newFilename == oldFilename {
		fmt.Println("\033[33mFilename unchanged.\033[0m")
		return nil
	}

	if !strings.HasSuffix(newFilename, ".md") {
		newFilename += ".md"
	}

	oldFilePath := filepath.Join(notesDir, oldFilename)
	newFilePath := filepath.Join(notesDir, newFilename)

	if _, err := os.Stat(newFilePath); err == nil {
		fmt.Printf("\033[31mFile already exists: %s\033[0m\n", newFilename)
		return nil
	}

	if err := os.Rename(oldFilePath, newFilePath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	for i := range entries {
		if entries[i].filename == oldFilename {
			entries[i].filename = newFilename
		}
	}

	if err := saveNotes(notesPath, vaultFile, entries); err != nil {
		return err
	}

	fmt.Printf("Renamed \033[32m'%s'\033[0m to \033[32m'%s'\033[0m.\n", oldFilename, newFilename)

	return nil
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
	tagGroups := make(map[string][]noteEntry)
	var tagOrder []string

	// Add untagged entries to the start of the file
	tagOrder = append(tagOrder, "_untagged")
	tagGroups["_untagged"] = []noteEntry{}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseNoteLine(line)
		if entry.filename == "" {
			continue
		}

		tag := ""
		if len(entry.tags) > 0 {
			tag = entry.tags[0]
		}
		if tag == "" {
			tag = "_untagged"
		}

		_, seenTag := tagGroups[tag];
		if !seenTag {
			matchedTag := false
			for i := len(tagOrder) - 1; i >= 0; i-- {
				if(tagMatches(tag, tagOrder[i])) {
					for j := i + 1; j < len(tagOrder); j++ {
						if(!tagMatches(tagOrder[j], tagOrder[i])) {
							tagOrder = append(tagOrder[:j], append([]string{tag}, tagOrder[j:]...)...)
							matchedTag = true
							break
						}
					}
					break
				}
			}
			if(!matchedTag) {
				tagOrder = append(tagOrder, tag)
			}
		}
		tagGroups[tag] = append(tagGroups[tag], entry)
	}

	// If no entries found, we're done
	if len(tagOrder) == 0 {
		return nil
	}

	// Rewrite vault file with entries grouped by tag
	// Tags are written in the order they first appeared in the file
	file, err := os.Create(notesVaultFile)
	if err != nil {
		return fmt.Errorf("failed to open notes vault for writing: %w", err)
	}
	defer file.Close()

	for _, tag := range tagOrder {
		for _, entry := range tagGroups[tag] {
			var line string
			filename := entry.filename
			if entry.hidden {
				filename = "! " + filename
			}
			if len(entry.tags) > 0 {
				line = fmt.Sprintf("%s [%s]\n", filename, strings.Join(entry.tags, ","))
			} else {
				line = fmt.Sprintf("%s\n", filename)
			}
			if _, err := file.WriteString(line); err != nil {
				return fmt.Errorf("failed to write to notes vault: %w", err)
			}
		}
	}

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
	if err := cmd.Run(); err != nil {
		return err
	}
	// Small delay to allow terminal to settle after editor exits
	time.Sleep(50 * time.Millisecond)
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

	tags := make([]string, 0)
	for _, tag := range strings.Split(input, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

// tagMatches checks if a tag matches a filter using hierarchical matching.
// A tag matches if:
//   - It equals the filter exactly (e.g., "robotics" matches "robotics")
//   - It is a nested tag under the filter (e.g., "robotics/fpga" matches "robotics")
//   - It is a deeper nested tag under the filter (e.g., "robotics/fpga/sensor" matches "robotics")
//
// Examples:
//   - tagMatches("robotics/fpga", "robotics") → true
//   - tagMatches("robotics/fpga", "robotics/fpga") → true
//   - tagMatches("robotics", "robotics/fpga") → false
//   - tagMatches("robotics/fpga/sensor", "robotics") → true
//   - tagMatches("robotics/fpga/sensor", "robotics/fpga") → true
func tagMatches(tag, filterTag string) bool {
	if tag == filterTag {
		return true
	}
	// Check if tag is a nested child of filterTag
	// e.g., "robotics/fpga" should match filter "robotics"
	if strings.HasPrefix(tag, filterTag+"/") {
		return true
	}
	return false
}

// tagsMatch checks if two tag slices contain the same tags in the same order
func tagsMatch(tags1, tags2 []string) bool {
	if len(tags1) != len(tags2) {
		return false
	}
	for i := range tags1 {
		if tags1[i] != tags2[i] {
			return false
		}
	}
	return true
}

// ============================================================================
// Terminal Control and Autocomplete
// ============================================================================

// readKey reads a single key press from stdin
func readKey() (rune, error) {
	b := make([]byte, 1)
	_, err := os.Stdin.Read(b)
	if err != nil {
		return 0, err
	}
	return rune(b[0]), nil
}

// readEscapeSequence reads escape sequences for special keys
func readEscapeSequence() (rune, error) {
	// Read '[' after ESC
	b := make([]byte, 1)
	_, err := os.Stdin.Read(b)
	if err != nil {
		return 0, err
	}
	
	if b[0] == '[' {
		// Read the next character to determine the key
		_, err := os.Stdin.Read(b)
		if err != nil {
			return 0, err
		}
		switch b[0] {
		case 'A':
			return '↑', nil // Up arrow
		case 'B':
			return '↓', nil // Down arrow
		case 'C':
			return '→', nil // Right arrow
		case 'D':
			return '←', nil // Left arrow
		case '3':
			// Delete key, consume '~'
			os.Stdin.Read(b)
			return '×', nil // Delete
		default:
			return rune(b[0]), nil
		}
	}
	return rune(b[0]), nil
}

// clearLine clears the current line and moves cursor to beginning
func clearLine() {
	fmt.Print("\033[2K\033[0G")
}

// moveCursorLeft moves cursor left by n positions
func moveCursorLeft(n int) {
	if n > 0 {
		fmt.Printf("\033[%dD", n)
	}
}

// getAllTags collects all unique tags from the vault file
func getAllTags(notesPath, vaultFile string) ([]string, error) {
	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return nil, err
	}

	tagSet := make(map[string]bool)
	for _, entry := range entries {
		for _, tag := range entry.tags {
			if tag != "" {
				tagSet[tag] = true
			}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

// findTagCompletions finds tags that start with the given prefix
func findTagCompletions(allTags []string, prefix string) []string {
	if prefix == "" {
		return allTags
	}

	var completions []string
	for _, tag := range allTags {
		if strings.HasPrefix(tag, prefix) {
			completions = append(completions, tag)
		}
	}
	return completions
}

// completeTag attempts to autocomplete a tag prefix
// Returns the completed tag and whether there were multiple matches
func completeTag(allTags []string, prefix string) (string, bool) {
	completions := findTagCompletions(allTags, prefix)
	if len(completions) == 0 {
		return prefix, false
	}
	if len(completions) == 1 {
		return completions[0], false
	}
	// Multiple completions - find common prefix
	common := completions[0]
	for _, tag := range completions[1:] {
		for len(common) > 0 && !strings.HasPrefix(tag, common) {
			common = common[:len(common)-1]
		}
	}
	if len(common) > len(prefix) {
		return common, false
	}
	// No common prefix to add, show all completions
	return prefix, true
}

// showCompletions displays matching tags to the user
func showCompletions(completions []string, cursorPos int, currentInput string) {
	if len(completions) == 0 {
		return
	}

	// Limit to first 10 suggestions
	maxDisplay := 10
	displayCount := len(completions)
	if displayCount > maxDisplay {
		displayCount = maxDisplay
	}

	// Use \r\n for raw mode compatibility (terminal is in raw mode)
	fmt.Print("\r\n")
	fmt.Print("\033[90mSuggestions:\033[0m\r\n")
	for i := 0; i < displayCount; i++ {
		fmt.Printf("  \033[36m%s\033[0m\r\n", completions[i])
	}
	if len(completions) > maxDisplay {
		fmt.Printf("  \033[90m... and %d more\033[0m\r\n", len(completions)-maxDisplay)
	}

	// Move cursor back up to the input line
	// +1 for the initial newline, +1 for "Suggestions:" line, +displayCount for tags, +1 for "and X more" if shown
	linesUp := 2 + displayCount
	if len(completions) > maxDisplay {
		linesUp++
	}
	fmt.Printf("\033[%dA", linesUp)

	// Redraw the input line
	clearLine()
	fmt.Print("Enter tags (comma-separated, Tab for autocomplete): ")
	fmt.Print(currentInput)
	moveCursorLeft(len(currentInput) - cursorPos)
}

// getAllNoteFilenames collects all filenames from the vault file
func getAllNoteFilenames(notesPath, vaultFile string) ([]string, error) {
	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return nil, err
	}

	filenames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.filename != "" {
			filenames = append(filenames, entry.filename)
		}
	}
	sort.Strings(filenames)
	return filenames, nil
}

// findFilenameCompletions finds filenames that start with the given prefix
func findFilenameCompletions(allFilenames []string, prefix string) []string {
	if prefix == "" {
		return allFilenames
	}

	var completions []string
	for _, filename := range allFilenames {
		if strings.HasPrefix(filename, prefix) {
			completions = append(completions, filename)
		}
	}
	return completions
}

// showFilenameCompletions displays matching filenames to the user
func showFilenameCompletions(completions []string, cursorPos int, currentInput string) {
	if len(completions) == 0 {
		return
	}

	// Limit to first 10 suggestions
	maxDisplay := 10
	displayCount := len(completions)
	if displayCount > maxDisplay {
		displayCount = maxDisplay
	}

	// Use \r\n for raw mode compatibility (terminal is in raw mode)
	fmt.Print("\r\n")
	fmt.Print("\033[90mSuggestions:\033[0m\r\n")
	for i := 0; i < displayCount; i++ {
		fmt.Printf("  \033[36m%s\033[0m\r\n", completions[i])
	}
	if len(completions) > maxDisplay {
		fmt.Printf("  \033[90m... and %d more\033[0m\r\n", len(completions)-maxDisplay)
	}

	// Move cursor back up to the input line
	// +1 for the initial newline, +1 for "Suggestions:" line, +displayCount for filenames, +1 for "and X more" if shown
	linesUp := 2 + displayCount
	if len(completions) > maxDisplay {
		linesUp++
	}
	fmt.Printf("\033[%dA", linesUp)

	// Redraw the input line
	clearLine()
	fmt.Print("Enter filename (Tab for autocomplete): ")
	fmt.Print(currentInput)
	moveCursorLeft(len(currentInput) - cursorPos)
}

// promptForFilenameWithAutocomplete prompts for a filename with tab completion support
func promptForFilenameWithAutocomplete(notesPath, vaultFile string) (string, error) {
	// Get all existing filenames for autocomplete
	allFilenames, err := getAllNoteFilenames(notesPath, vaultFile)
	if err != nil {
		// Fall back to simple prompt if we can't load filenames
		return promptForFilenameSimple()
	}

	// Check if terminal supports raw mode
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return promptForFilenameSimple()
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return promptForFilenameSimple()
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Print("Enter filename (Tab for autocomplete): ")

	var input strings.Builder
	cursorPos := 0

	for {
		key, err := readKey()
		if err != nil {
			return "", err
		}

		switch key {
		case '\t': // Tab - autocomplete
			currentInput := input.String()
			completions := findFilenameCompletions(allFilenames, currentInput)
			if len(completions) == 1 {
				// Single match - complete it
				completion := completions[0]
				input.Reset()
				input.WriteString(completion)
				cursorPos = input.Len()
				clearLine()
				fmt.Print("Enter filename (Tab for autocomplete): ")
				fmt.Print(input.String())
			} else if len(completions) > 1 {
				// Multiple matches - show suggestions
				showFilenameCompletions(completions, cursorPos, input.String())
			}
			// No matches - do nothing

		case '\r', '\n': // Enter - accept
			fmt.Println()
			return strings.TrimSpace(input.String()), nil

		case 127, 8: // Backspace
			if cursorPos > 0 {
				inputStr := input.String()
				newStr := inputStr[:cursorPos-1] + inputStr[cursorPos:]
				input.Reset()
				input.WriteString(newStr)
				cursorPos--
				clearLine()
				fmt.Print("Enter filename (Tab for autocomplete): ")
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}

		case '×': // Delete key
			if cursorPos < input.Len() {
				inputStr := input.String()
				newStr := inputStr[:cursorPos] + inputStr[cursorPos+1:]
				input.Reset()
				input.WriteString(newStr)
				clearLine()
				fmt.Print("Enter filename (Tab for autocomplete): ")
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}

		case '←': // Left arrow
			if cursorPos > 0 {
				cursorPos--
				moveCursorLeft(1)
			}

		case '→': // Right arrow
			if cursorPos < input.Len() {
				cursorPos++
				fmt.Print(string(key))
			}

		case 3: // Ctrl+C
			fmt.Println("\n\033[33mCancelled.\033[0m")
			return "", nil

		default:
			// Insert character at cursor position
			if key >= 32 && key < 127 { // Printable ASCII
				inputStr := input.String()
				newStr := inputStr[:cursorPos] + string(key) + inputStr[cursorPos:]
				input.Reset()
				input.WriteString(newStr)
				cursorPos++
				clearLine()
				fmt.Print("Enter filename (Tab for autocomplete): ")
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}
		}
	}
}

// promptForFilenameSimple is the fallback simple prompt without autocomplete
func promptForFilenameSimple() (string, error) {
	fmt.Print("Enter filename: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	return strings.TrimSpace(input), nil
}

// promptForTagsWithAutocomplete prompts for tags with tab completion support
func promptForTagsWithAutocomplete(notesPath, vaultFile string) ([]string, error) {
	// Get all existing tags for autocomplete
	allTags, err := getAllTags(notesPath, vaultFile)
	if err != nil {
		// Fall back to simple prompt if we can't load tags
		return promptForTagsSimple()
	}

	// Check if terminal supports raw mode
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return promptForTagsSimple()
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return promptForTagsSimple()
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Print("Enter tags (comma-separated, Tab for autocomplete): ")

	var input strings.Builder
	cursorPos := 0

	for {
		key, err := readKey()
		if err != nil {
			return nil, err
		}

		switch key {
		case '\t': // Tab - autocomplete
			// Find the current word (tag being typed)
			currentInput := input.String()
			wordStart := strings.LastIndexByte(currentInput[:cursorPos], ',')
			if wordStart == -1 {
				wordStart = 0
			} else {
				wordStart++ // Skip the comma
			}
			prefix := currentInput[wordStart:cursorPos]
			
			completions := findTagCompletions(allTags, strings.TrimSpace(prefix))
			if len(completions) == 1 {
				// Single match - complete it
				completion := completions[0]
				input.WriteString(completion[len(prefix):])
				cursorPos = input.Len()
				clearLine()
				fmt.Print("Enter tags (comma-separated, Tab for autocomplete): ")
				fmt.Print(input.String())
			} else if len(completions) > 1 {
				// Multiple matches - show suggestions
				showCompletions(completions, cursorPos, input.String())
			}
			// No matches - do nothing

		case '\r', '\n': // Enter - accept
			fmt.Println()
			tags := parseTagsInput(input.String())
			return tags, nil

		case 127, 8: // Backspace
			if cursorPos > 0 {
				inputStr := input.String()
				newStr := inputStr[:cursorPos-1] + inputStr[cursorPos:]
				input.Reset()
				input.WriteString(newStr)
				cursorPos--
				clearLine()
				fmt.Print("Enter tags (comma-separated, Tab for autocomplete): ")
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}

		case '×': // Delete key
			if cursorPos < input.Len() {
				inputStr := input.String()
				newStr := inputStr[:cursorPos] + inputStr[cursorPos+1:]
				input.Reset()
				input.WriteString(newStr)
				clearLine()
				fmt.Print("Enter tags (comma-separated, Tab for autocomplete): ")
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}

		case '←': // Left arrow
			if cursorPos > 0 {
				cursorPos--
				moveCursorLeft(1)
			}

		case '→': // Right arrow
			if cursorPos < input.Len() {
				cursorPos++
				fmt.Print(string(key))
			}

		case 3: // Ctrl+C
			fmt.Println("\n\033[33mCancelled.\033[0m")
			return []string{}, nil

		default:
			// Insert character at cursor position
			if key >= 32 && key < 127 { // Printable ASCII
				inputStr := input.String()
				newStr := inputStr[:cursorPos] + string(key) + inputStr[cursorPos:]
				input.Reset()
				input.WriteString(newStr)
				cursorPos++
				clearLine()
				fmt.Print("Enter tags (comma-separated, Tab for autocomplete): ")
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}
		}
	}
}

// promptForTagsSimple is the fallback simple prompt without autocomplete
func promptForTagsSimple() ([]string, error) {
	fmt.Print("Enter tags (comma-separated, or press Enter to skip): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return []string{}, nil
	}

	return parseTagsInput(input), nil
}

// parseTagsInput parses a comma-separated input string into tags
func parseTagsInput(input string) []string {
	tags := make([]string, 0)
	for _, tag := range strings.Split(input, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// promptForSingleTagWithAutocomplete prompts for a single tag with tab completion support
// Used for filtering notes by tag in browse/read/export operations
func promptForSingleTagWithAutocomplete(notesPath, vaultFile string, promptText string) (string, error) {
	// Get all existing tags for autocomplete
	allTags, err := getAllTags(notesPath, vaultFile)
	if err != nil {
		// Fall back to simple prompt if we can't load tags
		return promptForSingleTagSimple(promptText)
	}

	// Check if terminal supports raw mode
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return promptForSingleTagSimple(promptText)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return promptForSingleTagSimple(promptText)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Print(promptText)

	var input strings.Builder
	cursorPos := 0

	for {
		key, err := readKey()
		if err != nil {
			return "", err
		}

		switch key {
		case '\t': // Tab - autocomplete
			currentInput := input.String()
			completions := findTagCompletions(allTags, currentInput)
			if len(completions) == 1 {
				// Single match - complete it
				completion := completions[0]
				input.Reset()
				input.WriteString(completion)
				cursorPos = input.Len()
				clearLine()
				fmt.Print(promptText)
				fmt.Print(input.String())
			} else if len(completions) > 1 {
				// Multiple matches - show suggestions
				showSingleTagCompletions(completions, cursorPos, input.String(), promptText)
			}
			// No matches - do nothing

		case '\r', '\n': // Enter - accept
			fmt.Println()
			return strings.TrimSpace(input.String()), nil

		case 127, 8: // Backspace
			if cursorPos > 0 {
				inputStr := input.String()
				newStr := inputStr[:cursorPos-1] + inputStr[cursorPos:]
				input.Reset()
				input.WriteString(newStr)
				cursorPos--
				clearLine()
				fmt.Print(promptText)
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}

		case '×': // Delete key
			if cursorPos < input.Len() {
				inputStr := input.String()
				newStr := inputStr[:cursorPos] + inputStr[cursorPos+1:]
				input.Reset()
				input.WriteString(newStr)
				clearLine()
				fmt.Print(promptText)
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}

		case '←': // Left arrow
			if cursorPos > 0 {
				cursorPos--
				moveCursorLeft(1)
			}

		case '→': // Right arrow
			if cursorPos < input.Len() {
				cursorPos++
				fmt.Print(string(key))
			}

		case 3: // Ctrl+C
			fmt.Println("\n\033[33mCancelled.\033[0m")
			return "", nil

		default:
			// Insert character at cursor position
			if key >= 32 && key < 127 { // Printable ASCII
				inputStr := input.String()
				newStr := inputStr[:cursorPos] + string(key) + inputStr[cursorPos:]
				input.Reset()
				input.WriteString(newStr)
				cursorPos++
				clearLine()
				fmt.Print(promptText)
				fmt.Print(input.String())
				moveCursorLeft(len(input.String()) - cursorPos)
			}
		}
	}
}

// showSingleTagCompletions displays matching tags for single tag input
func showSingleTagCompletions(completions []string, cursorPos int, currentInput string, promptText string) {
	if len(completions) == 0 {
		return
	}

	// Limit to first 10 suggestions
	maxDisplay := 10
	displayCount := len(completions)
	if displayCount > maxDisplay {
		displayCount = maxDisplay
	}

	// Use \r\n for raw mode compatibility (terminal is in raw mode)
	fmt.Print("\r\n")
	fmt.Print("\033[90mSuggestions:\033[0m\r\n")
	for i := 0; i < displayCount; i++ {
		fmt.Printf("  \033[36m%s\033[0m\r\n", completions[i])
	}
	if len(completions) > maxDisplay {
		fmt.Printf("  \033[90m... and %d more\033[0m\r\n", len(completions)-maxDisplay)
	}

	// Move cursor back up to the input line
	// +1 for the initial newline, +1 for "Suggestions:" line, +displayCount for tags, +1 for "and X more" if shown
	linesUp := 2 + displayCount
	if len(completions) > maxDisplay {
		linesUp++
	}
	fmt.Printf("\033[%dA", linesUp)

	// Redraw the input line
	clearLine()
	fmt.Print(promptText)
	fmt.Print(currentInput)
	moveCursorLeft(len(currentInput) - cursorPos)
}

// promptForSingleTagSimple is the fallback simple prompt without autocomplete
func promptForSingleTagSimple(promptText string) (string, error) {
	fmt.Print(promptText)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	return strings.TrimSpace(input), nil
}

func parseNoteLine(line string) noteEntry {
	line = strings.TrimSpace(line)
	if line == "" {
		return noteEntry{}
	}

	entry := noteEntry{}
	
	// Check if the line starts with "! " indicating a hidden file
	if strings.HasPrefix(line, "! ") {
		entry.hidden = true
		line = strings.TrimPrefix(line, "! ")
	}
	
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
	} else {
		entry.filename = line
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
			fmt.Println("\033[33mExiting note add mode.\033[0m")
			break
		}

		// Generate auto-generated filename
		filename := time.Now().Format("2006-01-02_15-04-05") + ".md"
		filePath := filepath.Join(notesDir, filename)

		// Ensure filename is unique
		for {
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				break
			}
			// File exists, add milliseconds to make it unique
			filename = time.Now().Format("2006-01-02_15-04-05.000") + ".md"
			filePath = filepath.Join(notesDir, filename)
		}

		if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
			fmt.Printf("Failed to create note file: %v\n", err)
			continue
		}

		fmt.Printf("Created note: \033[32m%s\033[0m\n\n\n", filepath.Base(filePath))

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
			fmt.Println("\033[32mNote is empty, deleted.\033[0m")
			fmt.Println()
			continue
		}

		// Ask if user wants to rename the file
		fmt.Printf("Current filename: \033[32m%s\033[0m\n", filename)
		fmt.Print("Rename file? (Enter to keep current, or type new name): ")
		newFilename, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Failed to read input: %v\n", err)
			continue
		}
		newFilename = strings.TrimSpace(newFilename)
		if newFilename != "" && newFilename != filename {
			if !strings.HasSuffix(newFilename, ".md") {
				newFilename += ".md"
			}

			newFilePath := filepath.Join(notesDir, newFilename)
			if _, err := os.Stat(newFilePath); err == nil {
				fmt.Printf("\033[31mFile already exists: %s\033[0m. Keeping original name.\n", newFilename)
			} else {
				if err := os.Rename(filePath, newFilePath); err != nil {
					fmt.Printf("Failed to rename file: %v\n", err)
				} else {
					fmt.Printf("Renamed to: \033[32m%s\033[0m\n", newFilename)
					filename = newFilename
					filePath = newFilePath
				}
			}
		}

		tags, err := promptForTagsWithAutocomplete(notesPath, vaultFile)
		if err != nil {
			fmt.Printf("Failed to read tags: %v\n", err)
		}

		if err := addNoteToVault(notesPath, vaultFile, filename, tags); err != nil {
			fmt.Printf("Failed to add note to vault: %v\n", err)
		}

		// Display preview of the created note
		if err := previewNote(ctx, filename); err != nil {
			fmt.Printf("Failed to display preview: %v\n", err)
		}
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
			fmt.Println("\033[33mExiting image add mode.\033[0m")
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
		fmt.Printf("Added image: \033[32m%s\033[0m\n", destPath)
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
		fmt.Println("\033[33mCancelled.\033[0m")
		return nil
	}

	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(vaultFiles) {
		fmt.Println("\033[31mInvalid option.\033[0m")
		return nil
	}

	targetVaultFile := vaultFiles[idx-1]
	if targetVaultFile == currentVaultFile {
		fmt.Println("\033[33mNote is already in this vault file.\033[0m")
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

	fmt.Printf("Moved \033[32m'%s'\033[0m from \033[32m'%s'\033[0m to \033[32m'%s'\033[0m.\n", filename, currentVaultFile, targetVaultFile)

	return nil
}

// ============================================================================
// Note Action Handlers - Shared between browse and edit menus
// ============================================================================

// actionResult represents the result of handling a note action
type actionResult struct {
	shouldReload   bool
	shouldExit     bool
	shouldAdvance  bool // For browse: move to next note
	shouldRetreat  bool // For browse: move to previous note
	entryUpdated   *noteEntry // Updated entry after reload
}

// handleEditAction opens the note in the editor and shows a preview
func handleEditAction(ctx context.Context, entry noteEntry, filePath string) actionResult {
	if err := openEditor(filePath); err != nil {
		fmt.Printf("Failed to open editor: %v\n", err)
		return actionResult{}
	}
	fmt.Println("\033[32mNote edited.\033[0m")
	if err := previewNote(ctx, entry.filename); err != nil {
		fmt.Printf("Failed to display preview: %v\n", err)
	}
	return actionResult{}
}

// handleRenameAction renames the note file and updates the vault
func handleRenameAction(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry, filterTag string, untaggedOnly bool, currentIndex int) (actionResult, int, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	if err := renameNoteFile(ctx, reader, entry.filename); err != nil {
		fmt.Printf("Failed to rename file: %v\n", err)
		return result, currentIndex, *entries
	}

	// Reload entries
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, currentIndex, *entries
	}

	filteredEntries := filterEntries(newEntries, filterTag, untaggedOnly)
	if len(filteredEntries) == 0 {
		fmt.Println("\nNo more notes to browse.")
		result.shouldExit = true
		return result, currentIndex, newEntries
	}

	// Find the renamed entry or adjust index
	found := false
	for i, e := range filteredEntries {
		if e.filename == entry.filename {
			currentIndex = i
			result.entryUpdated = &filteredEntries[i]
			found = true
			break
		}
	}

	if !found {
		// Entry may have been renamed to something outside the filter
		// Stay at same position or move to last entry
		if currentIndex >= len(filteredEntries) {
			currentIndex = len(filteredEntries) - 1
		}
		if len(filteredEntries) > 0 {
			result.entryUpdated = &filteredEntries[currentIndex]
		}
	}

	result.shouldReload = true
	return result, currentIndex, newEntries
}

// handleTagsAction updates tags for a note
func handleTagsAction(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry, filterTag string, untaggedOnly bool, currentIndex int) (actionResult, int, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	fmt.Printf("Current tags: %s\n", strings.Join(entry.tags, ", "))
	fmt.Print("Enter new tags (Tab for autocomplete, or Enter to keep current): ")

	newTags, err := promptForTagsWithAutocomplete(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to read tags: %v\n", err)
		return result, currentIndex, *entries
	}

	// If user just pressed Enter without typing anything, keep current tags
	var finalTags []string
	if len(newTags) == 0 {
		finalTags = entry.tags
	} else {
		finalTags = newTags
	}

	if err := updateNoteTags(notesPath, vaultFile, entry.filename, finalTags); err != nil {
		fmt.Printf("Failed to update tags: %v\n", err)
		return result, currentIndex, *entries
	}

	fmt.Println("\033[32mTags updated.\033[0m")

	// Reload entries
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, currentIndex, *entries
	}

	filteredEntries := filterEntries(newEntries, filterTag, untaggedOnly)

	// Find the same entry in the reloaded list
	found := false
	for i, e := range filteredEntries {
		if e.filename == entry.filename {
			currentIndex = i
			result.entryUpdated = &filteredEntries[i]
			found = true
			break
		}
	}

	if !found {
		// Note no longer matches filter
		fmt.Println("\033[33mNote no longer matches current filter.\033[0m")
		if len(filteredEntries) == 0 {
			fmt.Println("\033[33mNo more notes match the filter.\033[0m")
			result.shouldExit = true
			return result, currentIndex, newEntries
		}
		// Move to next note at same position, or last if at end
		if currentIndex >= len(filteredEntries) {
			currentIndex = len(filteredEntries) - 1
		}
		if len(filteredEntries) > 0 {
			result.entryUpdated = &filteredEntries[currentIndex]
		}
	}

	result.shouldReload = true
	return result, currentIndex, newEntries
}

// handleDeleteAction deletes a note file and removes it from the vault
func handleDeleteAction(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry, filterTag string, untaggedOnly bool, currentIndex int) (actionResult, int, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)
	notesDir := filepath.Join(notesPath, "notes")
	filePath := filepath.Join(notesDir, entry.filename)

	fmt.Printf("Are you sure you want to delete '%s'? (y/n): ", entry.filename)
	confirm, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Failed to read confirmation: %v\n", err)
		return result, currentIndex, *entries
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		return result, currentIndex, *entries
	}

	if err := os.Remove(filePath); err != nil {
		fmt.Printf("Failed to delete file: %v\n", err)
		return result, currentIndex, *entries
	}

	fmt.Printf("Deleted file: \033[32m%s\033[0m\n", filePath)
	if err := removeNoteFromVault(notesPath, vaultFile, entry.filename); err != nil {
		fmt.Printf("Failed to remove from vault: %v\n", err)
	} else {
		fmt.Println("\033[32mRemoved from vault.\033[0m")
	}

	// Reload entries
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, currentIndex, *entries
	}

	filteredEntries := filterEntries(newEntries, filterTag, untaggedOnly)
	if len(filteredEntries) == 0 {
		fmt.Println("\nNo more notes to browse.")
		result.shouldExit = true
		return result, currentIndex, newEntries
	}

	// Adjust index if needed
	if currentIndex >= len(filteredEntries) {
		currentIndex = len(filteredEntries) - 1
	}
	if len(filteredEntries) > 0 {
		result.entryUpdated = &filteredEntries[currentIndex]
	}

	result.shouldReload = true
	return result, currentIndex, newEntries
}

// handleMoveAction moves a note to another vault file
func handleMoveAction(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry, filterTag string, untaggedOnly bool, currentIndex int) (actionResult, int, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	if err := moveNoteToVault(ctx, reader, entry.filename); err != nil {
		fmt.Printf("Failed to move note: %v\n", err)
		return result, currentIndex, *entries
	}

	// Reload entries
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, currentIndex, *entries
	}

	filteredEntries := filterEntries(newEntries, filterTag, untaggedOnly)
	if len(filteredEntries) == 0 {
		fmt.Println("\nNo more notes to browse.")
		result.shouldExit = true
		return result, currentIndex, newEntries
	}

	// Adjust index if needed
	if currentIndex >= len(filteredEntries) {
		currentIndex = len(filteredEntries) - 1
	}
	if len(filteredEntries) > 0 {
		result.entryUpdated = &filteredEntries[currentIndex]
	}

	result.shouldReload = true
	return result, currentIndex, newEntries
}

// handleViewAction opens the note in a markdown reader
func handleViewAction(ctx context.Context, entry noteEntry, filePath string) actionResult {
	notesPath := getNotesPath(ctx)
	markdownReader, err := config.GetMarkdownReader()
	if err != nil {
		fmt.Printf("Failed to get markdown reader: %v\n", err)
		return actionResult{}
	}

	// Copy file to vault directory so relative image paths work
	vaultDir := notesPath
	tmpFile, err := os.CreateTemp(vaultDir, "view_*.md")
	if err != nil {
		fmt.Printf("Failed to create temporary file: %v\n", err)
		return actionResult{}
	}
	defer os.Remove(tmpFile.Name())

	tmpPath := tmpFile.Name()
	content, readErr := os.ReadFile(filePath)
	if readErr != nil {
		fmt.Printf("Failed to read note: %v\n", readErr)
		tmpFile.Close()
		return actionResult{}
	}

	if _, writeErr := tmpFile.Write(content); writeErr != nil {
		fmt.Printf("Failed to write temporary file: %v\n", writeErr)
		tmpFile.Close()
		return actionResult{}
	}
	tmpFile.Close()

	cmd := exec.Command(markdownReader, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to open markdown reader: %v\n", err)
	}

	return actionResult{}
}

// displayActionMenu shows the available actions for a note
func displayActionMenu(browseMode bool, isHidden bool) {
	fmt.Println("Actions:")
	fmt.Println("  (E)dit note")
	fmt.Println("  (R)ename file")
	fmt.Println("  (T)ags update")
	fmt.Println("  (D)elete note")
	fmt.Println("  (M)ove to another vault file")
	fmt.Println("  (V)iew full content in markdown reader")
	if isHidden {
		fmt.Println("  (U)nhide note")
	} else {
		fmt.Println("  (H)ide note")
	}
	if browseMode {
		fmt.Println("  (N)ext / (P)revious / (Q)uit")
	} else {
		fmt.Println("  (Q)uit")
	}
	fmt.Println()
}

// Simplified action handlers for edit mode (no navigation, exit on destructive actions)

// handleRenameActionEdit is a simplified rename handler for edit mode
func handleRenameActionEdit(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry) (actionResult, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	if err := renameNoteFile(ctx, reader, entry.filename); err != nil {
		fmt.Printf("Failed to rename file: %v\n", err)
		return result, *entries
	}

	// Reload entries
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, *entries
	}

	result.shouldReload = true
	return result, newEntries
}

// handleTagsActionEdit is a simplified tags handler for edit mode
func handleTagsActionEdit(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry) (actionResult, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	fmt.Printf("Current tags: %s\n", strings.Join(entry.tags, ", "))
	fmt.Print("Enter new tags (Tab for autocomplete, or Enter to keep current): ")

	newTags, err := promptForTagsWithAutocomplete(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to read tags: %v\n", err)
		return result, *entries
	}

	// If user just pressed Enter without typing anything, keep current tags
	var finalTags []string
	if len(newTags) == 0 {
		finalTags = entry.tags
	} else {
		finalTags = newTags
	}

	if err := updateNoteTags(notesPath, vaultFile, entry.filename, finalTags); err != nil {
		fmt.Printf("Failed to update tags: %v\n", err)
		return result, *entries
	}

	fmt.Println("\033[32mTags updated.\033[0m")

	// Reload entries
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, *entries
	}

	result.shouldReload = true
	return result, newEntries
}

// handleDeleteActionEdit is a simplified delete handler for edit mode
func handleDeleteActionEdit(ctx context.Context, reader *bufio.Reader, entry noteEntry, filePath string) actionResult {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	fmt.Printf("Are you sure you want to delete '%s'? (y/n): ", entry.filename)
	confirm, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Failed to read confirmation: %v\n", err)
		return result
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		return result
	}

	if err := os.Remove(filePath); err != nil {
		fmt.Printf("Failed to delete file: %v\n", err)
		return result
	}

	fmt.Printf("Deleted file: \033[32m%s\033[0m\n", filePath)
	if err := removeNoteFromVault(notesPath, vaultFile, entry.filename); err != nil {
		fmt.Printf("Failed to remove from vault: %v\n", err)
	} else {
		fmt.Println("\033[32mRemoved from vault.\033[0m")
	}
	fmt.Println("\033[32mNote deleted. Exiting.\033[0m")
	result.shouldExit = true
	return result
}

// handleMoveActionEdit is a simplified move handler for edit mode
func handleMoveActionEdit(ctx context.Context, reader *bufio.Reader, entry noteEntry) actionResult {
	result := actionResult{}

	if err := moveNoteToVault(ctx, reader, entry.filename); err != nil {
		fmt.Printf("Failed to move note: %v\n", err)
		return result
	}

	fmt.Println("\033[33mNote moved to another vault file. Exiting.\033[0m")
	result.shouldExit = true
	return result
}

// handleHideActionEdit toggles the hidden state of a note in edit mode
func handleHideActionEdit(ctx context.Context, entry noteEntry, entries *[]noteEntry) (actionResult, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	// Toggle the hidden state
	entry.hidden = !entry.hidden

	// Update the entry in the entries slice
	for i := range *entries {
		if (*entries)[i].filename == entry.filename {
			(*entries)[i].hidden = entry.hidden
			break
		}
	}

	// Save the updated entries
	if err := saveNotes(notesPath, vaultFile, *entries); err != nil {
		fmt.Printf("Failed to save vault: %v\n", err)
		return result, *entries
	}

	if entry.hidden {
		fmt.Println("\033[33mNote hidden.\033[0m")
	} else {
		fmt.Println("\033[32mNote unhidden.\033[0m")
	}
	result.shouldReload = true
	return result, *entries
}

// handleHideActionBrowse toggles the hidden state of a note in browse mode
func handleHideActionBrowse(ctx context.Context, reader *bufio.Reader, entry noteEntry, entries *[]noteEntry, filterTag string, untaggedOnly bool, currentIndex int) (actionResult, int, []noteEntry) {
	result := actionResult{}
	notesPath := getNotesPath(ctx)
	vaultFile := getVaultFile(ctx)

	// Toggle the hidden state
	entry.hidden = !entry.hidden

	// Update the entry in the entries slice
	for i := range *entries {
		if (*entries)[i].filename == entry.filename {
			(*entries)[i].hidden = entry.hidden
			break
		}
	}

	// Save the updated entries
	if err := saveNotes(notesPath, vaultFile, *entries); err != nil {
		fmt.Printf("Failed to save vault: %v\n", err)
		return result, currentIndex, *entries
	}

	if entry.hidden {
		fmt.Println("\033[33mNote hidden.\033[0m")
	} else {
		fmt.Println("\033[32mNote unhidden.\033[0m")
	}

	// Reload entries and filter
	newEntries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		fmt.Printf("Failed to reload notes: %v\n", err)
		return result, currentIndex, *entries
	}

	filteredEntries := filterEntries(newEntries, filterTag, untaggedOnly)
	
	// Note may have been hidden/unhidden, adjust index if needed
	// If hidden, it may no longer match the filter (if filter is for untagged only)
	// If unhidden, it may now match the filter
	if len(filteredEntries) == 0 {
		fmt.Println("\033[33mNo more notes to browse.\033[0m")
		result.shouldExit = true
		return result, currentIndex, newEntries
	}

	// Try to find the entry in the filtered list
	found := false
	for i, e := range filteredEntries {
		if e.filename == entry.filename {
			currentIndex = i
			result.entryUpdated = &filteredEntries[i]
			found = true
			break
		}
	}

	if !found {
		// Entry not in filtered list (may have been hidden or filter changed)
		if currentIndex >= len(filteredEntries) {
			currentIndex = len(filteredEntries) - 1
		}
		if len(filteredEntries) > 0 {
			result.entryUpdated = &filteredEntries[currentIndex]
		}
	}

	result.shouldReload = true
	return result, currentIndex, newEntries
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

	// Filter entries using helper function
	filteredEntries := filterEntries(entries, filterTag, untaggedOnly)

	if len(filteredEntries) == 0 {
		if untaggedOnly {
			fmt.Println("\033[33mNo untagged notes found.\033[0m")
		} else if filterTag != "" {
			fmt.Printf("\033[33mNo notes found with tag: %s\033[0m\n", filterTag)
		} else {
			fmt.Println("\033[33mNo notes found.\033[0m")
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
		fmt.Printf("\n\033[1m[%d/%d] %s\033[0m", i+1, len(filteredEntries), entry.filename)
		if entry.hidden {
			fmt.Print(" \033[33m[HIDDEN]\033[0m")
		}
		fmt.Println()
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
		displayActionMenu(true, entry.hidden)

		fmt.Print("Select action: ")
		action, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read action: %w", err)
		}
		action = strings.ToLower(strings.TrimSpace(action))

		var result actionResult
		switch action {
		case "e", "edit":
			result = handleEditAction(ctx, entry, filePath)
		case "r", "rename":
			result, i, entries = handleRenameAction(ctx, reader, entry, &entries, filterTag, untaggedOnly, i)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				if result.entryUpdated != nil {
					entry = *result.entryUpdated
				}
			}
			continue // Skip the advance at the end of loop
		case "t", "tags":
			result, i, entries = handleTagsAction(ctx, reader, entry, &entries, filterTag, untaggedOnly, i)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				if result.entryUpdated != nil {
					entry = *result.entryUpdated
				}
			}
			continue // Skip the advance at the end of loop
		case "d", "delete":
			result, i, entries = handleDeleteAction(ctx, reader, entry, &entries, filterTag, untaggedOnly, i)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				if result.entryUpdated != nil {
					entry = *result.entryUpdated
				}
			}
			continue // Skip the advance at the end of loop
		case "m", "move":
			result, i, entries = handleMoveAction(ctx, reader, entry, &entries, filterTag, untaggedOnly, i)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				if result.entryUpdated != nil {
					entry = *result.entryUpdated
				}
			}
			continue // Skip the advance at the end of loop
		case "h", "hide":
			result, i, entries = handleHideActionBrowse(ctx, reader, entry, &entries, filterTag, untaggedOnly, i)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				if result.entryUpdated != nil {
					entry = *result.entryUpdated
				}
			}
			continue // Skip the advance at the end of loop
		case "u", "unhide":
			result, i, entries = handleHideActionBrowse(ctx, reader, entry, &entries, filterTag, untaggedOnly, i)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				filteredEntries = filterEntries(entries, filterTag, untaggedOnly)
				if result.entryUpdated != nil {
					entry = *result.entryUpdated
				}
			}
			continue // Skip the advance at the end of loop
		case "v", "view":
			result = handleViewAction(ctx, entry, filePath)
		case "n", "next":
			i++
			if i >= len(filteredEntries) {
				fmt.Println("\033[33mEnd of notes.\033[0m")
				return nil
			}
			continue
		case "p", "prev", "previous":
			if i > 0 {
				i--
			} else {
				fmt.Println("\033[33mAlready at first note.\033[0m")
			}
			continue
		case "q", "quit", "exit":
			fmt.Println("\033[33mExiting browse mode.\033[0m")
			return nil
		}

		fmt.Println()
		i++
	}

	fmt.Println("\033[33mEnd of notes.\033[0m")
	return nil
}

// editNoteInteractive displays a menu for a single note by filename
func editNoteInteractive(ctx context.Context, filename string) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	vaultFile := getVaultFile(ctx)
	reader := bufio.NewReader(os.Stdin)

	// Check if the note exists in the current vault file
	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	var entry noteEntry
	found := false
	for _, e := range entries {
		if e.filename == filename {
			entry = e
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("note '%s' not found in current vault file '%s'", filename, vaultFile)
	}

	filePath := filepath.Join(notesDir, filename)

	// Check if the note file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("note file does not exist: %s", filePath)
	}

	fmt.Printf("\n\033[1m%s\033[0m", entry.filename)
	if entry.hidden {
		fmt.Print(" \033[33m[HIDDEN]\033[0m")
	}
	fmt.Println()
	if len(entry.tags) > 0 {
		fmt.Printf("Tags: \033[36m%s\033[0m\n", strings.Join(entry.tags, ", "))
	} else {
		fmt.Println("Tags: \033[33m(none)\033[0m")
	}
	fmt.Println(strings.Repeat("-", 50))

	// Read and display note content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
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

	for {
		// Show action menu
		displayActionMenu(false, entry.hidden)

		fmt.Print("Select action: ")
		action, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read action: %w", err)
		}
		action = strings.ToLower(strings.TrimSpace(action))

		switch action {
		case "e", "edit":
			handleEditAction(ctx, entry, filePath)
		case "r", "rename":
			// For edit mode, we use a simplified rename handler
			oldFilename := filename
			result, newEntries := handleRenameActionEdit(ctx, reader, entry, &entries)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				entries = newEntries
				// Find the renamed entry - look for entry with matching tags
				found = false
				for _, e := range entries {
					if e.filename != oldFilename && tagsMatch(e.tags, entry.tags) {
						entry = e
						filename = e.filename
						filePath = filepath.Join(notesDir, filename)
						found = true
						break
					}
				}
				// If not found by tags, the filename may not have changed or tags changed
				if !found {
					for _, e := range entries {
						if e.filename == oldFilename {
							entry = e
							found = true
							break
						}
					}
				}
				if !found && len(entries) > 0 {
					// Fallback: use first entry
					entry = entries[0]
					filename = entries[0].filename
					filePath = filepath.Join(notesDir, filename)
					found = true
				}
				if !found {
					fmt.Println("\033[33mNote renamed successfully. Exiting.\033[0m")
					return nil
				}
			}
		case "t", "tags":
			// For edit mode, we use a simplified tags handler
			result, newEntries := handleTagsActionEdit(ctx, reader, entry, &entries)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				entries = newEntries
				// Find the same entry in the reloaded list
				found = false
				for _, e := range entries {
					if e.filename == entry.filename {
						entry = e
						found = true
						break
					}
				}
				if !found {
					fmt.Println("\033[33mNote not found after reload. Exiting.\033[0m")
					return nil
				}
			}
		case "d", "delete":
			// For edit mode, delete exits after successful deletion
			result := handleDeleteActionEdit(ctx, reader, entry, filePath)
			if result.shouldExit {
				return nil
			}
		case "m", "move":
			// For edit mode, move exits after successful move
			result := handleMoveActionEdit(ctx, reader, entry)
			if result.shouldExit {
				return nil
			}
		case "h", "hide":
			// For edit mode, hide exits after successful hide
			result, newEntries := handleHideActionEdit(ctx, entry, &entries)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				entries = newEntries
			}
		case "u", "unhide":
			// For edit mode, unhide exits after successful unhide
			result, newEntries := handleHideActionEdit(ctx, entry, &entries)
			if result.shouldExit {
				return nil
			}
			if result.shouldReload {
				entries = newEntries
			}
		case "v", "view":
			handleViewAction(ctx, entry, filePath)
		case "q", "quit", "exit":
			fmt.Println("\033[33mExiting edit mode.\033[0m")
			return nil
		}

		fmt.Println()
	}
}

// previewNote displays a preview of a note's content in the CLI
func previewNote(ctx context.Context, filename string) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	vaultFile := getVaultFile(ctx)

	// Load entries to get tags for this note
	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	var tags []string
	for _, entry := range entries {
		if entry.filename == filename {
			tags = entry.tags
			break
		}
	}

	filePath := filepath.Join(notesDir, filename)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read note: %w", err)
	}

	fmt.Println()
	fmt.Printf("\033[1mPreview: %s\033[0m\n", filename)
	if len(tags) > 0 {
		fmt.Printf("Tags: \033[36m%s\033[0m\n", strings.Join(tags, ", "))
	} else {
		fmt.Println("Tags: \033[33m(none)\033[0m")
	}
	fmt.Println(strings.Repeat("-", 50))

	contentStr := string(content)
	if len(contentStr) > 2000 {
		fmt.Println(contentStr[:2000])
		fmt.Println("\n... (content truncated)")
	} else if len(contentStr) == 0 {
		fmt.Println("\033[33m(Empty note)\033[0m")
	} else {
		fmt.Println(contentStr)
	}
	fmt.Println()

	return nil
}

// filterEntries filters entries by tag or untagged status
func filterEntries(entries []noteEntry, filterTag string, untaggedOnly bool) []noteEntry {
	var filtered []noteEntry
	for _, entry := range entries {
		if untaggedOnly {
			// Consider note untagged if it has no tags or only empty tag
			if len(entry.tags) == 0 || (len(entry.tags) == 1 && entry.tags[0] == "") {
				filtered = append(filtered, entry)
			}
		} else if filterTag == "" {
			filtered = append(filtered, entry)
		} else {
			for _, tag := range entry.tags {
				if tagMatches(tag, filterTag) {
					filtered = append(filtered, entry)
					break
				}
			}
		}
	}
	return filtered
}

// renameVaultFile renames the current vault file to a new name
func renameVaultFile(ctx context.Context, reader *bufio.Reader) (string, error) {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return "", fmt.Errorf("notes path not configured")
	}

	currentVaultFile := getVaultFile(ctx)

	fmt.Print("Enter new vault file name (or Enter to cancel): ")
	newName, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	newName = strings.TrimSpace(newName)
	if newName == "" {
		fmt.Println("\033[33mCancelled.\033[0m")
		return currentVaultFile, nil
	}

	if newName == currentVaultFile {
		fmt.Println("\033[33mVault file name unchanged.\033[0m")
		return currentVaultFile, nil
	}

	if !strings.HasSuffix(newName, ".vault") {
		newName += ".vault"
	}

	oldVaultFilePath := filepath.Join(notesPath, currentVaultFile)
	newVaultFilePath := filepath.Join(notesPath, newName)

	if _, err := os.Stat(newVaultFilePath); err == nil {
		fmt.Printf("\033[31mVault file already exists: %s\033[0m\n", newName)
		return currentVaultFile, nil
	}

	if err := os.Rename(oldVaultFilePath, newVaultFilePath); err != nil {
		return "", fmt.Errorf("failed to rename vault file: %w", err)
	}

	if err := config.SetVaultFile(newName); err != nil {
		return "", fmt.Errorf("failed to update config: %w", err)
	}

	fmt.Printf("Renamed vault file from \033[32m'%s'\033[0m to \033[32m'%s'\033[0m.\n", currentVaultFile, newName)
	return newName, nil
}

// deleteVaultFile deletes a vault file and all notes referenced in it
func deleteVaultFile(ctx context.Context, reader *bufio.Reader) (string, error) {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return "", fmt.Errorf("notes path not configured")
	}

	currentVaultFile := getVaultFile(ctx)
	notesDir := filepath.Join(notesPath, "notes")

	fmt.Println()
	fmt.Printf("\033[31mWARNING: This will delete the vault file '%s' and all notes listed in it.\033[0m\n", currentVaultFile)
	fmt.Println("This action cannot be undone.")
	fmt.Println()

	// Ask for confirmation - require entering the vault file name twice
	fmt.Printf("Enter vault file name to confirm deletion (or 'cancel' to abort): ")
	confirm1, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read confirmation: %w", err)
	}
	confirm1 = strings.TrimSpace(confirm1)

	if confirm1 == "cancel" || confirm1 == "" {
		fmt.Println("\033[33mDeletion cancelled.\033[0m")
		return currentVaultFile, nil
	}

	if confirm1 != currentVaultFile {
		fmt.Printf("\033[31mVault file name does not match. Expected '%s', got '%s'.\033[0m\n", currentVaultFile, confirm1)
		fmt.Println("\033[33mDeletion cancelled.\033[0m")
		return currentVaultFile, nil
	}

	fmt.Printf("Enter vault file name again to confirm: ")
	confirm2, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read confirmation: %w", err)
	}
	confirm2 = strings.TrimSpace(confirm2)

	if confirm2 != currentVaultFile {
		fmt.Printf("\033[31mVault file name does not match. Expected '%s', got '%s'.\033[0m\n", currentVaultFile, confirm2)
		fmt.Println("\033[33mDeletion cancelled.\033[0m")
		return currentVaultFile, nil
	}

	// Load entries to get the list of notes to delete
	entries, err := loadNotes(notesPath, currentVaultFile)
	if err != nil {
		return "", fmt.Errorf("failed to load vault file: %w", err)
	}

	// Delete all note files referenced in the vault
	deletedCount := 0
	for _, entry := range entries {
		filePath := filepath.Join(notesDir, entry.filename)
		if _, err := os.Stat(filePath); err == nil {
			if err := os.Remove(filePath); err != nil {
				fmt.Printf("Failed to delete note file '%s': %v\n", entry.filename, err)
			} else {
				deletedCount++
			}
		}
	}

	// Delete the vault file itself
	vaultFilePath := filepath.Join(notesPath, currentVaultFile)
	if err := os.Remove(vaultFilePath); err != nil {
		return "", fmt.Errorf("failed to delete vault file: %w", err)
	}

	fmt.Printf("Deleted \033[32m%d note(s)\033[0m and vault file \033[32m'%s'\033[0m.\n", deletedCount, currentVaultFile)

	// List remaining vault files and switch to one automatically
	vaultFiles, err := config.ListVaultFiles(notesPath)
	if err != nil {
		return "", err
	}

	if len(vaultFiles) == 0 {
		// No vault files left, create default one
		defaultVault := "notes.vault"
		defaultVaultPath := filepath.Join(notesPath, defaultVault)
		if err := os.WriteFile(defaultVaultPath, []byte{}, 0644); err != nil {
			return "", fmt.Errorf("failed to create default vault file: %w", err)
		}
		if err := config.SetVaultFile(defaultVault); err != nil {
			return "", fmt.Errorf("failed to set default vault file: %w", err)
		}
		fmt.Printf("No vault files remaining. Created default vault file: \033[32m%s\033[0m\n", defaultVault)
		return defaultVault, nil
	}

	// Switch to the first available vault file
	selectedVaultFile := vaultFiles[0]
	if err := config.SetVaultFile(selectedVaultFile); err != nil {
		return "", fmt.Errorf("failed to set vault file: %w", err)
	}
	fmt.Printf("Switched to vault file: \033[32m%s\033[0m\n", selectedVaultFile)

	return selectedVaultFile, nil
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
		fmt.Println("  (O)pen vault file in editor")
		fmt.Println("  (R)ename vault file")
		fmt.Println("  (D)elete vault file")
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
		case "o":
			vaultFilePath := filepath.Join(notesPath, currentVaultFile)
			if err := openEditor(vaultFilePath); err != nil {
				fmt.Printf("Failed to open editor: %v\n", err)
			} else {
				fmt.Println("\033[32mVault file opened in editor.\033[0m")
			}
		case "r":
			return renameVaultFile(ctx, reader)
		case "d":
			// Delete vault file
			newVaultFile, err := deleteVaultFile(ctx, reader)
			if err != nil {
				fmt.Printf("Error deleting vault file: %v\n", err)
			} else {
				return newVaultFile, nil
			}
		case "n":
			fmt.Print("Enter new vault file name: ")
			newName, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read input: %w", err)
			}
			newName = strings.TrimSpace(newName)
			if newName == "" {
				fmt.Println("\033[31mVault file name cannot be empty.\033[0m")
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
			fmt.Printf("Created and switched to vault file: \033[32m%s\033[0m\n", newName)
			return newName, nil
		default:
			var idx int
			if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(vaultFiles) {
				fmt.Println("\033[31mInvalid option.\033[0m")
				continue
			}

			selectedVaultFile := vaultFiles[idx-1]
			if err := config.SetVaultFile(selectedVaultFile); err != nil {
				fmt.Printf("Failed to set vault file: %v\n", err)
				continue
			}
			fmt.Printf("Switched to vault file: \033[32m%s\033[0m\n", selectedVaultFile)
			return selectedVaultFile, nil
		}
	}
}

// manageTags provides a menu for tag management operations
func manageTags(ctx context.Context, reader *bufio.Reader) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	vaultFile := getVaultFile(ctx)

	for {
		// Load all entries and collect unique tags
		entries, err := loadNotes(notesPath, vaultFile)
		if err != nil {
			return err
		}

		// Collect unique tags with counts
		tagCounts := make(map[string]int)
		var tagOrder []string
		seenTags := make(map[string]bool)

		for _, entry := range entries {
			for _, tag := range entry.tags {
				if tag == "" {
					continue
				}
				if !seenTags[tag] {
					seenTags[tag] = true
					tagOrder = append(tagOrder, tag)
				}
				tagCounts[tag]++
			}
		}

		fmt.Println("\nTag Management")
		fmt.Println("==============")
		if len(tagOrder) == 0 {
			fmt.Println("\033[33mNo tags found.\033[0m")
		} else {
			fmt.Println("\nExisting tags:")
			for _, tag := range tagOrder {
				fmt.Printf("  - %s (%d notes)\n", tag, tagCounts[tag])
			}
		}
		fmt.Println()
		fmt.Println("  (M)ove notes with tag to another vault")
		fmt.Println("  (A)rchive notes with tag")
		fmt.Println("  (R)ename tag")
		fmt.Println("  (D)elete notes with tag")
		fmt.Println("  (Q)uit")
		fmt.Println()

		fmt.Print("Select option: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(input)) {
		case "q", "quit":
			fmt.Println("\033[33mExiting tag management.\033[0m")
			return nil
		case "m", "move":
			if err := moveNotesByTag(ctx, reader); err != nil {
				fmt.Printf("Error moving notes: %v\n", err)
			}
		case "r", "rename":
			if err := renameTag(ctx, reader); err != nil {
				fmt.Printf("Error renaming tag: %v\n", err)
			}
		case "d", "delete":
			if err := deleteNotesByTag(ctx, reader); err != nil {
				fmt.Printf("Error deleting notes: %v\n", err)
			}
		default:
			fmt.Println("\033[31mInvalid option.\033[0m")
		}
	}
}

// moveNotesByTag moves all notes with a specific tag to another vault file
func moveNotesByTag(ctx context.Context, reader *bufio.Reader) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	currentVaultFile := getVaultFile(ctx)

	fmt.Print("Enter tag to move: ")
	tagInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	tagInput = strings.TrimSpace(tagInput)
	if tagInput == "" {
		fmt.Println("\033[31mTag cannot be empty.\033[0m")
		return nil
	}

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

	fmt.Print("Enter vault file number to move notes to (or Enter to cancel): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		fmt.Println("\033[33mCancelled.\033[0m")
		return nil
	}

	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(vaultFiles) {
		fmt.Println("\033[31mInvalid option.\033[0m")
		return nil
	}

	targetVaultFile := vaultFiles[idx-1]
	if targetVaultFile == currentVaultFile {
		fmt.Println("\033[33mNote is already in this vault file.\033[0m")
		return nil
	}

	// Load current entries
	entries, err := loadNotes(notesPath, currentVaultFile)
	if err != nil {
		return err
	}

	// Find notes with the specified tag
	var notesToMove []noteEntry
	var notesToKeep []noteEntry
	for _, entry := range entries {
		hasTag := false
		for _, tag := range entry.tags {
			if tagMatches(tag, tagInput) {
				hasTag = true
				break
			}
		}
		if hasTag {
			notesToMove = append(notesToMove, entry)
		} else {
			notesToKeep = append(notesToKeep, entry)
		}
	}

	if len(notesToMove) == 0 {
		fmt.Printf("No notes found with tag: %s\n", tagInput)
		return nil
	}

	fmt.Printf("Found %d note(s) with tag '%s'.\n", len(notesToMove), tagInput)
	fmt.Printf("Move these notes from '%s' to '%s'? (y/n): ", currentVaultFile, targetVaultFile)
	confirm, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("\033[33mCancelled.\033[0m")
		return nil
	}

	// Remove from current vault
	if err := saveNotes(notesPath, currentVaultFile, notesToKeep); err != nil {
		return err
	}

	// Add to target vault
	targetEntries, err := loadNotes(notesPath, targetVaultFile)
	if err != nil {
		return err
	}
	targetEntries = append(targetEntries, notesToMove...)
	if err := saveNotes(notesPath, targetVaultFile, targetEntries); err != nil {
		return err
	}

	fmt.Printf("Moved \033[32m%d note(s)\033[0m with tag \033[32m'%s'\033[0m from \033[32m'%s'\033[0m to \033[32m'%s'\033[0m.\n", len(notesToMove), tagInput, currentVaultFile, targetVaultFile)

	return nil
}

// renameTag renames a tag across all notes in the current vault
func renameTag(ctx context.Context, reader *bufio.Reader) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	vaultFile := getVaultFile(ctx)

	fmt.Print("Enter current tag name: ")
	oldTag, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	oldTag = strings.TrimSpace(oldTag)
	if oldTag == "" {
		fmt.Println("\033[31mTag cannot be empty.\033[0m")
		return nil
	}

	fmt.Print("Enter new tag name: ")
	newTag, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	newTag = strings.TrimSpace(newTag)
	if newTag == "" {
		fmt.Println("\033[31mNew tag cannot be empty.\033[0m")
		return nil
	}

	if oldTag == newTag {
		fmt.Println("\033[33mTags are the same.\033[0m")
		return nil
	}

	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	// Count and rename all matching tags (exact + nested)
	// e.g., renaming "robotics" to "robot" changes:
	//   - "robotics" → "robot"
	//   - "robotics/fpga" → "robot/fpga"
	//   - "robotics/fpga/sensor" → "robot/fpga/sensor"
	affectedCount := 0
	for i := range entries {
		for j, tag := range entries[i].tags {
			if tag == oldTag {
				// Exact match
				entries[i].tags[j] = newTag
				affectedCount++
			} else if strings.HasPrefix(tag, oldTag+"/") {
				// Nested tag - replace the prefix
				entries[i].tags[j] = newTag + tag[len(oldTag):]
				affectedCount++
			}
		}
	}

	if affectedCount == 0 {
		fmt.Printf("No notes found with tag: %s\n", oldTag)
		return nil
	}

	if err := saveNotes(notesPath, vaultFile, entries); err != nil {
		return err
	}

	fmt.Printf("Renamed tag \033[32m'%s'\033[0m to \033[32m'%s'\033[0m in \033[32m%d occurrence(s)\033[0m.\n", oldTag, newTag, affectedCount)

	return nil
}

// deleteNotesByTag deletes all notes with a specific tag
func deleteNotesByTag(ctx context.Context, reader *bufio.Reader) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	vaultFile := getVaultFile(ctx)
	notesDir := filepath.Join(notesPath, "notes")

	fmt.Print("Enter tag to delete notes with: ")
	tagInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	tagInput = strings.TrimSpace(tagInput)
	if tagInput == "" {
		fmt.Println("\033[31mTag cannot be empty.\033[0m")
		return nil
	}

	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return err
	}

	// Find notes with the specified tag
	var notesToDelete []noteEntry
	var notesToKeep []noteEntry
	for _, entry := range entries {
		hasTag := false
		for _, tag := range entry.tags {
			if tagMatches(tag, tagInput) {
				hasTag = true
				break
			}
		}
		if hasTag {
			notesToDelete = append(notesToDelete, entry)
		} else {
			notesToKeep = append(notesToKeep, entry)
		}
	}

	if len(notesToDelete) == 0 {
		fmt.Printf("No notes found with tag: %s\n", tagInput)
		return nil
	}

	fmt.Printf("Found %d note(s) with tag '%s'.\n", len(notesToDelete), tagInput)
	fmt.Println("This will permanently delete the following files:")
	for _, entry := range notesToDelete {
		fmt.Printf("  - %s\n", entry.filename)
	}
	fmt.Println()
	fmt.Print("Are you sure you want to delete these notes? (y/n): ")
	confirm, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("\033[33mCancelled.\033[0m")
		return nil
	}

	// Delete files and save vault
	for _, entry := range notesToDelete {
		filePath := filepath.Join(notesDir, entry.filename)
		if err := os.Remove(filePath); err != nil {
			fmt.Printf("Warning: failed to delete file %s: %v\n", filePath, err)
		}
	}

	if err := saveNotes(notesPath, vaultFile, notesToKeep); err != nil {
		return err
	}

	fmt.Printf("Deleted \033[32m%d note(s)\033[0m with tag \033[32m'%s'\033[0m.\n", len(notesToDelete), tagInput)

	return nil
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
			// Skip hidden notes in read/export
			if entry.hidden {
				continue
			}
			if filterTag == "" {
				entries = append(entries, entry)
			} else {
				for _, tag := range entry.tags {
					if tagMatches(tag, filterTag) {
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

// listNotes lists all notes in the vault, optionally filtered by tag
func listNotes(ctx context.Context, filterTag string) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}
	vaultFile := getVaultFile(ctx)

	entries, err := loadNotes(notesPath, vaultFile)
	if err != nil {
		return fmt.Errorf("failed to load notes: %w", err)
	}

	// Filter entries by tag if specified
	var filteredEntries []noteEntry
	if filterTag == "" {
		filteredEntries = entries
	} else {
		for _, entry := range entries {
			for _, tag := range entry.tags {
				if tagMatches(tag, filterTag) {
					filteredEntries = append(filteredEntries, entry)
					break
				}
			}
		}
	}

	if len(filteredEntries) == 0 {
		if filterTag != "" {
			fmt.Printf("No notes found with tag: %s\n", filterTag)
		} else {
			fmt.Println("No notes found.")
		}
		return nil
	}

	// Display the notes
	for _, entry := range filteredEntries {
		tagsStr := ""
		if len(entry.tags) > 0 {
			tagsStr = fmt.Sprintf(" [%s]", strings.Join(entry.tags, ","))
		}
		fmt.Printf("  %s%s\n", entry.filename, tagsStr)
	}

	return nil
}

// catNote displays the contents of a specific note file
func catNote(ctx context.Context, filename string) error {
	notesPath := getNotesPath(ctx)
	if notesPath == "" {
		return fmt.Errorf("notes path not configured")
	}

	notesDir := filepath.Join(notesPath, "notes")
	filePath := filepath.Join(notesDir, filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("note not found: %s", filename)
	}

	// Read and display the file contents
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read note: %w", err)
	}

	fmt.Print(string(data))
	return nil
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

	// Get current working directory for output path resolution
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Get export command from config (prompts if not set)
	exportCommand, err := config.GetExportCommand()
	if err != nil {
		return fmt.Errorf("failed to get export command: %w", err)
	}

	// Prompt for output path
	reader := bufio.NewReader(os.Stdin)
	fmt.Println()
	fmt.Print("Enter output PDF path: ")
	outputPath, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read output path: %w", err)
	}
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	// Resolve relative paths against current working directory
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(cwd, outputPath)
	}

	// Replace placeholders in the export command
	exportCommand = strings.ReplaceAll(exportCommand, "{input}", tmpPath)
	exportCommand = strings.ReplaceAll(exportCommand, "{output}", outputPath)

	// Parse the command and arguments
	cmdParts := strings.Fields(exportCommand)
	if len(cmdParts) == 0 {
		return fmt.Errorf("export command cannot be empty")
	}

	cmdName := cmdParts[0]
	cmdArgs := cmdParts[1:]

	// Check if command exists
	if _, err := exec.LookPath(cmdName); err != nil {
		return fmt.Errorf("%s is not installed: %w", cmdName, err)
	}

	// Execute the export command
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Dir = filepath.Dir(notesVaultFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to export PDF (ensure %s and required tools are installed): %w", cmdName, err)
	}

	fmt.Printf("Exported PDF to: \033[32m%s\033[0m\n", outputPath)
	return nil
}

// ============================================================================
// Main Menu
// ============================================================================

func mainMenu(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		// Reorganize notes by tag when main menu loads
		if err := collectNotesByTag(ctx); err != nil {
			fmt.Printf("Error organizing notes by tag: %v\n", err)
		}

		fmt.Printf("notetree version %s\n", appVersion)
		fmt.Printf("Current vault file: \033[1m%s\033[0m\n", getVaultFile(ctx))
		fmt.Println("  (A)dd notes")
		fmt.Println("  (B)rowse notes")
		fmt.Println("  (R)ead notes")
		fmt.Println("  (E)xport note PDF")
		fmt.Println("  (T)ag management")
		fmt.Println("  (V)ault management")
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
			}
		case "b":
			notesPath := getNotesPath(ctx)
			vaultFile := getVaultFile(ctx)
			tagInput, err := promptForSingleTagWithAutocomplete(notesPath, vaultFile, "Enter tag to filter (or Enter for all, 'u' for untagged only): ")
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
			notesPath := getNotesPath(ctx)
			vaultFile := getVaultFile(ctx)
			tagInput, err := promptForSingleTagWithAutocomplete(notesPath, vaultFile, "Enter tag to filter (or Enter to read all): ")
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else {
				fmt.Println()
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
			notesPath := getNotesPath(ctx)
			vaultFile := getVaultFile(ctx)
			tagInput, err := promptForSingleTagWithAutocomplete(notesPath, vaultFile, "Enter tag to filter (or Enter to export all): ")
			if err != nil {
				fmt.Printf("Error reading tag: %v\n", err)
			} else if err := exportNotes(ctx, strings.TrimSpace(tagInput)); err != nil {
				fmt.Printf("Error exporting notes: %v\n", err)
			}
		case "v":
			if newVaultFile, err := manageVaultFiles(ctx, reader); err != nil {
				fmt.Printf("Error managing vault files: %v\n", err)
			} else {
				ctx = context.WithValue(ctx, vaultFileKey, newVaultFile)
			}
		case "t":
			if err := manageTags(ctx, reader); err != nil {
				fmt.Printf("Error managing tags: %v\n", err)
			}
		case "q":
			fmt.Println("\033[32mGoodbye!\033[0m")
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
				Name:        "ls",
				Usage:       "List all notes in the vault",
				Description: "Lists all notes registered in the vault file.\nUse -t flag to filter by tag.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "tag",
						Aliases: []string{"t"},
						Usage:   "Filter notes by tag",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					filterTag := c.String("tag")
					return listNotes(ctx, filterTag)
				},
			},
			{
				Name:        "cat",
				Aliases:     []string{"c"},
				Usage:       "Display contents of a note",
				ArgsUsage:   "[filename]",
				Description: "Displays the contents of a specific note file.\nIf no filename is provided, prompts for filename with autocomplete.",
				Action: func(ctx context.Context, c *cli.Command) error {
					filename := c.Args().First()
					if filename == "" {
						notesPath := getNotesPath(ctx)
						if notesPath == "" {
							return fmt.Errorf("notes path not configured")
						}
						vaultFile := getVaultFile(ctx)
						var err error
						filename, err = promptForFilenameWithAutocomplete(notesPath, vaultFile)
						if err != nil {
							return fmt.Errorf("failed to get filename: %w", err)
						}
						if filename == "" {
							return fmt.Errorf("filename required")
						}
					}
					if !strings.HasSuffix(filename, ".md") {
						filename += ".md"
					}
					return catNote(ctx, filename)
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
			{
				Name:        "edit",
				Aliases:     []string{"e"},
				Usage:       "Edit a note by filename",
				ArgsUsage:   "<filename>",
				Description: "Edit a specific note by its filename. The note must exist in the current vault file.",
				Action: func(ctx context.Context, c *cli.Command) error {
					filename := c.Args().First()
					if filename == "" {
						return fmt.Errorf("filename required, usage: notetree edit <filename>")
					}
					if !strings.HasSuffix(filename, ".md") {
						filename += ".md"
					}
					return editNoteInteractive(ctx, filename)
				},
			},
			{
				Name:        "tags",
				Aliases:     []string{"tag"},
				Usage:       "Manage tags",
				Description: "Manage tags across all notes: move notes by tag, rename tags, or delete notes by tag.",
				Action: func(ctx context.Context, c *cli.Command) error {
					reader := bufio.NewReader(os.Stdin)
					return manageTags(ctx, reader)
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
