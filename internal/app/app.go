package app

import (
	"fmt"
	"io"

	"glimpse/internal/keybind"
	"glimpse/internal/pager"
	"glimpse/internal/ui"
)

// Options contains startup settings resolved by the CLI configuration layer.
// Its zero value selects cmdheight = 0, with status plus an on-demand prompt row.
type Options struct {
	CmdHeight         ui.CmdHeight
	ChunkSize         int
	Follow            bool
	Keybinds          map[string][]string
	SmartCase         bool
	IgnoreCase        bool
	CaseSensitive     bool
	IncrementalSearch bool
	NoSearchHighlight bool
	Storage           pager.StorageOptions
	MetadataBudget    int64
	MetadataQuota     int64
	AmbiguousWidth    int
}

// App coordinates asynchronous ingestion with viewport-sized rendering.
type App struct {
	Document                      *pager.Document
	Renderer                      ui.Renderer
	UI                            *ui.Model
	Keys                          *keybind.Matcher
	SearchKeys                    *keybind.Matcher
	SmartCase                     bool
	IgnoreCase                    bool
	IncrementalSearch             bool
	HighlightSearch               bool
	metadataBudget, metadataQuota int64
	wideAmbiguous                 bool
	follow                        bool
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
	defaults := pager.DefaultStorageOptions()
	if options.Storage.MemoryBudget == 0 {
		options.Storage.MemoryBudget = defaults.MemoryBudget
	}
	if options.Storage.SpoolQuota == 0 {
		options.Storage.SpoolQuota = defaults.SpoolQuota
	}
	if options.MetadataBudget == 0 {
		options.MetadataBudget = 8 << 20
	}
	if options.MetadataQuota == 0 {
		options.MetadataQuota = 256 << 20
	}
	if options.MetadataBudget < 16<<10 || options.MetadataQuota < 56 {
		return nil, fmt.Errorf("metadata-budget must be at least 16384 and metadata-quota at least 56 bytes")
	}
	if options.AmbiguousWidth == 0 {
		options.AmbiguousWidth = 1
	}
	if options.AmbiguousWidth != 1 && options.AmbiguousWidth != 2 {
		return nil, fmt.Errorf("ambiguous-width must be 1 or 2")
	}
	document, err := pager.NewDocumentWithOptions(options.ChunkSize, options.Storage)
	if err != nil {
		return nil, err
	}
	return &App{
		Document:          document,
		UI:                model,
		Keys:              keys,
		SearchKeys:        searchKeys,
		SmartCase:         options.SmartCase && !options.CaseSensitive,
		IgnoreCase:        options.IgnoreCase && !options.CaseSensitive,
		IncrementalSearch: options.IncrementalSearch,
		HighlightSearch:   !options.NoSearchHighlight,
		metadataBudget:    options.MetadataBudget,
		metadataQuota:     options.MetadataQuota,
		wideAmbiguous:     options.AmbiguousWidth == 2,
		follow:            options.Follow,
	}, nil
}

// Ingest reads a stream into chunked storage. Rendering can independently
// take snapshots while this is running. The caller owns cancellation for
// readers that may block.
func (a *App) Ingest(input io.Reader) error {
	_, err := a.Document.ReadFrom(input)
	return err
}
