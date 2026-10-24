package ui

// Viewport describes the portion of the input needed for one screen frame.
// Storage and stream ingestion must not depend on the size of a logical line.
type Viewport struct {
	StartOffset int64
	Width       int
	Height      int
}

// Renderer turns a bounded input window into terminal output. Implementations
// should render only the viewport and coalesce repeated updates; they should
// not parse or format an entire logical line on every frame.
type Renderer interface {
	Render(data []byte, viewport Viewport) error
}
