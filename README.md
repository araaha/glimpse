<div align="center">
  <h1>glimpse</h1>
  <p>More than less. Just a glimpse</p>
  <a href="https://github.com/araaha/glimpse/actions/workflows/test.yml"><img src="https://github.com/araaha/glimpse/actions/workflows/test.yml/badge.svg?branch=master" alt="Build status"></a>
  <a href="https://github.com/araaha/glimpse/releases"><img src="https://img.shields.io/github/v/release/araaha/glimpse" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/araaha/glimpse" alt="License"></a>
</div>

---

glimpse is a pager. It
stays responsive on large files and streams.

Highlights
----------

- **Responsive on large inputs** // Regular files are read in place instead of
  loaded, so huge files, endless streams and single lines that never end stay
  quick to scroll and search
- **Vim-like** // `/` regular expression search, `:` commands with `:set` and autocomplete, `gg`,
  `G`, counts, and keys like `zq` you can rebind in a TOML file
- **Scriptable** // Bind keys to shell commands that receive the current file
  and line
- **Batteries-included** // Comes with built-in completions for Bash, Zsh, Fish and Nushell

Table of Contents
-----------------

* [Installation](#installation)
* [Usage](#usage)
    * [As your default pager](#as-your-default-pager)
    * [Keys](#keys)
    * [Searching](#searching)
    * [Commands](#commands)
* [Configuration](#configuration)
    * [Key bindings](#key-bindings)
    * [Running commands](#running-commands)
* [Following files](#following-files)
* [Large input and memory](#large-input-and-memory)
* [VS](#vs)
* [License](#license)

Installation
------------

glimpse is tested on Linux and macOS and builds on FreeBSD and OpenBSD.

### Arch Linux

From the AUR, built from source or as the release binary:

```sh
yay -S glimpse-pager        # or glimpse-pager-bin
```

Both install `/usr/bin/glimpse`, so they conflict with the unrelated AUR
package named `glimpse`.

### Debian and Ubuntu

Download the `.deb` for your architecture (`amd64` or `arm64`) from the
[latest release](https://github.com/araaha/glimpse/releases/latest) and install
it:

```sh
sudo apt install ./glimpse_*_linux_amd64.deb
```

### Fedora

```sh
sudo dnf install ./glimpse_*_linux_amd64.rpm
```

using the `.rpm` from the
[latest release](https://github.com/araaha/glimpse/releases/latest).

### Nix

```sh
nix run github:araaha/glimpse -- FILE           # try it
nix profile install github:araaha/glimpse       # install it
```

In a flake-based NixOS or Home Manager configuration, add the flake as an input
and use `inputs.glimpse.packages.${pkgs.system}.default`.

### Binary releases

Download an archive for your platform from the
[releases page](https://github.com/araaha/glimpse/releases). Each one contains
the `glimpse` binary, the man page `docs/glimpse.1`, bash, zsh, fish and
Nushell completions in `completions/`, and the reference configuration
`glimpse.toml`.

### Using Go

```sh
go install github.com/araaha/glimpse/cmd/glimpse@latest
```

This installs the binary only. Generate completions with
`glimpse --completion bash|zsh|fish|nushell`.

### From source

Building needs Go 1.27 or later.

```sh
git clone https://github.com/araaha/glimpse.git
cd glimpse
sudo make install
```

This installs the binary, the man page and the bash, zsh, fish and Nushell
completions under `/usr/local`.
Set `PREFIX` to install elsewhere, or `DESTDIR` for a staged install, and use
`make uninstall` to remove them.

Usage
-----

```sh
glimpse FILE
command | glimpse
```

With no file, or `-`, glimpse reads standard input. Keys are read from the
terminal, so piped input works. When standard output is not a terminal, glimpse
copies its input unchanged, like `cat`.

While piped input is still silent, glimpse waits up to half a second before
taking over the screen, and for as long as another program has turned off
terminal echo. A password prompt such as the one in `sudo journalctl | glimpse`
can be answered first.

### As your default pager

```sh
export PAGER=glimpse
export MANPAGER=glimpse
git config --global core.pager glimpse
```

For man pages, the statusline shows the page name, taken from `$MAN_PN`.

### Keys

Press `g?` inside glimpse for every binding in effect, including the ones you
configured.

| Keys | Action |
| --- | --- |
| `j`, Down / `k`, Up | Scroll one row |
| Ctrl-F, PageDown, Space / Ctrl-B, PageUp | Scroll one screen |
| Ctrl-D / Ctrl-U | Scroll half a screen |
| `gg`, Home / `G`, End | Go to the first / last line; `NG` goes to line N |
| `h`, Left / `l`, Right | Scroll unwrapped lines sideways |
| `/` / `?` | Search forward / backward |
| `n` / `N` | Repeat the search in the same / opposite direction |
| `:` | Enter a command |
| `F` | Toggle following the end of the input |
| `w` | Toggle line wrapping |
| `#` | Toggle line numbers |
| `R` | Toggle coloring `#RRGGBB` codes in their own color |
| `v` | Edit the file in `$VISUAL` or `$EDITOR` at the current line |
| `=` | Toggle the statusline (with `--cmdheight` -1 or -2) |
| `g?` | Show all bindings |
| Esc | Cancel a pending key sequence or search |
| `q`, `zq`, Ctrl-C | Quit |

### Searching

Searches are regular expressions in Go's
[RE2 syntax](https://github.com/google/re2/wiki/Syntax), matched against the
text without its colors, so a search finds words in colored output and man
pages. As in vim, `\V` makes the rest of a pattern literal and `\c` / `\C`
force case-insensitive or case-sensitive matching.

| Option | Effect |
| --- | --- |
| `--smart-case` | Ignore case unless the search has an uppercase letter |
| `--ignore-case` / `--case-sensitive` | Always ignore / always match case |
| `--incremental-search` | Show matches while typing |
| `--regex=false` | Search for plain text |
| `--highlight-search=false` | Do not highlight visible matches |

Search and command history are saved in `$XDG_DATA_HOME/glimpse/`, usually
`~/.local/share/glimpse/`.

### Commands

| Command | Action |
| --- | --- |
| `:N` | Go to line N |
| `:top`, `:bottom` | Go to the first or last line |
| `:set OPTION`, `:set noOPTION` | Change an option, with vim's names: `ignorecase`, `smartcase`, `incsearch`, `hlsearch`, `magic`, `wrap`, `number`, `hex-colors` |
| `:set jump-target=N` | Change where jumps land on screen |
| `:set spool-quota=BYTES` | Raise the stream storage quota and resume reading |
| `:q`, `:quit` | Quit |

Tab completes commands and option names.

Configuration
-------------

Settings are read from `$XDG_CONFIG_HOME/glimpse/glimpse.toml`, usually
`~/.config/glimpse/glimpse.toml`. Command-line options override environment
variables, which override the file. Every option can also be set as an
environment variable such as `GLIMPSE_CMDHEIGHT=1` or
`GLIMPSE_SEARCH_SMART_CASE=true`.

[`glimpse.toml`](glimpse.toml) lists every setting, commented out at its
default, and is a good starting point. The packages install it under
`/usr/share/doc/`. A short example:

```toml
# Land searches and line jumps in the middle of the screen.
jump-target = 0.5

[display]
line-numbers = true

[search]
smart-case = true
incremental = true

[statusline]
filename-style = "short" # ~/.c/n/init.lua

[keybinds]
half-page-down = ["d"]
half-page-up = ["u"]
```

`glimpse --check-config` validates the file, including misspelled option
names, and prints which file it read. The [man page](docs/glimpse.1) lists every
key, including the statusline layout and colors.

### Key bindings

Each entry under `[keybinds]` is **one key sequence**, and it replaces all of
that action's default keys. An empty array unbinds the action:

```toml
[keybinds]
top = ["g", "g"]
quit = ["q"]         # zq and Ctrl-C no longer quit
toggle-status = []
```

Keys are written as in vim, for example `<C-f>` or `<S-Tab>`. Unknown actions,
invalid keys and two actions bound to the same sequence are reported at
startup.

### Running commands

`[[run]]` binds a key sequence to a shell command, run with `sh -c`. The command
takes over the terminal until it exits, or runs in the background with
`background = true`. It gets two environment variables: `$file`, the file's
absolute path (empty for standard input), and `$line`, the line at the jump
target, which is the line you just jumped or searched to.

```toml
# Open the current line in Neovim.
[[run]]
keys = ["s", "h"]
command = 'nvim +$line "$file"'

# Copy the current line number without leaving glimpse.
[[run]]
keys = ["s", "y"]
command = 'printf %s "$line" | wl-copy'
background = true
```

The variables are never pasted into the command text, so file names with spaces
or quotes are safe as long as you quote `"$file"`. If a command fails, its exit
status is shown. When a foreground command saves the file by renaming a new
file over it, as vim does, glimpse reopens the new file.

Following files
---------------

`--follow` (or `F`) keeps the end of the input on screen as it grows. Scrolling,
a jump or a search match stops following; opening the search or command prompt
does not.

Once glimpse has read a regular file to the end, it keeps watching it, even
without `--follow`. Appends, truncation and edits in place are reloaded, and the line at
the jump target stays where it was on screen.

| | Watches | Like |
| --- | --- | --- |
| default | the open file, even after it is renamed | `tail -f` |
| `--follow-name` | the path, reopening it after rotation and waiting while it is missing | `tail -F` |

```sh
glimpse --follow --follow-name -N /var/log/app.log
```

Large input and memory
----------------------

Regular files are read in place, so their size barely affects memory. Pipes
have to be stored somewhere:

- Up to `--memory-budget` (64 MiB) of a stream is kept in memory.
- Beyond that, it goes to a spool file in `--spool-dir`
  (`$XDG_CACHE_HOME/glimpse`), which is deleted as soon as it is created, so it
  never outlives glimpse.
- When the spool reaches `--spool-quota` (1 GiB), reading pauses until you
  raise it with `:set spool-quota=BYTES`. Nothing is dropped.

For a stream that never ends, `--retention=rolling` keeps only the most recent
`--memory-budget` bytes instead:

```sh
journalctl -f | glimpse --follow --retention=rolling
```

The line and row index has its own limits, `--metadata-budget` and
`--metadata-quota`.

Not supported
-------------

- Windows
- More than one file at a time
- Filtering the view to matching lines
- Syntax highlighting; use a preprocessor such as
  [bat](https://github.com/sharkdp/bat) and pipe its colored output in
- Compressed files; decompress into a pipe, for example `zcat file.gz | glimpse`
- Mouse scrolling

VS
--

How glimpse compares with [less](https://github.com/gwsw/less),
[ov](https://github.com/noborus/ov) and [moor](https://github.com/walles/moor),
based on their documentation for less 710, ov 0.55.0 and moor 2.19.6.

| | glimpse | less | ov | moor |
| --- | :---: | :---: | :---: | :---: |
| Follow appended input (`tail -f`) | ✓ | ✓ | ✓ | ✓ |
| Follow a path through rotation (`tail -F`) | ✓ | ✓ | ✓ | ✗ |
| Incremental search | ✓ | ✓ | ✓ | ✓ |
| Regular expression search | ✓ | ✓ | ✓ | ✓ |
| Open the file in an editor | ✓ | ✓ | ✓ | ✓ |
| Rebind keys | ✓ | ✓ | ✓ | ✗ |
| Key sequences | ✓ | ✗ | ✗ | ✗ |
| Spool long pipes to disk, losslessly | ✓ | ✗ | ✗ | ✗ |
| Filter to matching lines | ✗ | ✓ | ✓ | ✓ |
| Configuration | TOML file, environment | `lesskey` file, `LESS` | YAML file | `MOOR` |

- **less** is the standard pager, installed almost everywhere, with decades of
  features and options.
- **ov** has the most features for tabular and sectioned text: header lines,
  column mode, sections, multi-color highlights and running a command to view
  its output.
- **moor** aims to do the right thing with no configuration, including syntax
  highlighting and transparent decompression.
- **glimpse** focuses on performance for large streams and files with a vim-like interface.

License
-------

glimpse is free software, licensed under the
[GNU General Public License, version 3 or later](LICENSE).
