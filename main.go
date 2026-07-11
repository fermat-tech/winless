// winless is a less-like terminal pager for Windows.
//
// It pages files or piped stdin with syntax highlighting, regex search,
// follow mode (tail -f), mouse-drag copy-to-clipboard, multi-file navigation,
// and an in-pager key reference overlay. A single self-contained .exe with
// no runtime or external dependencies required.
//
// # Install
//
//	go install github.com/fermat-tech/winless@latest
//
// Or download a pre-built binary from the Releases page on GitHub.
//
// # Usage
//
//	winless [options] [file ...]
//	command | winless [options]
//
// When no file is given and stdin is a pipe, winless pages stdin.
// Multiple files may be specified; use :n / :p to move between them.
//
// # Options
//
//	-N, --line-numbers      Show line numbers
//	-S, --chop-long-lines   Chop long lines (no wrap); toggle with S key in-pager
//	-i, --ignore-case       Case-insensitive search (default); toggle with I key
//	-f, --follow            Start in follow mode (like tail -f); any key stops
//	-h, --help              Print help and exit
//	-V, --version           Print version and exit
//
// # Navigation
//
//	↑ / k            Scroll up one line
//	↓ / j            Scroll down one line
//	d / Ctrl+D       Scroll down half a page
//	u / Ctrl+U       Scroll up half a page
//	PgUp / b         Scroll up one page
//	PgDn / Space     Scroll down one page
//	g / Home         Jump to first line  (Ng = jump to line N)
//	G / End          Jump to last line   (NG = jump to line N)
//	Np / N%          Jump to N% of file  (e.g. 50p = midpoint)
//	← / →            Scroll horizontally (chop mode only)
//	Mouse wheel      Scroll three lines up or down
//
// # Search
//
//	/pattern         Search forward (regular expression)
//	?pattern         Search backward
//	n                Jump to next match
//	N                Jump to previous match
//	I                Toggle case-sensitive / case-insensitive search
//
// While typing a pattern, paste the clipboard into the search box with
// Ctrl+V, Insert (Shift+Insert), or right-click.
//
// # Multi-file Commands
//
//	:n    Next file
//	:p    Previous file
//	:x    First file
//	:d    Remove current file from list
//	:e f  Examine (open) file f
//
// # Copy to Clipboard
//
//	Mouse drag     Select text; automatically copied to clipboard on release
//	y              Copy the current top line to the clipboard
//	Escape         Clear the selection highlight
//
// # Toggles and Other Keys
//
//	S   Toggle wrap / chop mode (chop truncates long lines; wrap is the default)
//	H   Toggle syntax highlighting on / off
//	F   Enter follow mode (live tail); press any key to stop
//	h   Show the in-pager key reference overlay
//	=   Show file info (name, current line, total lines, mode)
//	q   Quit
//
// # Syntax Highlighting
//
// Language is detected from the file extension. Supported languages:
// Go, JavaScript, TypeScript, Python, Shell, JSON, YAML, TOML,
// CSS/SCSS, HTML/XML, Markdown, Rust, C, C++, Ruby.
//
// # Follow Mode
//
// Follow mode (F key or -f flag) polls the file every 250 ms and appends
// new lines as they arrive, keeping the view scrolled to the bottom —
// equivalent to tail -f. It requires a real file; stdin is not supported.
// Press any key to exit follow mode.
//
// # Rename-Friendly
//
// The binary reads its own name at startup via os.Args[0], so renaming the
// .exe changes the name shown in the status bar and help text without
// recompiling.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// version is set to the released tag (e.g. "v1.2.1"). Bump it with each release.
const version = "v1.2.2"

// ── command name ─────────────────────────────────────────────────────────────

var cmdName string

func init() {
	cmdName = filepath.Base(os.Args[0])
	cmdName = strings.TrimSuffix(cmdName, filepath.Ext(cmdName))
}

// ── styles ────────────────────────────────────────────────────────────────────

var (
	styleDefault   = tcell.StyleDefault.Background(tcell.ColorReset).Foreground(tcell.ColorReset)
	styleStatusBar = tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	styleSearch    = tcell.StyleDefault.Background(tcell.ColorDarkGreen).Foreground(tcell.ColorWhite)
	styleHighlight = tcell.StyleDefault.Background(tcell.ColorYellow).Foreground(tcell.ColorBlack)
	styleSelection = tcell.StyleDefault.Background(tcell.ColorTeal).Foreground(tcell.ColorWhite)
	styleLineNum   = tcell.StyleDefault.Foreground(tcell.ColorDarkCyan)
	styleError     = tcell.StyleDefault.Background(tcell.ColorMaroon).Foreground(tcell.ColorWhite)
	stylePrompt    = tcell.StyleDefault.Background(tcell.ColorReset).Foreground(tcell.ColorReset).Bold(true)
	styleWrapMark  = tcell.StyleDefault.Foreground(tcell.ColorGray)
)

// ── display row mapping ───────────────────────────────────────────────────────

// drow maps one terminal row to a position within p.lines.
type drow struct {
	lineIdx   int // index into p.lines
	runeStart int // starting rune offset within that line
}

// ── document position ─────────────────────────────────────────────────────────

type docPos struct {
	lineIdx int
	runeOff int
}

