package search

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const MaxQueryBytes = 16 * 1024
const maxHistoryEntries = 1000

// History uses newline-delimited JSON strings, so queries cannot inject records.
// Each append is a single O_APPEND write, preserving other running viewers'
// entries instead of replacing the file with an outdated in-memory snapshot.
type History struct {
	Path    string
	Entries []string // Oldest first, deduplicated on load/add.
}

func DefaultHistoryPath() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "glimpse", "search-history"), nil
}

func (h *History) Load() error {
	file, err := os.Open(h.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	// Only read a bounded tail of a potentially old, very large history file.
	start := max(0, info.Size()-1024*1024)
	reader := bufio.NewReader(io.NewSectionReader(file, start, info.Size()-start))
	if start > 0 {
		if _, err := reader.ReadString('\n'); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaxQueryBytes*6+16)
	for scanner.Scan() {
		var query string
		if json.Unmarshal(scanner.Bytes(), &query) == nil && query != "" && len(query) <= MaxQueryBytes {
			h.remember(query)
		}
	}
	return scanner.Err()
}

func (h *History) remember(query string) {
	for i, entry := range h.Entries {
		if entry == query {
			h.Entries = append(h.Entries[:i], h.Entries[i+1:]...)
			break
		}
	}
	h.Entries = append(h.Entries, query)
	if len(h.Entries) > maxHistoryEntries {
		h.Entries = h.Entries[len(h.Entries)-maxHistoryEntries:]
	}
}

func (h *History) Add(query string) error {
	if query == "" || len(query) > MaxQueryBytes {
		return nil
	}
	h.remember(query)
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(h.Path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	data, err := json.Marshal(query)
	if err != nil {
		file.Close()
		return err
	}
	// A leading separator also recovers cleanly from a truncated final record
	// left by an interrupted writer. Blank lines are ignored when loading.
	_, err = file.Write(append(append([]byte{'\n'}, data...), '\n'))
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}
