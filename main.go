// winless is a less-like terminal pager for Windows.
//
// It pages files or piped stdin with syntax highlighting, regex search,
// follow mode (tail -f), mouse-drag copy-to-clipboard, and an in-pager
// key reference overlay. A single self-contained .exe with no runtime
// or external dependencies required.
//
// # Install
//
//	go install github.com/fermat-tech/winless@latest
//
// Or download a pre-built binary from the Releases page on GitHub.
//
// # Usage
//
//	winless [options] <file>
//	command | winless [options]
//
// When no file is given and stdin is a pipe, winless pages stdin.
//
// # Options
//
//	-N, --line-numbers      Show line numbers
//	-S, --chop-long-lines   Chop long lines (no wrap); toggle with S key in-pager
//	-f, --follow            Start in follow mode (like tail -f); any key stops
//	-h, --help              Print help and exit
//
// # Navigation
//
//	↑ / k          Scroll up one line
//	↓ / j          Scroll down one line
//	PgUp / b       Scroll up one page
//	PgDn / Space   Scroll down one page
//	g / Home       Jump to first line
//	G / End        Jump to last line
//	← / →          Scroll horizontally (chop mode only)
//	Mouse wheel    Scroll three lines up or down
//
// # Search
//
//	/pattern       Search forward (regular expression, case-insensitive)
//	?pattern       Search backward
//	n              Jump to next match
//	N              Jump to previous match
//
// While typing a pattern, paste the clipboard into the search box with
// Ctrl+V, Insert (Shift+Insert), or right-click.
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

// ── pager state ───────────────────────────────────────────────────────────────

type Pager struct {
	screen      tcell.Screen
	lines       []string // raw file lines
	drows       []drow   // display rows; rebuilt on resize / wrap toggle
	builtWidth  int      // content width drows was built for
	filename    string
	filepath    string // real path for follow mode (empty for stdin)
	fileSize    int64  // size at open time, for follow offset
	lang        string // detected language for syntax highlighting
	topRow      int    // first visible display row (index into drows)
	leftCol     int    // horizontal scroll offset (no-wrap mode only)
	showLineNum bool   // -N flag
	noWrap      bool   // -S flag: chop long lines instead of wrapping
	syntaxOn    bool   // syntax highlighting toggle (H key)
	helpMode    bool   // showing in-pager help overlay

	// follow mode
	followMode bool
	followStop chan struct{}

	// search
	searchPat   string
	searchRe    *regexp.Regexp
	searchFwd   bool
	inputMode   bool
	inputBuf    string
	inputPrompt rune
	statusMsg   string
	statusErr   bool

	// mouse selection
	selDragging    bool
	selHasSelection bool
	selAnchor      docPos
	selCursor      docPos
}

// ── entry point ───────────────────────────────────────────────────────────────