func (a docPos) before(b docPos) bool {
	return a.lineIdx < b.lineIdx || (a.lineIdx == b.lineIdx && a.runeOff < b.runeOff)
}

// ── file entry (for multi-file support) ──────────────────────────────────────

type fileEntry struct {
	name string
	path string
	size int64
}

// ── input mode action ─────────────────────────────────────────────────────────

type inputAction int

const (
	actionSearch inputAction = iota
	actionColon
)

// ── pager state ───────────────────────────────────────────────────────────────

type Pager struct {
	screen     tcell.Screen
	lines      []string // raw file lines
	drows      []drow   // display rows; rebuilt on resize / wrap toggle
	builtWidth int      // content width drows was built for
	filename   string
	filepath   string // real path for follow mode (empty for stdin)
	fileSize   int64  // size at open time, for follow offset
	lang       string // detected language for syntax highlighting
	topRow     int    // first visible display row (index into drows)
	leftCol    int    // horizontal scroll offset (no-wrap mode only)
	showLineNum bool  // -N flag
	noWrap      bool  // -S flag: chop long lines instead of wrapping
	syntaxOn    bool  // syntax highlighting toggle (H key)
	helpMode    bool  // showing in-pager help overlay

	// follow mode
	followMode bool
	followStop chan struct{}

	// search
	searchPat      string
	searchRe       *regexp.Regexp
	searchFwd      bool
	caseInsensitive bool
	inputMode      bool
	inputBuf       string
	inputPrompt    rune
	inputAction    inputAction
	statusMsg      string
	statusErr      bool

	// numeric prefix (like vim's count prefix: 42g = go to line 42)
	numBuf string

	// multi-file
	files   []fileEntry
	fileIdx int

	// mouse selection
	selDragging     bool
	selHasSelection bool
	selAnchor       docPos
	selCursor       docPos
}

// ── entry point ───────────────────────────────────────────────────────────────

func main() {
	args := os.Args[1:]
	showLineNum := false
	noWrap := false
	startFollow := false
	caseInsensitive := true
	var filenames []string

	for _, a := range args {
		switch a {
		case "-N", "--line-numbers":
			showLineNum = true
		case "-S", "--chop-long-lines":
			noWrap = true
		case "-i", "--ignore-case":
			caseInsensitive = true
		case "-f", "--follow":
			startFollow = true
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		case "-V", "--version":
			printVersion()
			os.Exit(0)
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "%s: unknown flag %q\n", cmdName, a)
				os.Exit(1)
			}
			filenames = append(filenames, a)
		}
	}

	// build file list
	var files []fileEntry
	var reader io.Reader
	var firstLines []string

	if len(filenames) == 0 {
		fi, err := os.Stdin.Stat()
		if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
			printUsage()
			os.Exit(1)
		}
		reader = os.Stdin
		files = append(files, fileEntry{name: "(stdin)"})
	} else {
		for _, fn := range filenames {
			fi, err := os.Stat(fn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
				os.Exit(1)
			}
			files = append(files, fileEntry{name: fn, path: fn, size: fi.Size()})
		}
		f, err := os.Open(files[0].path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
			os.Exit(1)
		}
		defer f.Close()
		reader = f
	}

	lines, err := readLines(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		os.Exit(1)
	}
	firstLines = lines

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		os.Exit(1)
	}
	if err = screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		os.Exit(1)
	}
	screen.SetStyle(styleDefault)
	screen.EnableMouse(tcell.MouseMotionEvents)
	screen.Clear()

	p := &Pager{
		screen:          screen,
		lines:           firstLines,
		filename:        files[0].name,
		filepath:        files[0].path,
		fileSize:        files[0].size,
		lang:            detectLang(files[0].name),
		showLineNum:     showLineNum,
		noWrap:          noWrap,
		syntaxOn:        true,
		caseInsensitive: caseInsensitive,
		files:           files,
		fileIdx:         0,
	}
	if startFollow {
		p.startFollow()
	}
	p.run()
}

func printUsage() {
	fmt.Println(cmdName + " — a less-like pager for Windows")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  " + cmdName + " [options] [file ...]")
	fmt.Println("  command | " + cmdName + " [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -N, --line-numbers      Show line numbers")
	fmt.Println("  -S, --chop-long-lines   Chop long lines (no wrap); toggle with S key")
	fmt.Println("  -i, --ignore-case       Case-insensitive search (default); toggle with I")
	fmt.Println("  -f, --follow            Start in follow mode (like tail -f)")
	fmt.Println("  -h, --help              Show this help")
	fmt.Println("  -V, --version           Show version and exit")
	fmt.Println()
	fmt.Println("Navigation:")
	fmt.Println("  Arrow keys / j k        Scroll one line")
	fmt.Println("  d / Ctrl+D              Scroll down half page")
	fmt.Println("  u / Ctrl+U              Scroll up half page")
	fmt.Println("  Page Down / Space       Scroll one page")
	fmt.Println("  Page Up / b             Scroll back one page")
	fmt.Println("  Right / Left arrow      Scroll horizontally (chop mode only)")
	fmt.Println("  g / Home                Go to first line  (Ng = jump to line N)")
	fmt.Println("  G / End                 Go to last line   (NG = jump to line N)")
	fmt.Println("  Np / N%                 Jump to N% of file")
	fmt.Println()
	fmt.Println("Search:")
	fmt.Println("  /pattern                Search forward (regex)")
	fmt.Println("  ?pattern                Search backward (regex)")
	fmt.Println("  n                       Next match")
	fmt.Println("  N                       Previous match")
	fmt.Println("  I                       Toggle case sensitivity")
	fmt.Println()
	fmt.Println("Multi-file:")
	fmt.Println("  :n                      Next file")
	fmt.Println("  :p                      Previous file")
	fmt.Println("  :x                      First file")
	fmt.Println("  :d                      Remove current file from list")
	fmt.Println("  :e <file>               Open file")
	fmt.Println()
	fmt.Println("  S                       Toggle wrap/chop mode")
	fmt.Println("  H                       Toggle syntax highlighting")
	fmt.Println("  F                       Follow mode (like tail -f); any key to stop")
	fmt.Println("  h                       Show in-pager key reference")
	fmt.Println("  q / Q                   Quit")
	fmt.Println()
	fmt.Println("Copy:")
	fmt.Println("  Mouse drag              Select text; auto-copied to clipboard on release")
	fmt.Println("  y                       Copy current line to clipboard")
	fmt.Println("  Escape                  Clear selection")
}

