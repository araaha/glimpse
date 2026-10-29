package command

import (
	"reflect"
	"strings"
	"testing"

	"glimpse/internal/app"

	"github.com/spf13/viper"
)

func TestReadRunTables(t *testing.T) {
	parse := func(text string) ([]app.RunCommand, error) {
		t.Helper()
		config := viper.New()
		config.SetConfigType("toml")
		if err := config.ReadConfig(strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
		return readRun(config.Get("run"))
	}
	runs, err := parse("[[run]]\nkeys = ['s', 'h']\ncommand = 'nvim +$line \"$file\"'\n\n[[run]]\nkeys = ['<C-y>']\ncommand = 'wl-copy'\nbackground = true\n")
	want := []app.RunCommand{
		{Keys: []string{"s", "h"}, Command: `nvim +$line "$file"`},
		{Keys: []string{"<C-y>"}, Command: "wl-copy", Background: true},
	}
	if err != nil || !reflect.DeepEqual(runs, want) {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	if runs, err := parse("cmdheight = 0\n"); err != nil || runs != nil {
		t.Fatalf("no run tables = %+v, %v", runs, err)
	}
	for _, tt := range []struct{ config, want string }{
		{"run = 'x'", "run must be an array of tables"},
		{"run = ['x']", "run must be an array of tables"},
		{"[[run]]\nkeys = 's'\ncommand = 'x'", "run-1: keys must be an array"},
		{"[[run]]\nkeys = [1]\ncommand = 'x'", "run-1: keys contains a non-string key"},
		{"[[run]]\nkeys = ['s']\ncommand = 1", "run-1: command must be a string"},
		{"[[run]]\nkeys = ['s']\ncommand = 'x'\nbackground = 'yes'", "run-1: background must be true or false"},
		{"[[run]]\nkeys = ['s']\ncommand = 'x'\n[[run]]\nkeys = ['t']\ncmd = 'x'", `run-2: unknown option "cmd"`},
	} {
		if _, err := parse(tt.config); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q: error %v, want %q", tt.config, err, tt.want)
		}
	}
}
