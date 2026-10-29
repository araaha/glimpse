package app

import (
	"fmt"
	"strconv"
	"strings"

	"glimpse/internal/keybind"
)

type commandResult struct {
	quit    bool
	action  keybind.Action
	line    int
	message string
}

// optionName resolves a boolean :set option or its vim short name, returning
// "" for anything else.
func optionName(name string) string {
	if full, ok := map[string]string{"nu": "number", "hls": "hlsearch", "is": "incsearch", "ic": "ignorecase", "scs": "smartcase"}[name]; ok {
		return full
	}
	for _, option := range setOptions {
		if option == name {
			return name
		}
	}
	return ""
}

// hexColorsUnsupported explains why enabled hex colors draw nothing.
const hexColorsUnsupported = "hex-colors needs a truecolor terminal (COLORTERM=truecolor)"

// Commands deliberately have no shell escape: document input cannot execute
// programs, and command parsing never interpolates strings into a shell.
func (a *App) command(input string) (commandResult, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return commandResult{}, nil
	}
	if len(fields) == 1 {
		switch fields[0] {
		case "q", "quit":
			return commandResult{quit: true}, nil
		case "top":
			return commandResult{action: keybind.Top}, nil
		case "bottom":
			return commandResult{action: keybind.Bottom}, nil
		}
		if line, err := strconv.Atoi(fields[0]); err == nil && line > 0 {
			return commandResult{line: line}, nil
		}
	}
	if len(fields) == 2 && fields[0] == "set" {
		name, value, explicit := strings.Cut(fields[1], "=")
		if name == "spool-quota" && explicit {
			quota, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return commandResult{}, fmt.Errorf("spool-quota requires an integer byte count")
			}
			if err := a.Document.SetQuota(quota); err != nil {
				return commandResult{}, err
			}
			return commandResult{message: fmt.Sprintf("spool-quota=%d", quota)}, nil
		}
		if name == "jump-target" && explicit {
			target, err := ParseJumpTarget(value)
			if err != nil {
				return commandResult{}, err
			}
			a.JumpTarget = target
			return commandResult{message: "jump-target=" + target.String()}, nil
		}
		if explicit {
			return commandResult{}, fmt.Errorf("set %s takes no value", name)
		}
		// Boolean options use vim's names: a no prefix disables, and the short
		// names nu, hls, is, ic, and scs are accepted.
		option, enabled := optionName(name), true
		if option == "" && strings.HasPrefix(name, "no") {
			option, enabled = optionName(strings.TrimPrefix(name, "no")), false
		}
		switch option {
		case "smartcase":
			a.SmartCase = enabled
			if enabled {
				a.IgnoreCase = false
			}
		case "ignorecase":
			// noignorecase matches case exactly, also overriding smartcase.
			a.IgnoreCase, a.SmartCase = enabled, false
		case "incsearch":
			a.IncrementalSearch = enabled
		case "hlsearch":
			a.HighlightSearch = enabled
		case "magic":
			a.Magic = enabled
		case "wrap":
			a.Wrap = enabled
		case "number":
			a.LineNumbers = enabled
		case "hex-colors":
			a.HexColors = enabled
			if enabled && !a.truecolor {
				return commandResult{message: hexColorsUnsupported}, nil
			}
		default:
			return commandResult{}, fmt.Errorf("unknown option %q", name)
		}
		if !enabled {
			option = "no" + option
		}
		return commandResult{message: option}, nil
	}
	return commandResult{}, fmt.Errorf("unknown command %q", input)
}