func printVersion() {
	fmt.Printf("%s %s\n", cmdName, version)
}

// ── file reading ──────────────────────────────────────────────────────────────

func readLines(r io.Reader) ([]string, error) {
	var lines []string
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			lines = append(lines, strings.TrimRight(line, "\r\n"))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, nil
}

// ── display-row management ────────────────────────────────────────────────────

func (p *Pager) lineNumWidth() int {
	if !p.showLineNum {
		return 0
	}
	return len(strconv.Itoa(len(p.lines))) + 1
}

// buildDrows rebuilds p.drows for the given content width.
func (p *Pager) buildDrows(cw int) {
	if cw < 1 {
		cw = 1
	}
	p.builtWidth = cw
	p.drows = p.drows[:0]
	for i, line := range p.lines {
		runes := []rune(line)
		if p.noWrap || len(runes) == 0 {
			p.drows = append(p.drows, drow{i, 0})
		} else {
			for offset := 0; offset < len(runes); offset += cw {
				p.drows = append(p.drows, drow{i, offset})
			}
		}
	}
}

// ensureDrows rebuilds if the content width has changed.
func (p *Pager) ensureDrows() {
	width, _ := p.screen.Size()
	cw := width - p.lineNumWidth()
	if cw != p.builtWidth {
		var anchorLine int
		if p.topRow < len(p.drows) {
			anchorLine = p.drows[p.topRow].lineIdx
		}
		p.buildDrows(cw)
		p.topRow = p.firstRowForLine(anchorLine)
		p.clampTop()
	}
}

// firstRowForLine returns the first display row for the given file line.
func (p *Pager) firstRowForLine(lineIdx int) int {
	for i, r := range p.drows {
		if r.lineIdx == lineIdx {
			return i
		}
	}
	return 0
}

// ── main loop ─────────────────────────────────────────────────────────────────

func (p *Pager) run() {
	defer p.screen.Fini()

	width, _ := p.screen.Size()
	p.buildDrows(width - p.lineNumWidth())

	for {
		p.ensureDrows()
		p.draw()
		ev := p.screen.PollEvent()
		switch e := ev.(type) {
		case *tcell.EventResize:
			p.screen.Sync()

		case *tcell.EventMouse:
			if p.followMode {
				p.stopFollow()
			}
			p.handleMouse(e)

		case *eventFollow:
			p.appendLines(e.lines)

		case *tcell.EventKey:
			if p.followMode {
				p.stopFollow()
				continue
			}
			if p.inputMode {
				p.handleInputKey(e)
			} else {
				if p.handleNavKey(e) {
					return
				}
			}
		}
	}
}

// ── keyboard: normal mode ─────────────────────────────────────────────────────

