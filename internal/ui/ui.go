package ui

// Viewport describes the portion of the input needed for one screen frame.
// Storage and stream ingestion must not depend on the size of a logical line.
type Viewport struct {
	StartOffset int64
	Width       int
	Height      int
}

// Frame contains only the text and footer needed for one screen update.
type Frame struct {
	Layout Layout
	Lines  []string
	Status string
}

// Renderer draws a bounded frame, independently of how the document is stored.
type Renderer interface {
	Render(frame Frame) error
}
