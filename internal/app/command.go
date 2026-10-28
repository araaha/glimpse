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
		enabled := true
		if explicit {
			var err error
			enabled, err = strconv.ParseBool(value)
			if err != nil {
				return commandResult{}, fmt.Errorf("set %s requires true or false", name)
			}
		} else if strings.HasPrefix(name, "no-") {
			name, enabled = strings.TrimPrefix(name, "no-"), false
		}
		switch name {
		case "smart-case":
			a.SmartCase = enabled
			if enabled {
				a.IgnoreCase = false
			}
		case "ignore-case":
			a.IgnoreCase = enabled
			if enabled {
				a.SmartCase = false
			}
		case "case-sensitive":
			if enabled {
				a.SmartCase, a.IgnoreCase = false, false
			} else {
				a.IgnoreCase = true
				a.SmartCase = false
			}
		case "incremental":
			a.IncrementalSearch = enabled
		case "highlight":
			a.HighlightSearch = enabled
		case "wrap":
			a.Wrap = enabled
		case "line-numbers":
			a.LineNumbers = enabled
		default:
			return commandResult{}, fmt.Errorf("unknown option %q", name)
		}
		return commandResult{message: fmt.Sprintf("%s=%t", name, enabled)}, nil
	}
	return commandResult{}, fmt.Errorf("unknown command %q", input)
}