func (p *Pager) handleNavKey(ev *tcell.EventKey) bool {
	if p.helpMode {
		p.helpMode = false
		return false
	}

	_, height := p.screen.Size()
	pageSize := height - 1
	halfPage := max(1, pageSize/2)

	// consume and reset numeric prefix; digit keys restore it
	numPrefix := p.numBuf
	p.numBuf = ""

	// digit keys accumulate the numeric prefix without clearing statusMsg
	if ev.Key() == tcell.KeyRune {
		r := ev.Rune()
		if r >= '0' && r <= '9' {
			p.numBuf = numPrefix + string(r)
			return false
		}
	}

	p.statusMsg = ""

	switch ev.Key() {
	case tcell.KeyEscape:
		p.selHasSelection = false
	case tcell.KeyDown, tcell.KeyCtrlN:
		p.scroll(1)
	case tcell.KeyUp, tcell.KeyCtrlP:
		p.scroll(-1)
	case tcell.KeyCtrlD:
		p.scroll(halfPage)
	case tcell.KeyCtrlU:
		p.scroll(-halfPage)
	case tcell.KeyRight:
		if p.noWrap {
			p.leftCol += 8
		}
	case tcell.KeyLeft:
		if p.noWrap && p.leftCol > 0 {
			p.leftCol -= 8
			if p.leftCol < 0 {
				p.leftCol = 0
			}
		}
	case tcell.KeyPgDn, tcell.KeyCtrlF:
		p.scroll(pageSize)
	case tcell.KeyPgUp, tcell.KeyCtrlB:
		p.scroll(-pageSize)
	case tcell.KeyHome:
		p.topRow = 0
		p.leftCol = 0
	case tcell.KeyEnd:
		p.topRow = max(0, len(p.drows)-pageSize)
	case tcell.KeyCtrlG:
		p.showFileInfo()
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q', 'Q':
			return true
		case 'j':
			p.scroll(1)
		case 'k':
			p.scroll(-1)
		case 'd':
			p.scroll(halfPage)
		case 'u':
			p.scroll(-halfPage)
		case 'b':
			p.scroll(-pageSize)
		case ' ':
			p.scroll(pageSize)
		case 'g':
			if numPrefix != "" {
				n, _ := strconv.Atoi(numPrefix)
				p.jumpToLine(n - 1)
			} else {
				p.topRow = 0
				p.leftCol = 0
			}
		case 'G':
			if numPrefix != "" {
				n, _ := strconv.Atoi(numPrefix)
				p.jumpToLine(n - 1)
			} else {
				p.topRow = max(0, len(p.drows)-pageSize)
			}
		case 'p':
			if numPrefix != "" {
				p.jumpToPercent(numPrefix)
			} else {
				p.topRow = 0
				p.leftCol = 0
			}
		case '%':
			if numPrefix != "" {
				p.jumpToPercent(numPrefix)
			} else {
				p.showFileInfo()
			}
		case 'n':
			p.findNext(p.searchFwd)
		case 'N':
			p.findNext(!p.searchFwd)
		case '/':
			p.startInput('/')
		case '?':
			p.startInput('?')
		case ':':
			p.startColonInput()
		case '=':
			p.showFileInfo()
		case 'I':
			p.caseInsensitive = !p.caseInsensitive
			p.recompileSearch()
			if p.caseInsensitive {
				p.setStatus("Case insensitive", false)
			} else {
				p.setStatus("Case sensitive", false)
			}
		case 'S':
			p.toggleWrap()
		case 'H':
			p.syntaxOn = !p.syntaxOn
			if p.syntaxOn {
				p.setStatus("Syntax highlighting on", false)
			} else {
				p.setStatus("Syntax highlighting off", false)
			}
		case 'y':
			p.copyCurrentLine()
		case 'F':
			p.startFollow()
		case 'h':
			p.helpMode = true
		}
	}
	return false
}

// jumpToLine scrolls so that lineIdx (0-based) is at the top.
func (p *Pager) jumpToLine(lineIdx int) {
	if lineIdx < 0 {
		lineIdx = 0
	}
	if lineIdx >= len(p.lines) {
		lineIdx = len(p.lines) - 1
	}
	p.topRow = p.firstRowForLine(lineIdx)
	p.clampTop()
}

// jumpToPercent jumps to N% through the file.
func (p *Pager) jumpToPercent(numStr string) {
	pct, _ := strconv.Atoi(numStr)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	lineIdx := (len(p.lines) - 1) * pct / 100
	p.jumpToLine(lineIdx)
	p.setStatus(fmt.Sprintf("%d%%", pct), false)
}

// ── follow mode ───────────────────────────────────────────────────────────────

func (p *Pager) startFollow() {
	if p.filepath == "" {
		p.setStatus("Follow mode only works with files, not stdin", true)
		return
	}
	if p.followMode {
		return
	}
	p.followMode = true
	p.followStop = make(chan struct{})
	_, height := p.screen.Size()
	p.topRow = max(0, len(p.drows)-(height-1))
	go follower(p.screen, p.filepath, p.fileSize, p.followStop)
}

func (p *Pager) stopFollow() {
	if !p.followMode {
		return
	}
	p.followMode = false
	close(p.followStop)
	p.followStop = nil
	p.setStatus("Follow mode stopped", false)
}

func (p *Pager) appendLines(newLines []string) {
	p.lines = append(p.lines, newLines...)
	p.fileSize += int64(func() int {
		n := 0
		for _, l := range newLines {
			n += len(l) + 1
		}
		return n
	}())
	width, height := p.screen.Size()
	cw := width - p.lineNumWidth()
	p.buildDrows(cw)
	p.topRow = max(0, len(p.drows)-(height-1))
}

func (p *Pager) toggleWrap() {
	var anchorLine int
	if p.topRow < len(p.drows) {
		anchorLine = p.drows[p.topRow].lineIdx
	}
	p.noWrap = !p.noWrap
	p.leftCol = 0
	width, _ := p.screen.Size()
	p.buildDrows(width - p.lineNumWidth())
	p.topRow = p.firstRowForLine(anchorLine)
	p.clampTop()
	if p.noWrap {
		p.setStatus("Chop mode (–S)", false)
	} else {
		p.setStatus("Wrap mode", false)
	}
}

