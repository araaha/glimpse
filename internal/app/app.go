package app

import (
	"fmt"
	"io"

	"github.com/araaha/glimpse/internal/keybind"
	"github.com/araaha/glimpse/internal/pager"
	"github.com/araaha/glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
)

// Options contains startup settings resolved by the CLI configuration layer.
// Its zero value selects cmdheight = 0, with status plus an on-demand prompt row.
type Options struct {
	CmdHeight         ui.CmdHeight
	ChunkSize         int
	Follow            bool
	FollowName        bool
	Label             string // statusline name for the input instead of its path
	Keybinds          map[string][]string
	SmartCase         bool
	IgnoreCase        bool
	CaseSensitive     bool
	Literal           bool // match searches literally instead of as regular expressions
	IncrementalSearch bool
	NoSearchHighlight bool
	NoWrap            bool
	LineNumbers       bool
	HexColors         bool
	Storage           pager.StorageOptions
	MetadataBudget    int64
	MetadataQuota     int64
	AmbiguousWidth    int
	Statusline        ui.StatuslineConfig
	SearchHighlight   ui.Colors
	LineNumberStyle   ui.Colors
	UnprintableChar   string
	UnprintableStyle  ui.Colors
	JumpTarget        JumpTarget
	Run               []RunCommand
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
	Magic                         bool // searches are regular expressions; \V makes them literal
	IncrementalSearch             bool
	HighlightSearch               bool
	Wrap                          bool
	LineNumbers                   bool
	HexColors                     bool // drawn only when truecolor is true
	truecolor                     bool
	metadataBudget, metadataQuota int64
	wideAmbiguous                 bool
	follow, followName            bool
	Label                         string // statusline name for the input; empty shows its path
	Statusline                    *ui.Statusline
	SearchHighlight               tcell.Style
	LineNumberStyle               tcell.Style
	UnprintableStyle              tcell.Style
	unprintableChar               string
	tempDir                       string // spool-dir, also used for spilled indexes
	FilenameStyle                 string // statusline filename style: full, short, or tail
	JumpTarget                    JumpTarget
	runs                          map[keybind.Action]RunCommand
}

// New creates document storage and view state without initializing a terminal.
func New(options Options) (*App, error) {
	statusline, err := ui.NewStatusline(options.Statusline)
	if err != nil {
		return nil, err
	}
	highlight, err := options.SearchHighlight.Style(tcell.StyleDefault)
	if err != nil {
		return nil, fmt.Errorf("search highlight: %w", err)
	}
	// Line numbers default to the palette's bright black, so they follow the
	// terminal theme and stay quieter than the text.
	numbers, _ := (ui.Colors{FG: "bright-black"}).Style(tcell.StyleDefault)
	numbers, err = options.LineNumberStyle.Style(numbers)
	if err != nil {
		return nil, fmt.Errorf("line numbers: %w", err)
	}
	unprintable, err := options.UnprintableStyle.Style(tcell.StyleDefault)
	if err != nil {
		return nil, fmt.Errorf("unprintable: %w", err)
	}
	if options.UnprintableChar != "" {
		if err := ui.ValidateCellText(options.UnprintableChar, options.AmbiguousWidth == 2); err != nil {
			return nil, fmt.Errorf("unprintable char: %w", err)
		}
	}
	model, err := ui.NewModel(options.CmdHeight)
	if err != nil {
		return nil, err
	}
	extra, runs, err := runBindings(options.Run)
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	keys, err := keybind.Compile(options.Keybinds, extra...)
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
		Magic:             !options.Literal,
		IncrementalSearch: options.IncrementalSearch,
		HighlightSearch:   !options.NoSearchHighlight,
		Wrap:              !options.NoWrap,
		LineNumbers:       options.LineNumbers,
		HexColors:         options.HexColors,
		metadataBudget:    options.MetadataBudget,
		metadataQuota:     options.MetadataQuota,
		tempDir:           options.Storage.SpoolDir,
		wideAmbiguous:     options.AmbiguousWidth == 2,
		follow:            options.Follow,
		followName:        options.FollowName,
		Label:             options.Label,
		Statusline:        statusline,
		SearchHighlight:   highlight,
		JumpTarget:        options.JumpTarget,
		runs:              runs,
		LineNumberStyle:   numbers,
		UnprintableStyle:  unprintable,
		unprintableChar:   options.UnprintableChar,
		FilenameStyle:     options.Statusline.FilenameStyle,
	}, nil
}

// Ingest reads a stream into chunked storage. Rendering can independently
// take snapshots while this is running. The caller owns cancellation for
// readers that may block.
func (a *App) Ingest(input io.Reader) error {
	_, err := a.Document.ReadFrom(input)
	return err
}
