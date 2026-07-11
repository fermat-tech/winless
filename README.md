# winless

A `less`-like terminal pager for Windows — single binary, no runtime, no dependencies to install.

## Features

- Scroll files, piped stdin, or multiple files in the terminal
- Syntax highlighting for Go, JS/TS, Python, Rust, C/C++, Shell, JSON, YAML, TOML, CSS, HTML, Markdown, Ruby
- Regex search forward and backward with match highlighting, case-sensitive toggle
- Half-page scroll, jump to line number, jump to percentage
- **Multi-file navigation** — `:n` / `:p` / `:x` / `:d` / `:e` like real `less`
- **Mouse drag to select and auto-copy to clipboard** — drag to highlight, text is copied on release
- **`y` key** — copy the current line to clipboard instantly
- **Paste into search** — `Ctrl+V`, `Insert`, or right-click while typing a pattern
- Follow mode (like `tail -f`) for live log watching
- Line wrap / chop toggle
- Optional line numbers
- Mouse scroll support
- In-pager help overlay (`h`)

## Install

```
go install github.com/fermat-tech/winless@latest
```

Or grab a pre-built `.exe` from the [Releases](https://github.com/fermat-tech/winless/releases) page — no Go toolchain needed.

Requires Go 1.21+ to build from source. Produces a single self-contained `.exe`.

## Usage

```
winless [options] [file ...]
command | winless [options]
```

Multiple files may be given; move between them with `:n` / `:p` (see Multi-file below).

**Options**

| Flag | Description |
|------|-------------|
| `-N`, `--line-numbers` | Show line numbers |
| `-S`, `--chop-long-lines` | Chop long lines instead of wrapping |
| `-i`, `--ignore-case` | Case-insensitive search (default); toggle with `I` |
| `-f`, `--follow` | Start in follow mode (like `tail -f`) |
| `-h`, `--help` | Show help |
| `-V`, `--version` | Show version and exit |

## Key Bindings

**Navigation**

| Key | Action |
|-----|--------|
| `↑` / `k`, `↓` / `j` | Scroll one line |
| `d` / `Ctrl+D`, `u` / `Ctrl+U` | Scroll half a page down / up |
| `PgUp` / `b`, `PgDn` / `Space` | Scroll one page |
| `g` / `Home` | First line — `Ng` jumps to line `N` |
| `G` / `End` | Last line — `NG` jumps to line `N` |
| `Np` / `N%` | Jump to `N`% through the file |
| `←` / `→` | Horizontal scroll (chop mode) |

**Search**

| Key | Action |
|-----|--------|
| `/pattern` | Search forward (regex) |
| `?pattern` | Search backward (regex) |
| `n` / `N` | Next / previous match |
| `I` | Toggle case-sensitive / case-insensitive search |

While typing a search pattern, paste the clipboard with `Ctrl+V`, `Insert` (Shift+Insert), or right-click.

**Multi-file**

| Command | Action |
|---------|--------|
| `:n` | Next file |
| `:p` | Previous file |
| `:x` | First file |
| `:d` | Remove current file from the list |
| `:e <file>` | Open a file |

**Copy**

| Key / Action | Description |
|--------------|-------------|
| Mouse drag | Select text; automatically copied to clipboard on release |
| `y` | Copy current line to clipboard |
| `Escape` | Clear selection |

**Toggles & Other**

| Key | Action |
|-----|--------|
| `S` | Toggle wrap / chop mode |
| `H` | Toggle syntax highlighting |
| `F` | Follow mode — any key stops |
| `h` | Key reference overlay |
| `=` / `Ctrl+G` | File info |
| `q` / `Q` | Quit |

## License

MIT