func (p *Pager) showFileInfo() {
	fileLineIdx := 0
	if p.topRow < len(p.drows) {
		fileLineIdx = p.drows[p.topRow].lineIdx
	}
	mode := "wrap"
	if p.noWrap {
		mode = "chop"
	}
	info := fmt.Sprintf("%q  line %d/%d  [%s]", p.filename, fileLineIdx+1, len(p.lines), mode)
	if len(p.files) > 1 {
		info = fmt.Sprintf("(%d/%d) %s", p.fileIdx+1, len(p.files), info)
	}
	p.setStatus(info, false)
}

// ── keyboard: input mode ──────────────────────────────────────────────────────

func (p *Pager) startInput(prompt rune) {
	p.inputMode = true
	p.inputPrompt = prompt
	p.inputBuf = ""
	p.inputAction = actionSearch
}

func (p *Pager) startColonInput() {
	p.inputMode = true
	p.inputPrompt = ':'
	p.inputBuf = ""
	p.inputAction = actionColon
}

func (p *Pager) handleInputKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEnter:
		switch p.inputAction {
		case actionSearch:
			p.commitSearch()
		case actionColon:
			p.commitColonCmd()
		}
		p.inputMode = false
	case tcell.KeyEscape:
		p.inputMode = false
		p.inputBuf = ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(p.inputBuf) > 0 {
			_, sz := utf8.DecodeLastRuneInString(p.inputBuf)
			p.inputBuf = p.inputBuf[:len(p.inputBuf)-sz]
		} else if p.inputAction == actionColon {
			// backspace on empty colon prompt cancels
			p.inputMode = false
		}
	case tcell.KeyCtrlV, tcell.KeyInsert:
		if p.inputAction == actionSearch {
			p.pasteFromClipboard()
		}
	case tcell.KeyRune:
		// single-char colon commands execute immediately (no Enter needed)
		if p.inputAction == actionColon && p.inputBuf == "" {
			switch ev.Rune() {
			case 'n', 'p', 'x', 'd':
				p.inputBuf = string(ev.Rune())
				p.commitColonCmd()
				p.inputMode = false
				return
			}
		}
		p.inputBuf += string(ev.Rune())
	}
}

func (p *Pager) commitSearch() {
	pat := p.inputBuf
	if pat == "" {
		if p.searchPat == "" {
			p.setStatus("No previous search pattern", true)
			return
		}
	} else {
		prefix := ""
		if p.caseInsensitive {
			prefix = "(?i)"
		}
		re, err := regexp.Compile(prefix + pat)
		if err != nil {
			p.setStatus(fmt.Sprintf("Bad pattern: %v", err), true)
			return
		}
		p.searchPat = pat
		p.searchRe = re
	}
	p.searchFwd = (p.inputPrompt == '/')
	p.findNext(p.searchFwd)
}

func (p *Pager) recompileSearch() {
	if p.searchPat == "" {
		return
	}
	prefix := ""
	if p.caseInsensitive {
		prefix = "(?i)"
	}
	re, err := regexp.Compile(prefix + p.searchPat)
	if err != nil {
		return
	}
	p.searchRe = re
}

// ── colon commands ────────────────────────────────────────────────────────────

func (p *Pager) commitColonCmd() {
	cmd := strings.TrimSpace(p.inputBuf)
	switch {
	case cmd == "n":
		p.nextFile()
	case cmd == "p":
		p.prevFile()
	case cmd == "x":
		p.switchToFile(0)
	case cmd == "d":
		p.removeCurrentFile()
	case strings.HasPrefix(cmd, "e"):
		name := strings.TrimSpace(cmd[1:])
		p.examineFile(name)
	default:
		if cmd != "" {
			p.setStatus("Unknown command: :"+cmd, true)
		}
	}
}

func (p *Pager) nextFile() { p.switchToFile(p.fileIdx + 1) }
func (p *Pager) prevFile() { p.switchToFile(p.fileIdx - 1) }

func (p *Pager) switchToFile(idx int) {
	if idx < 0 || idx >= len(p.files) {
		p.setStatus("No more files", true)
		return
	}
	if err := p.loadFile(p.files[idx]); err != nil {
		p.setStatus(err.Error(), true)
		return
	}
	p.fileIdx = idx
	if len(p.files) > 1 {
		p.setStatus(fmt.Sprintf("[%d/%d] %s", p.fileIdx+1, len(p.files), p.filename), false)
	}
}

