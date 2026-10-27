package ui

import (
	"glimpse/internal/content"

	"github.com/gdamore/tcell/v3"
)

// Viewport describes the portion of the input needed for one screen frame.
// Storage and stream ingestion must not depend on the size of a logical line.
type Viewport struct {
	StartOffset int64
	Width       int
	Height      int
}

// Frame contains only the text and footer needed for one screen update.
type Frame struct {
	Layout         Layout
	Lines          []string
	Status         string
	StatusBar      *StatusBar
	StatusState    StatusState
	Reading        bool
	Indexing       bool
	SeekingEnd     bool
	Follow         bool
	DocumentLength int64
	HighlightStyle tcell.Style
	Search         string
	SearchActive   bool
	SearchCursor   int // Rune index in Search, including the direction prefix.
	Highlights     []Highlight
	Styled         [][]Glyph
	WideAmbiguous  bool
}

// Glyphs are safe text plus explicit attributes, never terminal escape strings.
type Glyph struct {
	Text           string
	Column, Width  int
	Style          content.Style
	UnderlineColor content.Color
	Start, End     int64
}

type Highlight struct{ Row, Column, Width int }

// Renderer draws a bounded frame, independently of how the document is stored.
type Renderer interface {
	Render(frame Frame) error
}