func main() {
	args := os.Args[1:]
	showLineNum := false
	noWrap := false
	startFollow := false
	var filename string

	for _, a := range args {
		switch a {
		case "-N", "--line-numbers":
			showLineNum = true
		case "-S", "--chop-long-lines":
			noWrap = true
		case "-f", "--follow":
			startFollow = true
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "%s: unknown flag %q\n", cmdName, a)
				os.Exit(1)
			}
			filename = a
		}
	}

	var reader io.Reader
	var filePath string
	var fileSize int64
	if filename == "" {
		fi, err := os.Stdin.Stat()
		if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
			printUsage()
			os.Exit(1)
		}
		reader = os.Stdin
		filename = "(stdin)"
	} else {
		f, err := os.Open(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
			os.Exit(1)
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
			os.Exit(1)
		}
		filePath = filename
		fileSize = fi.Size()
		reader = f
	}

	lines, err := readLines(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		os.Exit(1)
	}

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
		screen:      screen,
		lines:       lines,
		filename:    filename,
		filepath:    filePath,
		fileSize:    fileSize,
		lang:        detectLang(filename),
		showLineNum: showLineNum,
		noWrap:      noWrap,
		syntaxOn:    true,
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
	fmt.Println("  " + cmdName + " [options] <file>")
	fmt.Println("  command | " + cmdName + " [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -N, --line-numbers      Show line numbers")
	fmt.Println("  -S, --chop-long-lines   Chop long lines (no wrap); toggle with S key")
	fmt.Println("  -f, --follow            Start in follow mode (like tail -f)")
	fmt.Println("  -h, --help              Show this help")
	fmt.Println()
	fmt.Println("Navigation:")
	fmt.Println("  Arrow keys / j k        Scroll one line")
	fmt.Println("  Page Down / Space       Scroll one page")
	fmt.Println("  Page Up / b             Scroll back one page")
	fmt.Println("  Right / Left arrow      Scroll horizontally (chop mode only)")
	fmt.Println("  g / Home                Go to first line")
	fmt.Println("  G / End                 Go to last line")
	fmt.Println()
	fmt.Println("Search:")
	fmt.Println("  /pattern                Search forward")
	fmt.Println("  ?pattern                Search backward")
	fmt.Println("  n                       Next match")
	fmt.Println("  N                       Previous match")
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
		// preserve file-line position across rebuild
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

	// initial build
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
				// consume the keypress — don't also act on it
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
	// any key dismisses the help overlay
	if p.helpMode {
		p.helpMode = false
		return false
	}

	_, height := p.screen.Size()
	pageSize := height - 1
	p.statusMsg = ""

	switch ev.Key() {
	case tcell.KeyEscape:
		p.selHasSelection = false
	case tcell.KeyDown, tcell.KeyCtrlN:
		p.scroll(1)
	case tcell.KeyUp, tcell.KeyCtrlP:
		p.scroll(-1)
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
		case 'b':
			p.scroll(-pageSize)
		case ' ':
			p.scroll(pageSize)
		case 'g':
			p.topRow = 0
			p.leftCol = 0
		case 'G':
			p.topRow = max(0, len(p.drows)-pageSize)
		case 'n':
			p.findNext(p.searchFwd)
		case 'N':
			p.findNext(!p.searchFwd)
		case '/':
			p.startInput('/')
		case '?':
			p.startInput('?')
		case '=':
			p.showFileInfo()
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
	// scroll to bottom first
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
	// track the current file-line anchor so we can restore position
	p.lines = append(p.lines, newLines...)
	p.fileSize += int64(func() int {
		n := 0
		for _, l := range newLines {
			n += len(l) + 1
		}
		return n
	}())

	// rebuild display rows and scroll to bottom
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
	p.setStatus(fmt.Sprintf("%q  line %d/%d  [%s]", p.filename, fileLineIdx+1, len(p.lines), mode), false)
}

// ── keyboard: input mode ──────────────────────────────────────────────────────

func (p *Pager) startInput(prompt rune) {
	p.inputMode = true
	p.inputPrompt = prompt
	p.inputBuf = ""
}