func (p *Pager) loadFile(entry fileEntry) error {
	if entry.path == "" {
		return fmt.Errorf("cannot reload stdin")
	}
	f, err := os.Open(entry.path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	lines, err := readLines(f)
	if err != nil {
		return err
	}
	if p.followMode {
		p.stopFollow()
	}
	p.lines = lines
	p.filename = entry.name
	p.filepath = entry.path
	p.fileSize = fi.Size()
	p.lang = detectLang(entry.name)
	p.topRow = 0
	p.leftCol = 0
	p.selHasSelection = false
	width, _ := p.screen.Size()
	p.buildDrows(width - p.lineNumWidth())
	return nil
}

func (p *Pager) removeCurrentFile() {
	if len(p.files) <= 1 {
		p.setStatus("Only one file — cannot remove", true)
		return
	}
	p.files = append(p.files[:p.fileIdx], p.files[p.fileIdx+1:]...)
	if p.fileIdx >= len(p.files) {
		p.fileIdx = len(p.files) - 1
	}
	p.switchToFile(p.fileIdx)
}

func (p *Pager) examineFile(name string) {
	if name == "" {
		p.setStatus("Usage: :e <filename>", true)
		return
	}
	fi, err := os.Stat(name)
	if err != nil {
		p.setStatus(err.Error(), true)
		return
	}
	entry := fileEntry{name: name, path: name, size: fi.Size()}
	newFiles := make([]fileEntry, 0, len(p.files)+1)
	newFiles = append(newFiles, p.files[:p.fileIdx+1]...)
	newFiles = append(newFiles, entry)
	newFiles = append(newFiles, p.files[p.fileIdx+1:]...)
	p.files = newFiles
	p.switchToFile(p.fileIdx + 1)
}

// ── search ────────────────────────────────────────────────────────────────────

func (p *Pager) findNext(forward bool) {
	if p.searchRe == nil {
		p.setStatus("No search pattern", true)
		return
	}
	curLine := 0
	if p.topRow < len(p.drows) {
		curLine = p.drows[p.topRow].lineIdx
	}
	n := len(p.lines)

	search := func(start, end, step int) bool {
		for i := start; i != end; i += step {
			if p.searchRe.MatchString(p.lines[i]) {
				p.topRow = p.firstRowForLine(i)
				return true
			}
		}
		return false
	}

	if forward {
		if search(curLine+1, n, 1) {
			return
		}
		if search(0, curLine, 1) {
			p.setStatus("Search wrapped", false)
			return
		}
	} else {
		if search(curLine-1, -1, -1) {
			return
		}
		if search(n-1, curLine, -1) {
			p.setStatus("Search wrapped", false)
			return
		}
	}
	p.setStatus("Pattern not found", true)
}

// ── mouse ─────────────────────────────────────────────────────────────────────

func (p *Pager) handleMouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	btns := ev.Buttons()

	// right-click pastes clipboard into search input
	if btns&tcell.Button2 != 0 && p.inputMode && p.inputAction == actionSearch {
		p.pasteFromClipboard()
		return
	}

	switch {
	case btns&tcell.WheelDown != 0:
		p.scroll(3)
	case btns&tcell.WheelUp != 0:
		p.scroll(-3)
	case btns&tcell.Button1 != 0:
		pos := p.screenToDocPos(y, x)
		if !p.selDragging {
			p.selDragging = true
			p.selHasSelection = true
			p.selAnchor = pos
		}
		p.selCursor = pos
	case btns == tcell.ButtonNone:
		if p.selDragging {
			p.selDragging = false
			p.selCursor = p.screenToDocPos(y, x)
			if text := p.selectionText(); text != "" {
				if err := writeClipboard(text); err != nil {
					p.setStatus("Copy failed: "+err.Error(), true)
				} else {
					p.setStatus("Copied to clipboard", false)
				}
			} else {
				p.selHasSelection = false
			}
		}
	}
}

// ── scrolling ─────────────────────────────────────────────────────────────────

func (p *Pager) scroll(delta int) {
	p.topRow += delta
	p.clampTop()
}

func (p *Pager) clampTop() {
	_, height := p.screen.Size()
	pageSize := height - 1
	maxTop := max(0, len(p.drows)-pageSize)
	if p.topRow > maxTop {
		p.topRow = maxTop
	}
	if p.topRow < 0 {
		p.topRow = 0
	}
}

// ── drawing ───────────────────────────────────────────────────────────────────

func (p *Pager) draw() {
	p.screen.Clear()
	width, height := p.screen.Size()
	pageSize := height - 1
	lnw := p.lineNumWidth()
	cw := width - lnw
	if cw < 1 {
		cw = 1
	}

	for screenRow := 0; screenRow < pageSize; screenRow++ {
		rowIdx := p.topRow + screenRow
		if rowIdx >= len(p.drows) {
			p.screen.SetContent(0, screenRow, '~', nil, styleWrapMark)
			continue
		}

		dr := p.drows[rowIdx]
		fileLineIdx := dr.lineIdx
		isFirstRow := dr.runeStart == 0

		if p.showLineNum {
			if isFirstRow {
				numStr := fmt.Sprintf("%*d ", lnw-1, fileLineIdx+1)
				for i, ch := range numStr {
					p.screen.SetContent(i, screenRow, ch, nil, styleLineNum)
				}
			}
		}

		fullLine := p.lines[fileLineIdx]
		fullRunes := []rune(fullLine)

		var synStyles []tcell.Style
		if p.syntaxOn && p.lang != "" {
			synStyles = colorLine(fullLine, p.lang)
		}

		var visRunes []rune
		visOffset := dr.runeStart
		if p.noWrap {
			visOffset = p.leftCol
			if p.leftCol < len(fullRunes) {
				visRunes = fullRunes[p.leftCol:]
			}
		} else {
			visRunes = fullRunes[dr.runeStart:]
		}

		highlights := p.highlightRanges(fullLine, dr.runeStart, p.leftCol)

		col := lnw
		for runeIdx, r := range visRunes {
			if col >= width {
				break
			}
			style := styleDefault
			absIdx := visOffset + runeIdx
			if synStyles != nil && absIdx < len(synStyles) {
				style = synStyles[absIdx]
			}
			for _, h := range highlights {
				if runeIdx >= h[0] && runeIdx < h[1] {
					style = styleHighlight
					break
				}
			}
			if p.inSelection(fileLineIdx, visOffset+runeIdx) {
				style = styleSelection
			}
			p.screen.SetContent(col, screenRow, r, nil, style)
			col++
		}

		if p.noWrap {
			if len(fullRunes) > p.leftCol+cw {
				p.screen.SetContent(width-1, screenRow, '›', nil, styleWrapMark)
			}
			if p.leftCol > 0 && len(fullRunes) > 0 {
				p.screen.SetContent(lnw, screenRow, '‹', nil, styleWrapMark)
			}
		}
	}

	if p.helpMode {
		p.drawHelp(width, height)
	}
	p.drawStatusBar(width, height)
	p.screen.Show()
}

