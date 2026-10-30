// Package command defines Glimpse's Cobra command tree and Viper configuration.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"glimpse/internal/app"
	"glimpse/internal/pager"
	"glimpse/internal/ui"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// NewRoot creates a root command with an isolated Viper instance, which keeps
// command construction predictable in tests and future embedded use.
func NewRoot() *cobra.Command {
	return newRoot(func(ctx context.Context, application *app.App, input io.ReadCloser, name string) error {
		return application.Run(ctx, input, name)
	}, requireInteractiveTerminal)
}

type runner func(context.Context, *app.App, io.ReadCloser, string) error

func newRoot(run runner, checkTerminal func(io.Writer) error) *cobra.Command {
	config := viper.New()
	var configFile string

	root := &cobra.Command{
		Use:           "glimpse [FILE]",
		Short:         "A terminal pager",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shell, _ := cmd.Flags().GetString("completion"); shell != "" {
				return writeCompletion(cmd.Root(), cmd.OutOrStdout(), shell)
			}
			if check, _ := cmd.Flags().GetBool("check-config"); check {
				return checkConfig(config, cmd)
			}
			if checkTerminal != nil {
				if err := checkTerminal(cmd.OutOrStdout()); errors.Is(err, errStdoutNotTerminal) {
					return passThrough(cmd, args)
				} else if err != nil {
					return err
				}
			}
			options, err := readOptions(config, cmd)
			if err != nil {
				return err
			}
			if options.Label == "" && (len(args) == 0 || args[0] == "-") {
				// man(1) names the page it pipes, e.g. "ls(1)".
				options.Label = os.Getenv("MAN_PN")
			}
			application, err := app.New(options)
			if err != nil {
				return err
			}
			input, name, err := openInput(cmd.InOrStdin(), args)
			if err != nil {
				return err
			}
			if checkTerminal != nil {
				awaitPipedPrompt(cmd.Context(), input)
			}
			return run(cmd.Context(), application, input, name)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Cobra's hidden completion command has none of the root's flags
			// and needs no configuration.
			if cmd != cmd.Root() {
				return nil
			}
			return loadConfig(config, cmd, configFile)
		},
	}

	root.SetVersionTemplate("glimpse {{.Version}}\n")
	root.PersistentFlags().StringVar(&configFile, "config", "", "TOML config file (default: $XDG_CONFIG_HOME/glimpse/glimpse.toml)")
	root.Flags().Bool("check-config", false, "validate the configuration (including unknown keys) and exit")
	root.Flags().String("completion", "", "print a completion script for bash, zsh, fish, or powershell and exit")
	root.Flags().String("label", "", "name shown for the input in the statusline (stdin defaults to $MAN_PN)")
	root.Flags().Int("chunk-size", 64*1024, "input storage chunk size in bytes")
	root.Flags().Bool("follow", false, "follow the end of incoming input (toggle with F)")
	root.Flags().Bool("follow-name", false, "watch a file by name, reopening it after rotation or replacement, instead of by descriptor")
	root.Flags().Bool("smart-case", false, "ignore search case unless the query contains an uppercase letter")
	root.Flags().Bool("ignore-case", false, "ignore search case, including uppercase queries")
	root.Flags().Bool("case-sensitive", false, "match search case exactly")
	root.Flags().Bool("incremental-search", false, "preview matches while editing a search")
	root.Flags().Bool("highlight-search", true, "highlight visible search matches")
	root.Flags().Bool("regex", true, "treat searches as regular expressions (\\V in a search makes the rest literal)")
	storage := pager.DefaultStorageOptions()
	root.Flags().Int64("memory-budget", storage.MemoryBudget, "resident input byte budget")
	root.Flags().Int64("spool-quota", storage.SpoolQuota, "lossless stream quota in bytes; pause when full")
	root.Flags().Int64("metadata-budget", 8<<20, "resident row/line index byte budget")
	root.Flags().Int64("metadata-quota", 256<<20, "maximum row/line index bytes")
	root.Flags().String("retention", "lossless", "stream retention: lossless or rolling (explicit history loss)")
	root.Flags().String("spool-dir", "", "directory for private spool and index files (default $XDG_CACHE_HOME/glimpse)")
	root.Flags().Int("ambiguous-width", 1, "East Asian ambiguous character width: 1 or 2 cells")
	root.Flags().BoolP("line-numbers", "N", false, "show a line number gutter (toggle with #)")
	root.Flags().Bool("hex-colors", false, "color #RRGGBB codes with their own color in truecolor terminals (toggle with R)")
	root.Flags().Bool("wrap", true, "wrap long lines (toggle with w); otherwise scroll horizontally with h/l")
	root.Flags().StringP("jump-target", "j", "1", "screen line for search matches and line jumps: 1 is the top, -1 the bottom, .5 the middle")
	root.Flags().Int("cmdheight", int(ui.DefaultCmdHeight), "footer layout: -2 like -1 with the statusline initially hidden, -1 toggleable status/prompt row, 0 status with on-demand search row, 1 permanent status and search rows")

	// Value completion: fixed choices, paths where they apply, and nothing
	// (rather than file names) for numbers and labels.
	for flag, values := range map[string][]cobra.Completion{
		"completion":      {"bash", "zsh", "fish", "powershell"},
		"retention":       {"lossless\tpause input when the spool quota is full", "rolling\tdrop the oldest input instead"},
		"ambiguous-width": {"1", "2"},
		"cmdheight":       {"-2\tlike -1, statusline initially hidden", "-1\ttoggleable status/prompt row", "0\tstatus with on-demand search row", "1\tpermanent status and search rows"},
		"jump-target":     {"1\ttop", ".5\tmiddle", "-1\tbottom"},
	} {
		_ = root.RegisterFlagCompletionFunc(flag, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
	}
	for _, flag := range []string{"label", "chunk-size", "memory-budget", "spool-quota", "metadata-budget", "metadata-quota"} {
		_ = root.RegisterFlagCompletionFunc(flag, cobra.NoFileCompletions)
	}
	_ = root.MarkPersistentFlagFilename("config", "toml")
	_ = root.MarkFlagDirname("spool-dir")
	return root
}

