package ui

import "fmt"

// CmdHeight controls which rows are reserved below the document viewport.
type CmdHeight int

const (
	// CmdHeightToggle reserves a status row that can be hidden in the view.
	CmdHeightToggle CmdHeight = -1
	// CmdHeightStatusOnly reserves one shared status/prompt row.
	CmdHeightStatusOnly CmdHeight = 0

	// CmdHeightWithSearch reserves a status row above a separate search row.
	CmdHeightWithSearch CmdHeight = 1

	DefaultCmdHeight = CmdHeightStatusOnly
)

// Validate rejects modes other than -1, 0, and 1.
func (h CmdHeight) Validate() error {
	switch h {
	case CmdHeightToggle, CmdHeightStatusOnly, CmdHeightWithSearch:
		return nil
	default:
		return fmt.Errorf("cmdheight must be -1, 0, or 1 (got %d)", h)
	}
}

// Model holds view-local state. The UI event loop owns this state, independently
// of the goroutine ingesting document bytes.
type Model struct {
	cmdHeight     CmdHeight
	statusVisible bool
	searchVisible bool
}

// SetSearchVisible temporarily uses the footer for a prompt or search message.
// In modes 0/-1 it replaces the status row; mode 1 keeps both separate rows.
func (m *Model) SetSearchVisible(visible bool) { m.searchVisible = visible }

// NewModel creates a view with its statusline visible in every mode.
func NewModel(cmdHeight CmdHeight) (*Model, error) {
	if err := cmdHeight.Validate(); err != nil {
		return nil, err
	}
	return &Model{cmdHeight: cmdHeight, statusVisible: true}, nil
}

// ToggleStatusline only changes visibility in mode -1. Modes 0 and 1 always
// reserve their footer rows, except when no terminal space is available.
func (m *Model) ToggleStatusline() {
	if m.cmdHeight == CmdHeightToggle {
		m.statusVisible = !m.statusVisible
	}
}

// Layout separates the document area from footer rows. Row positions are
// zero-based; -1 means the row is hidden or there is no room for it. Search
// temporarily replaces the footer in modes 0/-1; mode 1 reserves both rows.
type Layout struct {
	Body      Viewport
	StatusRow int
	SearchRow int
}

// Layout computes the available document size after reserving footer rows.
// With only one terminal row, the search row takes priority over the statusline.
func (m *Model) Layout(width, height int) Layout {
	layout := Layout{
		Body:      Viewport{Width: max(0, width), Height: max(0, height)},
		StatusRow: -1,
		SearchRow: -1,
	}
	if (m.cmdHeight == CmdHeightWithSearch || m.searchVisible) && layout.Body.Height > 0 {
		layout.Body.Height--
		layout.SearchRow = layout.Body.Height
	}
	if (m.cmdHeight == CmdHeightWithSearch || (m.statusVisible && !m.searchVisible)) && layout.Body.Height > 0 {
		layout.Body.Height--
		layout.StatusRow = layout.Body.Height
	}
	return layout
}