// highlightRanges returns highlight [start,end) ranges in visRune coordinates.
func (p *Pager) highlightRanges(line string, runeStart, leftCol int) [][2]int {
	if p.searchRe == nil {
		return nil
	}
	offset := runeStart
	if p.noWrap {
		offset = leftCol
	}
	matches := p.searchRe.FindAllStringIndex(line, -1)
	var out [][2]int
	for _, m := range matches {
		rs := utf8.RuneCountInString(line[:m[0]])
		re := utf8.RuneCountInString(line[:m[1]])
		rs -= offset
		re -= offset
		if re <= 0 {
			continue
		}
		if rs < 0 {
			rs = 0
		}
		out = append(out, [2]int{rs, re})
	}
	return out
}

// ── status bar ────────────────────────────────────────────────────────────────

func (p *Pager) drawStatusBar(width, height int) {
	row := height - 1

	if p.inputMode {
		prompt := string(p.inputPrompt) + p.inputBuf
		p.fillRow(row, ' ', styleSearch)
		col := 0
		for _, ch := range prompt {
			if col >= width {
				break
			}
			p.screen.SetContent(col, row, ch, nil, stylePrompt)
			col++
		}
		if col < width {
			p.screen.SetContent(col, row, ' ', nil, stylePrompt.Reverse(true))
		}
		p.screen.ShowCursor(col, row)
		return
	}

	p.screen.HideCursor()

	if p.statusMsg != "" {
		st := styleStatusBar
		if p.statusErr {
			st = styleError
		}
		p.fillRow(row, ' ', st)
		col := 0
		for _, ch := range p.statusMsg {
			if col >= width {
				break
			}
			p.screen.SetContent(col, row, ch, nil, st)
			col++
		}
		return
	}

	p.fillRow(row, ' ', styleStatusBar)

	fileLineIdx := 0
	if p.topRow < len(p.drows) {
		fileLineIdx = p.drows[p.topRow].lineIdx
	}

	left := p.filename
	if len(p.files) > 1 {
		left = fmt.Sprintf("(%d/%d) %s", p.fileIdx+1, len(p.files), left)
	}
	if p.lang != "" && p.syntaxOn {
		left += "  [" + p.lang + "]"
	}
	if p.followMode {
		left += "  [FOLLOW — press any key to stop]"
	}
	if p.searchPat != "" {
		ci := ""
		if !p.caseInsensitive {
			ci = " (case)"
		}
		left += "  [/" + p.searchPat + ci + "]"
	}
	if p.noWrap {
		left += "  [-S]"
	}

	_, h := p.screen.Size()
	ps := h - 1
	lastRow := min(p.topRow+ps, len(p.drows))
	lastLine := 0
	if lastRow > 0 && lastRow <= len(p.drows) {
		lastLine = p.drows[lastRow-1].lineIdx + 1
	}
	pct := 0
	if len(p.lines) > 0 {
		pct = lastLine * 100 / len(p.lines)
	}
	right := fmt.Sprintf("line %d/%d  %d%%", fileLineIdx+1, len(p.lines), pct)

	col := 1
	for _, ch := range left {
		if col >= width-len([]rune(right))-2 {
			break
		}
		p.screen.SetContent(col, row, ch, nil, styleStatusBar)
		col++
	}
	col = width - len([]rune(right)) - 1
	for _, ch := range right {
		p.screen.SetContent(col, row, ch, nil, styleStatusBar)
		col++
	}
}

