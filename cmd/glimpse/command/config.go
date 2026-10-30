package command

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"glimpse/internal/ui"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// fileConfig is the whole configuration as Viper merges it from flags,
// environment variables, and TOML. Tables with fixed keys reject unknown ones;
// the plain sections collect them in Unknown, which only --check-config reports.
type fileConfig struct {
	CmdHeight  int        `mapstructure:"cmdheight"`
	ChunkSize  int        `mapstructure:"chunk-size"`
	Follow     bool       `mapstructure:"follow"`
	FollowName bool       `mapstructure:"follow-name"`
	JumpTarget jumpTarget `mapstructure:"jump-target"`
	Label      string     `mapstructure:"label"`
	Search     struct {
		SmartCase      bool           `mapstructure:"smart-case"`
		IgnoreCase     bool           `mapstructure:"ignore-case"`
		CaseSensitive  bool           `mapstructure:"case-sensitive"`
		Incremental    bool           `mapstructure:"incremental"`
		Highlight      bool           `mapstructure:"highlight"`
		Regex          bool           `mapstructure:"regex"`
		HighlightStyle ui.Colors      `mapstructure:"highlight-style"`
		Unknown        map[string]any `mapstructure:",remain"`
	} `mapstructure:"search"`
	Display struct {
		AmbiguousWidth  int       `mapstructure:"ambiguous-width"`
		Wrap            bool      `mapstructure:"wrap"`
		LineNumbers     bool      `mapstructure:"line-numbers"`
		HexColors       bool      `mapstructure:"hex-colors"`
		LineNumberStyle ui.Colors `mapstructure:"line-number-style"`
		Unprintable     struct {
			Char string `mapstructure:"char"`
			FG   string `mapstructure:"fg"`
			BG   string `mapstructure:"bg"`
		} `mapstructure:"unprintable"`
		Unknown map[string]any `mapstructure:",remain"`
	} `mapstructure:"display"`
	Storage struct {
		MemoryBudget   int64          `mapstructure:"memory-budget"`
		SpoolQuota     int64          `mapstructure:"spool-quota"`
		MetadataBudget int64          `mapstructure:"metadata-budget"`
		MetadataQuota  int64          `mapstructure:"metadata-quota"`
		Retention      string         `mapstructure:"retention"`
		SpoolDir       string         `mapstructure:"spool-dir"`
		Unknown        map[string]any `mapstructure:",remain"`
	} `mapstructure:"storage"`
	Statusline struct {
		Left           []string                `mapstructure:"left"`
		Right          []string                `mapstructure:"right"`
		SeparatorOpen  *string                 `mapstructure:"separator-open"`
		SeparatorClose *string                 `mapstructure:"separator-close"`
		FG             string                  `mapstructure:"fg"`
		BG             string                  `mapstructure:"bg"`
		FilenameStyle  string                  `mapstructure:"filename-style"`
		Styles         map[string]sectionStyle `mapstructure:"styles"`
	} `mapstructure:"statusline"`
	Keybinds map[string][]string `mapstructure:"keybinds"`
	Run      []struct {
		Keys       []string `mapstructure:"keys"`
		Command    string   `mapstructure:"command"`
		Background bool     `mapstructure:"background"`
	} `mapstructure:"run"`
	Unknown map[string]any `mapstructure:",remain"`
}

// sectionStyle colors one statusline section; the mode section also takes a
// table per mode, e.g. [statusline.styles.mode] search = { bg = "yellow" }.
type sectionStyle struct {
	FG    string               `mapstructure:"fg"`
	BG    string               `mapstructure:"bg"`
	Modes map[string]ui.Colors `mapstructure:",remain"`
}

// jumpTarget is a string so TOML may write it as a number (0.5) or text.
type jumpTarget string

// flagKeys binds each flag that has a config key to that key.
var flagKeys = map[string]string{
	"cmdheight": "cmdheight", "chunk-size": "chunk-size", "follow": "follow",
	"follow-name": "follow-name", "jump-target": "jump-target", "label": "label",
	"smart-case": "search.smart-case", "ignore-case": "search.ignore-case",
	"case-sensitive": "search.case-sensitive", "incremental-search": "search.incremental",
	"highlight-search": "search.highlight", "regex": "search.regex",
	"memory-budget": "storage.memory-budget", "spool-quota": "storage.spool-quota",
	"metadata-budget": "storage.metadata-budget", "metadata-quota": "storage.metadata-quota",
	"retention": "storage.retention", "spool-dir": "storage.spool-dir",
	"ambiguous-width": "display.ambiguous-width", "wrap": "display.wrap",
	"line-numbers": "display.line-numbers", "hex-colors": "display.hex-colors",
}

// decodeConfig unmarshals the merged configuration. Types are strict, except
// that strings (as environment variables provide them) may set booleans and
// integers, and whole floats integers.
func decodeConfig(config *viper.Viper) (fileConfig, error) {
	var cfg fileConfig
	err := config.Unmarshal(&cfg, func(c *mapstructure.DecoderConfig) {
		c.ErrorUnused = true
		c.WeaklyTypedInput = false
		c.DecodeHook = mapstructure.DecodeHookFuncType(parseScalar)
	})
	if err != nil {
		return fileConfig{}, configError(err)
	}
	return cfg, nil
}

func parseScalar(_, to reflect.Type, data any) (any, error) {
	if to == reflect.TypeFor[jumpTarget]() {
		switch data.(type) {
		case string, int, int64, float64:
			return jumpTarget(fmt.Sprint(data)), nil
		}
		return data, nil
	}
	// TOML numbers with a fraction would otherwise be truncated.
	if number, ok := data.(float64); ok && (to.Kind() == reflect.Int || to.Kind() == reflect.Int64) {
		if number != math.Trunc(number) {
			return nil, fmt.Errorf("must be an integer, not %v", number)
		}
		return int64(number), nil
	}
	text, ok := data.(string)
	if !ok {
		return data, nil
	}
	switch to.Kind() {
	case reflect.Bool:
		value, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("must be true or false, not %q", text)
		}
		return value, nil
	case reflect.Int, reflect.Int64:
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("must be an integer, not %q", text)
		}
		return value, nil
	}
	return data, nil
}

// configError lists each decoding failure on its own line, without
// mapstructure's "decoding failed" preamble.
func configError(err error) error {
	var lines []string
	var walk func(error)
	walk = func(err error) {
		// Nested failures carry their full dotted names, so the tables that
		// join them add nothing.
		var joined interface{ Unwrap() []error }
		if errors.As(err, &joined) {
			for _, err := range joined.Unwrap() {
				walk(err)
			}
			return
		}
		lines = append(lines, "config: "+err.Error())
	}
	walk(err)
	return errors.New(strings.Join(lines, "\n"))
}

// unknownKeys returns the dotted keys that no part of glimpse reads.
func (cfg fileConfig) unknownKeys() []string {
	var keys []string
	for prefix, table := range map[string]map[string]any{
		"": cfg.Unknown, "search.": cfg.Search.Unknown, "display.": cfg.Display.Unknown, "storage.": cfg.Storage.Unknown,
	} {
		for key := range table {
			keys = append(keys, prefix+key)
		}
	}
	sort.Strings(keys)
	return keys
}