// loadConfig binds flags and the environment to config and reads the TOML
// file: configFile, or glimpse.toml in the user config directory if present.
func loadConfig(config *viper.Viper, cmd *cobra.Command, configFile string) error {
	config.SetEnvPrefix("GLIMPSE")
	config.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	config.AutomaticEnv()
	for flag, key := range flagKeys {
		if err := config.BindPFlag(key, cmd.Flags().Lookup(flag)); err != nil {
			return err
		}
	}

	if configFile != "" {
		config.SetConfigFile(configFile)
	} else {
		config.SetConfigName("glimpse")
		config.SetConfigType("toml")
		if dir, err := os.UserConfigDir(); err == nil {
			config.AddConfigPath(dir + string(os.PathSeparator) + "glimpse")
		}
	}

	if err := config.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("read config: %w", err)
		}
	}
	return nil
}

func readOptions(config *viper.Viper, cmd *cobra.Command) (app.Options, error) {
	cfg, err := decodeConfig(config)
	if err != nil {
		return app.Options{}, err
	}
	if cfg.ChunkSize <= 0 {
		return app.Options{}, fmt.Errorf("chunk-size must be a positive integer")
	}
	if cfg.Display.AmbiguousWidth != 1 && cfg.Display.AmbiguousWidth != 2 {
		return app.Options{}, fmt.Errorf("ambiguous-width must be 1 or 2")
	}
	options := app.Options{
		CmdHeight: ui.CmdHeight(cfg.CmdHeight), ChunkSize: cfg.ChunkSize, Follow: cfg.Follow, FollowName: cfg.FollowName, Label: cfg.Label,
		AmbiguousWidth: cfg.Display.AmbiguousWidth, NoWrap: !cfg.Display.Wrap, LineNumbers: cfg.Display.LineNumbers, HexColors: cfg.Display.HexColors,
		IncrementalSearch: cfg.Search.Incremental, NoSearchHighlight: !cfg.Search.Highlight, Literal: !cfg.Search.Regex,
		MetadataBudget: cfg.Storage.MetadataBudget, MetadataQuota: cfg.Storage.MetadataQuota,
		Keybinds: cfg.Keybinds,
	}
	if options.JumpTarget, err = app.ParseJumpTarget(string(cfg.JumpTarget)); err != nil {
		return app.Options{}, err
	}
	options.Storage.MemoryBudget, options.Storage.SpoolQuota = cfg.Storage.MemoryBudget, cfg.Storage.SpoolQuota
	for name, value := range map[string]int64{
		"memory-budget": options.Storage.MemoryBudget, "spool-quota": options.Storage.SpoolQuota,
		"metadata-budget": options.MetadataBudget, "metadata-quota": options.MetadataQuota,
	} {
		if value <= 0 {
			return app.Options{}, fmt.Errorf("storage.%s must be a positive integer byte count", name)
		}
	}
	options.Storage.Retention = cfg.Storage.Retention
	options.Storage.SpoolDir = cfg.Storage.SpoolDir
	if err := readAppearance(cfg, &options); err != nil {
		return app.Options{}, err
	}
	// Treat the case settings as one mode: an enabled flag outranks environment
	// modes, which outrank TOML modes. At the same tier, explicit sensitivity wins
	// over smart case, which wins over unconditional case folding.
	mode, tier := "", 0
	for _, setting := range []struct {
		name    string
		enabled bool
	}{{"ignore-case", cfg.Search.IgnoreCase}, {"smart-case", cfg.Search.SmartCase}, {"case-sensitive", cfg.Search.CaseSensitive}} {
		if !setting.enabled {
			continue
		}
		priority := 1
		if env := os.Getenv("GLIMPSE_SEARCH_" + strings.ToUpper(strings.ReplaceAll(setting.name, "-", "_"))); env != "" {
			priority = 2
		}
		if cmd.Flags().Changed(setting.name) {
			priority = 3
		}
		if priority >= tier {
			mode, tier = setting.name, priority
		}
	}
	options.SmartCase = mode == "smart-case"
	options.IgnoreCase = mode == "ignore-case"
	options.CaseSensitive = mode == "case-sensitive"
	for _, run := range cfg.Run {
		options.Run = append(options.Run, app.RunCommand{Keys: run.Keys, Command: run.Command, Background: run.Background})
	}
	return options, nil
}

func openInput(stdin io.Reader, args []string) (io.ReadCloser, string, error) {
	if len(args) == 1 && args[0] != "-" {
		file, err := os.Open(args[0])
		if err != nil {
			return nil, "", err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, "", err
		}
		if info.IsDir() {
			file.Close()
			return nil, "", fmt.Errorf("%s is a directory", args[0])
		}
		return file, args[0], nil
	}
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return nil, "", fmt.Errorf("provide a file or pipe input into glimpse")
	}
	// Stdin has no name; the statusline omits its filename section.
	if file, ok := stdin.(*os.File); ok {
		return pollableInput(file), "", nil
	}
	if input, ok := stdin.(io.ReadCloser); ok {
		return input, "", nil
	}
	return io.NopCloser(stdin), "", nil
}