func (p *Pager) handleInputKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEnter:
		p.commitSearch()
		p.inputMode = false
	case tcell.KeyEscape:
		p.inputMode = false
		p.inputBuf = ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(p.inputBuf) > 0 {
			_, sz := utf8.DecodeLastRuneInString(p.inputBuf)
			p.inputBuf = p.inputBuf[:len(p.inputBuf)-sz]
		}
	case tcell.KeyCtrlV, tcell.KeyInsert: // Ctrl+V or Shift+Insert (terminals send same Insert escape)
		p.pasteFromClipboard()
	case tcell.KeyRune:
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
		re, err := regexp.Compile("(?i)" + pat)
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
	if btns&tcell.Button2 != 0 && p.inputMode {
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

		// line number gutter
		if p.showLineNum {
			if isFirstRow {
				numStr := fmt.Sprintf("%*d ", lnw-1, fileLineIdx+1)
				for i, ch := range numStr {
					p.screen.SetContent(i, screenRow, ch, nil, styleLineNum)
				}
			}
			// continuation rows: leave gutter blank (already cleared)
		}

		fullLine := p.lines[fileLineIdx]
		fullRunes := []rune(fullLine)

		// per-rune syntax styles for the full line
		var synStyles []tcell.Style
		if p.syntaxOn && p.lang != "" {
			synStyles = colorLine(fullLine, p.lang)
		}

		// slice to the visible portion
		var visRunes []rune
		visOffset := dr.runeStart // rune offset of visRunes[0] within fullRunes
		if p.noWrap {
			visOffset = p.leftCol
			if p.leftCol < len(fullRunes) {
				visRunes = fullRunes[p.leftCol:]
			}
		} else {
			visRunes = fullRunes[dr.runeStart:]
		}

		// search highlight ranges mapped into vis-rune coordinates
		highlights := p.highlightRanges(fullLine, dr.runeStart, p.leftCol)

		// draw runes
		col := lnw
		for runeIdx, r := range visRunes {
			if col >= width {
				break
			}
			// base style: syntax or default
			style := styleDefault
			absIdx := visOffset + runeIdx
			if synStyles != nil && absIdx < len(synStyles) {
				style = synStyles[absIdx]
			}
			// search highlight overrides syntax
			for _, h := range highlights {
				if runeIdx >= h[0] && runeIdx < h[1] {
					style = styleHighlight
					break
				}
			}
			// selection overrides everything
			if p.inSelection(fileLineIdx, visOffset+runeIdx) {
				style = styleSelection
			}
			p.screen.SetContent(col, screenRow, r, nil, style)
			col++
		}

		// overflow indicators (chop mode)
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
// runeStart is the wrap offset; leftCol is the chop offset.
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
		// shift to vis-rune coordinates
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
	if p.lang != "" && p.syntaxOn {
		left += "  [" + p.lang + "]"
	}
	if p.followMode {
		left += "  [FOLLOW — press any key to stop]"
	}
	if p.searchPat != "" {
		left += "  [/" + p.searchPat + "]"
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
		{"  ← / →", "Scroll horizontally (chop mode)"},
		{"  PgUp / b,  PgDn / Space", "Scroll one page"},
		{"  g / Home", "Go to first line"},
		{"  G / End", "Go to last line"},
		{"", ""},
		{"── Search ──────────────────────", ""},
		{"  /pattern", "Search forward (regex)"},
		{"  ?pattern", "Search backward (regex)"},
		{"  n", "Next match"},
		{"  N", "Previous match"},
		{"", ""},
		{"── Toggles ─────────────────────", ""},
		{"  S", "Toggle wrap / chop mode"},
		{"  H", "Toggle syntax highlighting"},
		{"  F", "Follow mode (tail -f); any key stops"},
		{"", ""},
		{"── Copy ────────────────────────", ""},
		{"  Mouse drag", "Select & auto-copy to clipboard"},
		{"  y", "Copy current line to clipboard"},
		{"  Escape", "Clear selection"},
		{"", ""},
		{"── Other ───────────────────────", ""},
		{"  h", "Show this help"},
		{"  = / Ctrl+G", "Show file info"},
		{"  q / Q", "Quit"},
	}

	styleBox  := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	styleHead := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorYellow).Bold(true)
	styleDim  := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorSilver)

	boxW := 52
	boxH := len(lines) + 4 // title + blank + lines + footer
	startX := (width - boxW) / 2
	startY := (height - boxH) / 2
	if startX < 0 { startX = 0 }
	if startY < 0 { startY = 0 }

	// fill box background
	for row := startY; row < startY+boxH && row < height; row++ {
		for col := startX; col < startX+boxW && col < width; col++ {
			p.screen.SetContent(col, row, ' ', nil, styleBox)
		}
	}

	// title
	title := " " + cmdName + " key reference "
	drawStr := func(row, col int, s string, st tcell.Style) {
		for _, ch := range s {
			if col >= width { break }
			p.screen.SetContent(col, row, ch, nil, st)
			col++
		}
	}
	drawStr(startY+1, startX+2, title, styleHead)

	// key rows
	for i, l := range lines {
		row := startY + 3 + i
		if row >= height { break }
		if l.key == "" { continue }
		if l.desc == "" {
			// section header
			drawStr(row, startX+1, l.key, styleHead)
		} else {
			drawStr(row, startX+2, l.key, styleBox)
			drawStr(row, startX+22, l.desc, styleDim)
		}
	}

	// footer
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
	// strip newlines — search pattern must be single-line
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
