package app

import (
	"fmt"
	"io"

	"glimpse/internal/keybind"
	"glimpse/internal/pager"
	"glimpse/internal/ui"
)

// Options contains startup settings resolved by the CLI configuration layer.
// Its zero value selects cmdheight = 0, with the status row initially hidden.
type Options struct {
	CmdHeight     ui.CmdHeight
	ChunkSize     int
	Follow        bool
	Keybinds      map[string][]string
	SmartCase     bool
	IgnoreCase    bool
	CaseSensitive bool
}

// App coordinates asynchronous ingestion with viewport-sized rendering.
type App struct {
	Document   *pager.Document
	Renderer   ui.Renderer
	UI         *ui.Model
	Keys       *keybind.Matcher
	SearchKeys *keybind.Matcher
	SmartCase  bool
	IgnoreCase bool
	follow     bool
}

// New creates document storage and view state without initializing a terminal.
func New(options Options) (*App, error) {
	model, err := ui.NewModel(options.CmdHeight)
	if err != nil {
		return nil, err
	}
	keys, err := keybind.Compile(options.Keybinds)
	if err != nil {
		return nil, err
	}
	searchKeys, err := keybind.CompileSearch(options.Keybinds)
	if err != nil {
		return nil, fmt.Errorf("keybinds: %w", err)
	}
	if options.ChunkSize == 0 {
		options.ChunkSize = pager.DefaultChunkSize
	}
	if options.ChunkSize < 0 {
		return nil, fmt.Errorf("chunk-size must be positive")
	}
	return &App{
		Document:   pager.NewDocumentWithChunkSize(options.ChunkSize),
		UI:         model,
		Keys:       keys,
		SearchKeys: searchKeys,
		SmartCase:  options.SmartCase && !options.CaseSensitive,
		IgnoreCase: options.IgnoreCase && !options.CaseSensitive,
		follow:     options.Follow,
	}, nil
}

// Ingest reads a stream into chunked storage. Rendering can independently
// take snapshots while this is running. The caller owns cancellation for
// readers that may block.
func (a *App) Ingest(input io.Reader) error {
	_, err := a.Document.ReadFrom(input)
	return err
}
