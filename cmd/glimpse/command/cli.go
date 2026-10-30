package command

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"glimpse/internal/app"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version can be set at build time:
//
//	go build -ldflags "-X glimpse/cmd/glimpse/command.Version=v1.0.0" ./cmd/glimpse
//
// Otherwise it comes from the module version and VCS revision in build info.
var Version string

func version() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	result := info.Main.Version
	// Local builds get a VCS pseudo-version; the revision says more.
	if result == "" || result == "(devel)" || strings.HasPrefix(result, "v0.0.0-") {
		result = "devel"
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision != "" {
		result += " (" + revision[:min(12, len(revision))]
		if modified == "true" {
			result += ", modified"
		}
		result += ")"
	}
	return result
}

func writeCompletion(root *cobra.Command, out io.Writer, shell string) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(out, true)
	case "zsh":
		return root.GenZshCompletion(out)
	case "fish":
		return root.GenFishCompletion(out, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(out)
	}
	return fmt.Errorf("unknown completion shell %q (use bash, zsh, fish, or powershell)", shell)
}

// checkConfig validates the configuration as startup does, without a terminal
// or input, and additionally rejects keys that startup would silently ignore.
func checkConfig(config *viper.Viper, cmd *cobra.Command) error {
	used := config.ConfigFileUsed()
	cfg, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if unknown := cfg.unknownKeys(); len(unknown) > 0 {
		return fmt.Errorf("%s: unknown option(s): %s", used, strings.Join(unknown, ", "))
	}
	options, err := readOptions(config, cmd)
	if err != nil {
		return err
	}
	application, err := app.New(options)
	if err != nil {
		return err
	}
	if err := application.Document.Close(); err != nil {
		return err
	}
	if used == "" {
		used = "no config file found; defaults"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: OK\n", used)
	return nil
}

// passThrough behaves like cat when output is redirected, as less does, so
// glimpse works as $PAGER in pipelines.
func passThrough(cmd *cobra.Command, args []string) error {
	input, _, err := openInput(cmd.InOrStdin(), args)
	if err != nil {
		return err
	}
	defer input.Close()
	_, err = io.Copy(cmd.OutOrStdout(), input)
	if err != nil {
		return fmt.Errorf("copy input: %w", err)
	}
	return nil
}