func (p *Pager) drawHelp(width, height int) {
	lines := []struct{ key, desc string }{
		{"── Navigation ──────────────────", ""},
		{"  ↑ / k,  ↓ / j", "Scroll one line"},
		{"  d / Ctrl+D,  u / Ctrl+U", "Scroll half page down / up"},
		{"  ← / →", "Scroll horizontally (chop mode)"},
		{"  PgUp / b,  PgDn / Space", "Scroll one page"},
		{"  g / Home", "First line  (Ng = go to line N)"},
		{"  G / End", "Last line   (NG = go to line N)"},
		{"  Np / N%", "Jump to N% of file"},
		{"", ""},
		{"── Search ──────────────────────", ""},
		{"  /pattern", "Search forward (regex)"},
		{"  ?pattern", "Search backward (regex)"},
		{"  n", "Next match"},
		{"  N", "Previous match"},
		{"  I", "Toggle case sensitivity"},
		{"", ""},
		{"── Multi-file ──────────────────", ""},
		{"  :n / :p", "Next / previous file"},
		{"  :x", "First file"},
		{"  :d", "Remove current file from list"},
		{"  :e <file>", "Open file"},
		{"", ""},
		{"── Copy ────────────────────────", ""},
		{"  Mouse drag", "Select & auto-copy to clipboard"},
		{"  y", "Copy current line to clipboard"},
		{"  Escape", "Clear selection"},
		{"", ""},
		{"── Toggles & Other ─────────────", ""},
		{"  S", "Toggle wrap / chop mode"},
		{"  H", "Toggle syntax highlighting"},
		{"  F", "Follow mode (tail -f); any key stops"},
		{"  h", "Show this help"},
		{"  = / Ctrl+G", "Show file info"},
		{"  q / Q", "Quit"},
	}

	styleBox  := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	styleHead := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorYellow).Bold(true)
	styleDim  := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorSilver)

	boxW := 56
	boxH := len(lines) + 4
	startX := (width - boxW) / 2
	startY := (height - boxH) / 2
	if startX < 0 { startX = 0 }
	if startY < 0 { startY = 0 }

	for row := startY; row < startY+boxH && row < height; row++ {
		for col := startX; col < startX+boxW && col < width; col++ {
			p.screen.SetContent(col, row, ' ', nil, styleBox)
		}
	}

	title := " " + cmdName + " key reference "
	drawStr := func(row, col int, s string, st tcell.Style) {
		for _, ch := range s {
			if col >= width { break }
			p.screen.SetContent(col, row, ch, nil, st)
			col++
		}
	}
	drawStr(startY+1, startX+2, title, styleHead)

	for i, l := range lines {
		row := startY + 3 + i
		if row >= height { break }
		if l.key == "" { continue }
		if l.desc == "" {
			drawStr(row, startX+1, l.key, styleHead)
		} else {
			drawStr(row, startX+2, l.key, styleBox)
			drawStr(row, startX+26, l.desc, styleDim)
		}
	}

	footer := "  press any key to close  "
	drawStr(startY+boxH-1, startX+2, footer, styleDim)
}

// ── selection helpers ─────────────────────────────────────────────────────────

func (p *Pager) screenToDocPos(screenRow, screenCol int) docPos {
	rowIdx := p.topRow + screenRow
	if rowIdx < 0 {
		return docPos{0, 0}
	}
	if rowIdx >= len(p.drows) {
		last := len(p.lines) - 1
		return docPos{last, len([]rune(p.lines[last]))}
	}
	dr := p.drows[rowIdx]
	lnw := p.lineNumWidth()
	col := screenCol - lnw
	if col < 0 {
		col = 0
	}
	var runeOff int
	if p.noWrap {
		runeOff = p.leftCol + col
	} else {
		runeOff = dr.runeStart + col
	}
	runes := []rune(p.lines[dr.lineIdx])
	if runeOff > len(runes) {
		runeOff = len(runes)
	}
	return docPos{dr.lineIdx, runeOff}
}

func (p *Pager) inSelection(lineIdx, runeOff int) bool {
	if !p.selHasSelection {
		return false
	}
	lo, hi := p.selAnchor, p.selCursor
	if hi.before(lo) {
		lo, hi = hi, lo
	}
	pos := docPos{lineIdx, runeOff}
	return !pos.before(lo) && pos.before(hi)
}

func (p *Pager) selectionText() string {
	if !p.selHasSelection {
		return ""
	}
	lo, hi := p.selAnchor, p.selCursor
	if hi.before(lo) {
		lo, hi = hi, lo
	}
	if lo == hi {
		return ""
	}
	runes := func(i int) []rune { return []rune(p.lines[i]) }
	clamp := func(r []rune, n int) int {
		if n > len(r) {
			return len(r)
		}
		return n
	}
	if lo.lineIdx == hi.lineIdx {
		r := runes(lo.lineIdx)
		return string(r[clamp(r, lo.runeOff):clamp(r, hi.runeOff)])
	}
	var b strings.Builder
	r := runes(lo.lineIdx)
	b.WriteString(string(r[clamp(r, lo.runeOff):]))
	for i := lo.lineIdx + 1; i < hi.lineIdx; i++ {
		b.WriteByte('\n')
		b.WriteString(p.lines[i])
	}
	b.WriteByte('\n')
	r = runes(hi.lineIdx)
	b.WriteString(string(r[:clamp(r, hi.runeOff)]))
	return b.String()
}

func (p *Pager) pasteFromClipboard() {
	text, err := readClipboard()
	if err != nil {
		p.setStatus("Paste failed: "+err.Error(), true)
		return
	}
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, text)
	p.inputBuf += text
}

func (p *Pager) copyCurrentLine() {
	if p.topRow >= len(p.drows) {
		return
	}
	text := p.lines[p.drows[p.topRow].lineIdx]
	if err := writeClipboard(text); err != nil {
		p.setStatus("Copy failed: "+err.Error(), true)
	} else {
		p.setStatus("Copied line to clipboard", false)
	}
}

func (p *Pager) fillRow(row, ch int, style tcell.Style) {
	width, _ := p.screen.Size()
	for col := 0; col < width; col++ {
		p.screen.SetContent(col, row, rune(ch), nil, style)
	}
}

func (p *Pager) setStatus(msg string, isErr bool) {
	p.statusMsg = msg
	p.statusErr = isErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
