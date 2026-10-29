package command

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"glimpse/internal/app"
	"glimpse/internal/ui"

	"github.com/spf13/viper"
)

func readAppearance(config *viper.Viper, options *app.Options) error {
	for key := range config.GetStringMap("statusline") {
		switch key {
		case "left", "right", "separator-open", "separator-close", "fg", "bg", "styles", "filename-style":
		default:
			return fmt.Errorf("unknown statusline option %q", key)
		}
	}
	for _, path := range []string{"statusline", "statusline.styles", "search.highlight-style", "display.line-number-style", "display.unprintable"} {
		if raw := config.Get(path); raw != nil {
			if _, ok := raw.(map[string]any); !ok {
				return fmt.Errorf("%s must be a table", path)
			}
		}
	}
	readString := func(key string) (string, error) {
		if !config.IsSet(key) {
			return "", nil
		}
		value, ok := config.Get(key).(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", key)
		}
		return value, nil
	}
	// readColors reads a { fg, bg } table; nested names other tables allowed
	// inside it, which the caller reads itself.
	readColors := func(path string, nested ...string) (ui.Colors, error) {
		if raw := config.Get(path); raw != nil {
			table, ok := raw.(map[string]any)
			if !ok {
				return ui.Colors{}, fmt.Errorf("%s must be a table", path)
			}
			for key := range table {
				if key != "fg" && key != "bg" && !slices.Contains(nested, key) {
					return ui.Colors{}, fmt.Errorf("unknown color option %s.%s", path, key)
				}
			}
		}
		fg, err := readString(path + ".fg")
		if err != nil {
			return ui.Colors{}, err
		}
		bg, err := readString(path + ".bg")
		return ui.Colors{FG: fg, BG: bg}, err
	}
	for _, side := range []struct {
		key    string
		target *[]string
	}{{"left", &options.Statusline.Left}, {"right", &options.Statusline.Right}} {
		key := "statusline." + side.key
		if !config.IsSet(key) {
			continue
		}
		switch raw := config.Get(key).(type) {
		case []any:
			*side.target = make([]string, 0, len(raw))
			for _, item := range raw {
				value, ok := item.(string)
				if !ok {
					return fmt.Errorf("%s must contain only section names", key)
				}
				*side.target = append(*side.target, value)
			}
		case []string:
			*side.target = append(make([]string, 0, len(raw)), raw...)
		case string:
			if os.Getenv("GLIMPSE_STATUSLINE_"+strings.ToUpper(side.key)) == "" {
				return fmt.Errorf("%s must be an array of section names", key)
			}
			*side.target = strings.Split(raw, ",")
			for i, value := range *side.target {
				(*side.target)[i] = strings.TrimSpace(value)
			}
		default:
			return fmt.Errorf("%s must be an array of section names", key)
		}
	}
	var err error
	for _, separator := range []struct {
		key    string
		target **string
	}{{"separator-open", &options.Statusline.SeparatorOpen}, {"separator-close", &options.Statusline.SeparatorClose}} {
		if !config.IsSet("statusline." + separator.key) {
			continue
		}
		value, err := readString("statusline." + separator.key)
		if err != nil {
			return err
		}
		*separator.target = &value
	}
	options.Statusline.FG, err = readString("statusline.fg")
	if err != nil {
		return err
	}
	options.Statusline.BG, err = readString("statusline.bg")
	if err != nil {
		return err
	}
	options.Statusline.FilenameStyle, err = readString("statusline.filename-style")
	if err != nil {
		return err
	}
	if !ui.ValidFilenameStyle(options.Statusline.FilenameStyle) {
		return fmt.Errorf("statusline.filename-style must be tail, short, or full")
	}
	options.Statusline.Styles = make(map[string]ui.Colors)
	var modes []string
	for _, mode := range ui.Modes {
		modes = append(modes, strings.ToLower(mode))
	}
	// Only the mode section has per-mode tables.
	nested := func(section string) []string {
		if strings.EqualFold(section, ui.StatusMode) {
			return modes
		}
		return nil
	}
	for name := range config.GetStringMap("statusline.styles") {
		colors, err := readColors("statusline.styles."+name, nested(name)...)
		if err != nil {
			return err
		}
		options.Statusline.Styles[name] = colors
	}
	// Known keys are queried explicitly so AutomaticEnv also works without a
	// corresponding TOML table (Viper cannot enumerate environment-only keys).
	for _, name := range []string{ui.StatusMode, ui.StatusFilename, ui.StatusSearchResults, ui.StatusLineNumbers} {
		colors, err := readColors("statusline.styles."+name, nested(name)...)
		if err != nil {
			return err
		}
		if colors.FG != "" || colors.BG != "" {
			options.Statusline.Styles[name] = colors
		}
	}
	// Each mode's colors override the mode section's, e.g.
	// [statusline.styles.mode] search = { bg = "yellow" }.
	options.Statusline.ModeStyles = make(map[string]ui.Colors)
	for _, mode := range modes {
		colors, err := readColors("statusline.styles.mode." + mode)
		if err != nil {
			return err
		}
		if colors.FG != "" || colors.BG != "" {
			options.Statusline.ModeStyles[mode] = colors
		}
	}
	options.SearchHighlight, err = readColors("search.highlight-style")
	if err != nil {
		return err
	}
	options.LineNumberStyle, err = readColors("display.line-number-style")
	if err != nil {
		return err
	}
	for key := range config.GetStringMap("display.unprintable") {
		if key != "char" && key != "fg" && key != "bg" {
			return fmt.Errorf("unknown unprintable option display.unprintable.%s", key)
		}
	}
	if options.UnprintableChar, err = readString("display.unprintable.char"); err != nil {
		return err
	}
	if options.UnprintableStyle.FG, err = readString("display.unprintable.fg"); err != nil {
		return err
	}
	options.UnprintableStyle.BG, err = readString("display.unprintable.bg")
	return err
}
