<div align="center">
  <img src="docs/logo.svg" alt="" width="128" height="128">
  <h1>glimpse</h1>
  <p>More than less. Just a glimpse</p>

  [![Linux](https://img.shields.io/badge/Linux-131415?style=for-the-badge&logo=linux&logoColor=white)](#installation)
  [![macOS](https://img.shields.io/badge/macOS-FFFFFF?style=for-the-badge&logo=apple&logoColor=black)](#installation)
  [![Build status](https://img.shields.io/github/actions/workflow/status/araaha/glimpse/test.yml?branch=master&style=for-the-badge&logo=github&logoColor=white&label=build)](https://github.com/araaha/glimpse/actions/workflows/test.yml)
  [![Latest release](https://img.shields.io/github/v/release/araaha/glimpse?style=for-the-badge&logo=github&logoColor=white)](https://github.com/araaha/glimpse/releases)
  [![License](https://img.shields.io/github/license/araaha/glimpse?style=for-the-badge&logo=gnu&logoColor=white)](LICENSE)

  <a href="#installation">Install</a> · <a href="#usage">Usage</a> · <a href="#configuration">Configuration</a> · <a href="#following-files">Following files</a> · <a href="#large-input-and-memory">Large input</a>
</div>

glimpse is a pager, like less. It stays responsive on large files and streams with a vim-like interface.

Highlights
----------

- **Responsive on large inputs** Regular files are read in place instead of
  loaded, so huge files, endless streams and single lines that never end stay
  quick to scroll and search
- **Vim-like** `/` regular expression search, `:` commands with `:set` and autocomplete, `gg`,
  `G`, counts, and keys like `zq` you can rebind in a TOML file
- **Scriptable** Bind keys to shell commands that receive the current file
  and line
- **Batteries-included** Comes with built-in completions for Bash, Zsh, Fish and Nushell

Table of Contents
-----------------

* [Installation](#installation)
    * [Package managers](#package-managers)
    * [Other methods](#other-methods)
* [Usage](#usage)
    * [As your default pager](#as-your-default-pager)
    * [Keys](#keys)
    * [Searching](#searching)
    * [Filtering](#filtering)
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

> [!WARNING]
> Windows is not supported.

### Package managers

<details>
<summary><b>Arch Linux (AUR)</b></summary>
<br>

Build it from source with `glimpse-pager`, or install the release binary with
`glimpse-pager-bin`, using your AUR helper:

```sh
yay -S glimpse-pager        # or glimpse-pager-bin
```

OR

```sh
paru -S glimpse-pager       # or glimpse-pager-bin
```
> Both install `/usr/bin/glimpse`, so they conflict with the unrelated AUR
> package named `glimpse`.
</details>

<details>
<summary><b>macOS (Homebrew)</b></summary>
<br>

```sh
brew install --cask araaha/tap/glimpse
```

This installs the binary, the man page and the bash, zsh, fish and Nushell
completions.
</details>

<details>
<summary><b>Debian / Ubuntu (.deb)</b></summary>
<br>

```sh
curl -LO https://github.com/araaha/glimpse/releases/latest/download/glimpse_linux_amd64.deb
sudo apt install ./glimpse_linux_amd64.deb
rm glimpse_linux_amd64.deb
```

*(On ARM64, replace `amd64` with `arm64`.)*
</details>

<details>
<summary><b>Fedora (.rpm)</b></summary>
<br>

```sh
sudo dnf install https://github.com/araaha/glimpse/releases/latest/download/glimpse_linux_amd64.rpm
```

*(On ARM64, replace `amd64` with `arm64`.)*
</details>

<details>
<summary><b>Nix</b></summary>
<br>

**Try it without installing:**

```sh
nix run github:araaha/glimpse -- FILE
```

**Install it to your user profile:**

```sh
nix profile install github:araaha/glimpse
```

**In a flake-based configuration**, add the flake to your inputs:

```nix
glimpse.url = "github:araaha/glimpse";
```

Then add the package with Home Manager *(pass `inputs` to modules via `extraSpecialArgs`)*:

```nix
home.packages = [ inputs.glimpse.packages.${pkgs.stdenv.hostPlatform.system}.default ];
```

or system-wide on NixOS *(pass `inputs` via `specialArgs`)*:

```nix
environment.systemPackages = [ inputs.glimpse.packages.${pkgs.stdenv.hostPlatform.system}.default ];
```
</details>

### Other methods

<details>
<summary><b>Binary releases</b></summary>
<br>

Download an archive for your platform from the
[releases page](https://github.com/araaha/glimpse/releases). Each one contains
the `glimpse` binary, the man page `docs/glimpse.1`, bash, zsh, fish and
Nushell completions in `completions/`, and the reference configuration
`glimpse.toml`.
</details>

<details>
<summary><b>Using Go</b></summary>
<br>

```sh
go install github.com/araaha/glimpse/cmd/glimpse@latest
```

> [!NOTE]
> This installs the binary only. Generate completions with
> `glimpse --completion bash|zsh|fish|nushell`.
</details>

<details>
<summary><b>From source</b></summary>
<br>

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
</details>

Usage
-----

```sh
glimpse FILE
command | glimpse
```

With no file, or `-`, glimpse reads standard input. Keys are read from the
terminal, so piped input works.

> [!NOTE]
> When standard output is not a terminal, glimpse copies its input unchanged,
> like `cat`.

> [!TIP]
> While piped input is still silent, glimpse waits up to half a second before
> taking over the screen, and for as long as another program has turned off
> terminal echo. A password prompt such as the one in
> `sudo journalctl | glimpse` can be answered first.

### As your default pager

```sh
export PAGER=glimpse
export MANPAGER=glimpse
git config --global core.pager glimpse
```

For man pages, the statusline shows the page name, taken from `$MAN_PN`.

### Keys

> [!TIP]
> Press <kbd>g</kbd> <kbd>?</kbd> inside glimpse for every binding in effect,
> including the ones you configured.

| Keys | Action |
| --- | --- |
| <kbd>j</kbd>, <kbd>↓</kbd> / <kbd>k</kbd>, <kbd>↑</kbd> | Scroll one row |
| <kbd>Ctrl</kbd>+<kbd>F</kbd>, <kbd>PageDown</kbd>, <kbd>Space</kbd> / <kbd>Ctrl</kbd>+<kbd>B</kbd>, <kbd>PageUp</kbd> | Scroll one screen |
| <kbd>Ctrl</kbd>+<kbd>D</kbd> / <kbd>Ctrl</kbd>+<kbd>U</kbd> | Scroll half a screen |
| <kbd>g</kbd> <kbd>g</kbd>, <kbd>Home</kbd> / <kbd>G</kbd>, <kbd>End</kbd> | Go to the first / last line; `NG` goes to line N |
| <kbd>h</kbd>, <kbd>←</kbd> / <kbd>l</kbd>, <kbd>→</kbd> | Scroll unwrapped lines sideways |
| <kbd>/</kbd> / <kbd>?</kbd> | Search forward / backward |
| <kbd>n</kbd> / <kbd>N</kbd> | Repeat the search in the same / opposite direction |
| <kbd>&</kbd> | Show only lines matching a pattern; empty shows every line |
| <kbd>m</kbd> then a letter / <kbd>'</kbd> then a letter | Mark the current line / go to a mark |
| <kbd>'</kbd> <kbd>'</kbd> | Go back to where the last jump started |
| <kbd>:</kbd> | Enter a command |
| <kbd>F</kbd> | Toggle following the end of the input |
| <kbd>w</kbd> | Toggle line wrapping |
| <kbd>#</kbd> | Toggle line numbers |
| <kbd>R</kbd> | Toggle coloring `#RRGGBB` codes in their own color |
| <kbd>t</kbd> <kbd>s</kbd> | Toggle highlighting search matches |
| <kbd>v</kbd> | Edit the file in `$VISUAL` or `$EDITOR` at the current line |
| <kbd>=</kbd> | Toggle the statusline (with `--cmdheight` -1 or -2) |
| <kbd>g</kbd> <kbd>?</kbd> | Show all bindings |
| <kbd>Esc</kbd> | Cancel a pending key sequence or search |
| <kbd>q</kbd>, <kbd>z</kbd> <kbd>q</kbd>, <kbd>Ctrl</kbd>+<kbd>C</kbd> | Quit |

### Searching

Searches are regular expressions in Go's
[RE2 syntax](https://github.com/google/re2/wiki/Syntax), matched against the
text without its colors, so a search finds words in colored output and man
pages.

> [!TIP]
> As in vim, `\V` makes the rest of a pattern literal and `\c` / `\C` force
> case-insensitive or case-sensitive matching.

| Option | Effect |
| --- | --- |
| `--smart-case` | Ignore case unless the search has an uppercase letter |
| `--ignore-case` / `--case-sensitive` | Always ignore / always match case |
| `--incremental-search` | Show matches while typing |
| `--regex=false` | Search for plain text |
| `--highlight-search=false` | Do not highlight visible matches |

Search, filter and command history are saved in `$XDG_DATA_HOME/glimpse/`,
usually `~/.local/share/glimpse/`.

### Filtering

`&pattern` shows only the lines that match, like `grep` inside the pager. The
pattern works like a search, and line numbers, the statusline and `$line` keep
each line's number in the input. Matching lines appear as they are found, so a
filter on a huge file shows its first matches right away, and a filter on a
followed log keeps up with new lines:

```sh
journalctl -f | glimpse --follow   # then &error
```

The filter's pattern also becomes the search, as in ov: its matches are
highlighted, and `n` and `N` step through them. With `--incremental-search`,
matches on screen are highlighted as you type the pattern, and Esc brings back
the earlier search.

Searches, `G` and `:N` work within the matching lines. `&` and Enter, or
`:nofilter`, shows every line again with the same line in view, keeping the
filter's pattern as the search.

> [!NOTE]
> Filtering is not available with `--retention=rolling`.

### Commands

| Command | Action |
| --- | --- |
| `:N` | Go to line N |
| `:top`, `:bottom` | Go to the first or last line |
| `:filter PATTERN`, `:nofilter` | Show only matching lines, or every line again |
| `:marks`, `:delmarks a b`, `:delmarks!` | List marks, or delete some or all |
| `:set OPTION`, `:set noOPTION` | Change an option, with vim's names: `ignorecase`, `smartcase`, `incsearch`, `hlsearch`, `magic`, `wrap`, `number`, `hex-colors` |
| `:set jump-target=N` | Change where jumps land: `0` bottom, `0.5` middle, `1` top |
| `:set spool-quota=BYTES` | Raise the stream storage quota and resume reading |
| `:q`, `:quit` | Quit |

<kbd>Tab</kbd> completes commands and option names.

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
# Land searches and line jumps in the middle (0 = bottom, 1 = top).
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

> [!TIP]
> `glimpse --check-config` validates the file, including misspelled option
> names, and prints which file it read.

The [man page](docs/glimpse.1) lists every key, including the statusline layout
and colors.

### Key bindings

> [!IMPORTANT]
> Each entry under `[keybinds]` is **one key sequence**, and it replaces all of
> that action's default keys.

An empty array unbinds the action:

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

> [!WARNING]
> The variables are never pasted into the command text, so file names with
> spaces or quotes are safe as long as you quote `"$file"`.

If a command fails, its exit status is shown. When a foreground command saves
the file by renaming a new file over it, as vim does, glimpse reopens the new
file.

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

> [!CAUTION]
> Older input is discarded and cannot be scrolled back to.

The line and row index has its own limits, `--metadata-budget` and
`--metadata-quota`.

Not supported
-------------

- More than one file at a time
- Syntax highlighting; use a preprocessor such as
  [bat](https://github.com/sharkdp/bat) and pipe its colored output in
- Compressed files; decompress into a pipe, for example `zcat file.gz | glimpse`
- Mouse scrolling

VS
--

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
