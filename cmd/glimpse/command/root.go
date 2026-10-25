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
	})
}

type runner func(context.Context, *app.App, io.ReadCloser, string) error

func newRoot(run runner) *cobra.Command {
	config := viper.New()
	var configFile string

	root := &cobra.Command{
		Use:           "glimpse [FILE]",
		Short:         "A terminal pager",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := readOptions(config)
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
			config.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
			config.AutomaticEnv()
			if err := config.BindPFlags(cmd.Flags()); err != nil {
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
	root.Flags().Int("cmdheight", int(ui.DefaultCmdHeight), "footer layout: -1 statusline initially visible, 0 initially hidden, 1 statusline above search row")
	return root
}

func readOptions(config *viper.Viper) (app.Options, error) {
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
	if raw := config.Get("keybinds"); raw != nil {
		table, ok := raw.(map[string]any)
		if !ok {
			return app.Options{}, fmt.Errorf("keybinds must be a table of key arrays")
		}
		options.Keybinds = make(map[string][]string, len(table))
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
			options.Keybinds[action] = sequence
		}
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
