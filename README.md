# winless

A `less`-like terminal pager for Windows — single binary, no runtime, no dependencies to install.

## Features

- Scroll files or piped stdin in the terminal
- Syntax highlighting for Go, JS/TS, Python, Rust, C/C++, Shell, JSON, YAML, TOML, CSS, HTML, Markdown, Ruby
- Regex search forward and backward with match highlighting
- **Mouse drag to select and auto-copy to clipboard** — drag to highlight, text is copied on release
- **`y` key** — copy the current line to clipboard instantly
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
winless [options] <file>
command | winless [options]
```

**Options**

| Flag | Description |
|------|-------------|
| `-N`, `--line-numbers` | Show line numbers |
| `-S`, `--chop-long-lines` | Chop long lines instead of wrapping |
| `-f`, `--follow` | Start in follow mode (like `tail -f`) |
| `-h`, `--help` | Show help |

## Key Bindings

**Navigation**

| Key | Action |
|-----|--------|
| `↑` / `k`, `↓` / `j` | Scroll one line |
| `PgUp` / `b`, `PgDn` / `Space` | Scroll one page |
| `g` / `Home` | First line |
| `G` / `End` | Last line |
| `←` / `→` | Horizontal scroll (chop mode) |

**Search**

| Key | Action |
|-----|--------|
| `/pattern` | Search forward (regex) |
| `?pattern` | Search backward (regex) |
| `n` / `N` | Next / previous match |

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
