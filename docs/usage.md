# Minimal viewer

From the repository root:

```sh
go run ./cmd/glimpse path/to/file
printf 'hello\nworld\n' | go run ./cmd/glimpse
LC_ALL=C tr -dc 'a-zA-Z0-9' < /dev/urandom | go run ./cmd/glimpse --follow
```

Document input comes from the file or stdin; tcell uses the controlling terminal
for keyboard input and screen output. `-` explicitly selects stdin. This first
version supports one input at a time and basic plain-text wrapping. Search,
ANSI styling, and full grapheme-aware wrapping are not implemented yet.

## Default keys

| Keys | Action |
| --- | --- |
| `j`, Down / `k`, Up | Scroll down / up one wrapped row |
| `gg`, Home / `G`, End | Go to the top / bottom |
| Ctrl-F, PageDown, Space / Ctrl-B, PageUp | Page down / up |
| Ctrl-D / Ctrl-U | Half-page down / up |
| `F` | Toggle following the end of incoming input |
| `=` | Toggle the statusbar in `cmdheight = 0` or `-1` |
| `q`, `zq`, Ctrl-C | Quit |
| Esc | Cancel a pending key sequence |

Scrolling or jumping with `gg` / `G` leaves follow mode. `--follow` starts at the
latest screenful as input arrives; it does not yet watch a file for later changes
after EOF. Partial lines do not wait for a newline before appearing.

## TOML configuration

The default path is `$XDG_CONFIG_HOME/glimpse/glimpse.toml`, or
`~/.config/glimpse/glimpse.toml` when `XDG_CONFIG_HOME` is unset. Use `--config`
to select a different file. Flags override environment variables, which override
the configuration file (for example, `GLIMPSE_CMDHEIGHT=1`).

```toml
cmdheight = 0
chunk-size = 65536
follow = false

[keybinds]
top = ["g", "g"]
bottom = ["G"]
quit = ["z", "q"]
toggle-status = ["="]
page-down = ["<C-f>"]
```

Each array is **one sequence**, not a list of alternatives. Setting an action
replaces all its default aliases; an empty array disables it. Omitted actions
keep their defaults unless their sequence is explicitly assigned to another
action. Invalid keys, unknown actions, and duplicate sequences between explicit
bindings are rejected on startup.

Ctrl-C and Esc are ordinary, rebindable keys. For example:

```toml
[keybinds]
up = ["<C-c>"]
top = ["<Esc>"]
```

This removes their default quit/cancel behavior. The `cancel` action can also be
rebound or disabled with `cancel = []`; no keys are forcibly reserved.

Incomplete prefixes such as the first `g` in `gg` wait for the next key without
a timeout. Only ambiguous bindings time out after 90 ms: when a shorter binding
is also a prefix, it waits for the longer sequence and fires on timeout or a
nonmatching continuation. A nonmatching key is then retried as a fresh binding.

`cmdheight = 1` reserves two rows: status above a blank search row. In mode `0`,
the statusbar starts hidden; in mode `-1` it starts visible. `=` toggles it in
either mode. The top visible document row stays fixed unless expanding the
viewport at EOF would leave empty space; in that case, preceding text fills the
new row and the bottom stays anchored. Footer contents are temporary placeholders
until the statusline is designed.

## Startup

`go run` invokes the Go toolchain before starting the viewer. For normal use,
build once with `go build -o glimpse ./cmd/glimpse`, then run `./glimpse FILE`.

The viewer skips optional tcell capability negotiation, which can otherwise wait
up to one second for a missing terminal response. It uses known terminal profiles
and standard keyboard reporting instead. Set `TCELL_NEGOTIATE=auto` if you need
tcell's automatic capability probing. The first frame is painted immediately
instead of waiting for the periodic refresh tick.
