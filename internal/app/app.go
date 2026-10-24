package app

import (
	"io"

	"glimpse/internal/pager"
	"glimpse/internal/ui"
)

// App coordinates asynchronous ingestion with viewport-sized rendering.
// Frame scheduling, input handling, and terminal lifecycle are intentionally
// left for the TUI implementation.
type App struct {
	Document *pager.Document
	Renderer ui.Renderer
}

// Ingest reads a stream into chunked storage. Rendering can independently
// take snapshots while this is running. The caller owns cancellation for
// readers that may block.
func (a *App) Ingest(input io.Reader) error {
	_, err := a.Document.ReadFrom(input)
	return err
}
