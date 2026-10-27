// Package command defines Glimpse's Cobra command tree and Viper configuration.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if checkTerminal != nil {
				if err := checkTerminal(cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			options, err := readOptions(config, cmd)
			if err != nil {
				return err
			}
			application, err := app.New(options)
			if err != nil {
				return err
			}
			input, name, err := openInput(cmd.InOrStdin(), args)
			if err != nil {
				return err
			}
			return run(cmd.Context(), application, input, name)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			config.SetEnvPrefix("GLIMPSE")
			config.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
			config.AutomaticEnv()
			if err := config.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
			for _, name := range []string{"smart-case", "ignore-case", "case-sensitive"} {
				if err := config.BindPFlag("search."+name, cmd.Flags().Lookup(name)); err != nil {
					return err
				}
			}
			for flag, key := range map[string]string{"incremental-search": "search.incremental", "highlight-search": "search.highlight"} {
				if err := config.BindPFlag(key, cmd.Flags().Lookup(flag)); err != nil {
					return err
				}
			}
			for _, name := range []string{"memory-budget", "spool-quota", "metadata-budget", "metadata-quota", "retention", "spool-dir"} {
				if err := config.BindPFlag("storage."+name, cmd.Flags().Lookup(name)); err != nil {
					return err
				}
			}
			if err := config.BindPFlag("display.ambiguous-width", cmd.Flags().Lookup("ambiguous-width")); err != nil {
				return err
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
		},
	}

	root.PersistentFlags().StringVar(&configFile, "config", "", "TOML config file (default: $XDG_CONFIG_HOME/glimpse/glimpse.toml)")
	root.Flags().Int("chunk-size", 64*1024, "input storage chunk size in bytes")
	root.Flags().Bool("follow", false, "follow the end of incoming input (toggle with F)")
	root.Flags().Bool("smart-case", false, "ignore search case unless the query contains an uppercase letter")
	root.Flags().Bool("ignore-case", false, "ignore search case, including uppercase queries")
	root.Flags().Bool("case-sensitive", false, "match search case exactly")
	root.Flags().Bool("incremental-search", false, "preview matches while editing a search")
	root.Flags().Bool("highlight-search", true, "highlight visible search matches")
	storage := pager.DefaultStorageOptions()
	root.Flags().Int64("memory-budget", storage.MemoryBudget, "resident input byte budget")
	root.Flags().Int64("spool-quota", storage.SpoolQuota, "lossless stream quota in bytes; pause when full")
	root.Flags().Int64("metadata-budget", 8<<20, "resident row/line index byte budget")
	root.Flags().Int64("metadata-quota", 256<<20, "maximum row/line index bytes")
	root.Flags().String("retention", "lossless", "stream retention: lossless or rolling (explicit history loss)")
	root.Flags().String("spool-dir", "", "directory for private temporary input spools")
	root.Flags().Int("ambiguous-width", 1, "East Asian ambiguous character width: 1 or 2 cells")
	root.Flags().Int("cmdheight", int(ui.DefaultCmdHeight), "footer layout: -1 toggleable status/prompt row, 0 status with on-demand search row, 1 permanent status and search rows")
	return root
}

func readOptions(config *viper.Viper, cmd *cobra.Command) (app.Options, error) {
	height, err := strconv.Atoi(config.GetString("cmdheight"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse cmdheight: %w", err)
	}
	chunkSize, err := strconv.Atoi(config.GetString("chunk-size"))
	if err != nil || chunkSize <= 0 {
		return app.Options{}, fmt.Errorf("chunk-size must be a positive integer")
	}
	follow, err := strconv.ParseBool(config.GetString("follow"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse follow: %w", err)
	}
	options := app.Options{CmdHeight: ui.CmdHeight(height), ChunkSize: chunkSize, Follow: follow}
	options.AmbiguousWidth, err = strconv.Atoi(config.GetString("display.ambiguous-width"))
	if err != nil || options.AmbiguousWidth != 1 && options.AmbiguousWidth != 2 {
		return app.Options{}, fmt.Errorf("ambiguous-width must be 1 or 2")
	}
	for name, destination := range map[string]*int64{
		"memory-budget": &options.Storage.MemoryBudget, "spool-quota": &options.Storage.SpoolQuota,
		"metadata-budget": &options.MetadataBudget, "metadata-quota": &options.MetadataQuota,
	} {
		value, err := strconv.ParseInt(config.GetString("storage."+name), 10, 64)
		if err != nil || value <= 0 {
			return app.Options{}, fmt.Errorf("storage.%s must be a positive integer byte count", name)
		}
		*destination = value
	}
	options.Storage.Retention = config.GetString("storage.retention")
	options.Storage.SpoolDir = config.GetString("storage.spool-dir")
	options.IncrementalSearch, err = strconv.ParseBool(config.GetString("search.incremental"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse search.incremental: %w", err)
	}
	highlight, err := strconv.ParseBool(config.GetString("search.highlight"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse search.highlight: %w", err)
	}
	options.NoSearchHighlight = !highlight
	if err := readAppearance(config, &options); err != nil {
		return app.Options{}, err
	}
	// Treat the case settings as one mode: an enabled flag outranks environment
	// modes, which outrank TOML modes. At the same tier, explicit sensitivity wins
	// over smart case, which wins over unconditional case folding.
	mode, tier := "", 0
	for _, name := range []string{"ignore-case", "smart-case", "case-sensitive"} {
		enabled, err := strconv.ParseBool(config.GetString("search." + name))
		if err != nil {
			return app.Options{}, fmt.Errorf("parse search.%s: %w", name, err)
		}
		if !enabled {
			continue
		}
		priority := 1
		if env := os.Getenv("GLIMPSE_SEARCH_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))); env != "" {
			priority = 2
		}
		if cmd.Flags().Changed(name) {
			priority = 3
		}
		if priority >= tier {
			mode, tier = name, priority
		}
	}
	options.SmartCase = mode == "smart-case"
	options.IgnoreCase = mode == "ignore-case"
	options.CaseSensitive = mode == "case-sensitive"
	if raw := config.Get("keybinds"); raw != nil {
		table, ok := raw.(map[string]any)
		if !ok {
			return app.Options{}, fmt.Errorf("keybinds must be a table of key arrays")
		}
		bindings := make(map[string][]string, len(table))
		for action, rawKeys := range table {
			keys, ok := rawKeys.([]any)
			if !ok {
				return app.Options{}, fmt.Errorf("keybind %s must be an array of keys", action)
			}
			sequence := make([]string, 0, len(keys))
			for _, rawKey := range keys {
				key, ok := rawKey.(string)
				if !ok {
					return app.Options{}, fmt.Errorf("keybind %s contains a non-string key", action)
				}
				sequence = append(sequence, key)
			}
			bindings[action] = sequence
		}
		options.Keybinds = bindings
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
	if input, ok := stdin.(io.ReadCloser); ok {
		return input, "stdin", nil
	}
	return io.NopCloser(stdin), "stdin", nil
}
