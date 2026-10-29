package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Filename styles name a file in the statusline, always with $HOME as ~:
// the full path, the path with each directory shortened to its first letter
// (like vim's pathshorten()), or only the last component (vim's :t).
const (
	FilenameFull  = "full"
	FilenameShort = "short"
	FilenameTail  = "tail"
)

// ValidFilenameStyle reports whether style is a known filename style; empty
// selects the default, full.
func ValidFilenameStyle(style string) bool {
	return style == "" || slices.Contains([]string{FilenameFull, FilenameShort, FilenameTail}, style)
}

// FormatFilename names path in style, resolving it to an absolute path first
// so a file shows the same wherever glimpse was started.
func FormatFilename(path, style string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	home, _ := os.UserHomeDir()
	return formatFilename(path, home, style)
}

func formatFilename(path, home, style string) string {
	if style == FilenameTail {
		return filepath.Base(path)
	}
	home = strings.TrimSuffix(home, "/")
	if home != "" && strings.HasPrefix(path, home+"/") {
		path = "~" + path[len(home):]
	}
	if style != FilenameShort {
		return path
	}
	parts := strings.Split(path, "/")
	for i, part := range parts[:len(parts)-1] {
		if part == "" || part == "~" {
			continue
		}
		n := 1
		if part[0] == '.' && len(part) > 1 {
			n = 2
		}
		_, size := utf8.DecodeRuneInString(part[n-1:])
		parts[i] = part[:n-1+size]
	}
	return strings.Join(parts, "/")
}
