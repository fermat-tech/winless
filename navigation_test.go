package main

import (
	"regexp"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// newTestPager builds a Pager against tcell's own SimulationScreen (its
// built-in test backend, not a stand-in) so tests drive the exact production
// key-handling and scroll/clamp code paths without a real console.
func newTestPager(t *testing.T, lines []string, width, height int) *Pager {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(width, height)
	t.Cleanup(screen.Fini)

	p := &Pager{
		screen:   screen,
		lines:    lines,
		filename: "test",
		syntaxOn: true,
	}
	p.buildDrows(width - p.lineNumWidth())
	return p
}

func pressRune(p *Pager, r rune) {
	p.handleNavKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
}

func pressKey(p *Pager, k tcell.Key) {
	p.handleNavKey(tcell.NewEventKey(k, 0, tcell.ModNone))
}

// TestYankShortFileDownArrow is the second report on the same underlying
// bug: on the 2-line file, "g" (top) then Down arrow then "y" must yank
// line 2. Down arrow is scroll(1), a single-line move — but on a file that
// already fits entirely on one screen, topRow is pinned at 0 by clampTop
// (there's nothing left to scroll into), so the old syncCurLine()-only
// scroll() just re-read the same pinned topRow and curLine never advanced.
func TestYankShortFileDownArrow(t *testing.T) {
	lines := []string{
		`@ECHO OFF`,
		`"C:\Program Files\Eclipse Adoptium\jdk-21.0.6.7-hotspot\bin\java.exe" -jar %~d0%~p0google-java-format-1.32.0-all-deps.jar %*`,
	}
	p := newTestPager(t, lines, 80, 30)

	pressRune(p, 'g')
	pressKey(p, tcell.KeyDown)

	if p.topRow != 0 {
		t.Fatalf("expected topRow to stay pinned at 0 (whole file fits on screen), got %d", p.topRow)
	}
	if p.curLine != 1 {
		t.Fatalf("curLine after g + Down: got %d, want 1", p.curLine)
	}

	p.copyCurrentLine()
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != lines[1] {
		t.Fatalf("yanked line:\n got  %q\n want %q", got, lines[1])
	}

	// and Up should walk back to line 1
	pressKey(p, tcell.KeyUp)
	if p.curLine != 0 {
		t.Fatalf("curLine after Up: got %d, want 0", p.curLine)
	}
	p.copyCurrentLine()
	got, err = readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != lines[0] {
		t.Fatalf("yanked line after Up:\n got  %q\n want %q", got, lines[0])
	}
}

// TestDownArrowScrollsOneLineNotPage guards the down-arrow-is-not-PgDn
// expectation on a long file: repeated Down must move the viewport by
// exactly one row at a time, unchanged by the pinned-curLine nudge (which
// only applies once the viewport itself can't move any further).
func TestDownArrowScrollsOneLineNotPage(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i%26))
	}
	p := newTestPager(t, lines, 80, 11) // pageSize=10, plenty of room to scroll

	for i := 0; i < 5; i++ {
		pressKey(p, tcell.KeyDown)
	}
	if p.topRow != 5 {
		t.Fatalf("topRow after 5x Down: got %d, want 5 (one line per press)", p.topRow)
	}
	if p.curLine != 5 {
		t.Fatalf("curLine after 5x Down: got %d, want 5", p.curLine)
	}
}

// TestYankLongFileFinalPageWalkedByArrows: once the viewport reaches the
// document's final page (topRow pinned at maxTop, further Down presses
// can't scroll), Down should still walk curLine one line at a time through
// the remaining fully-visible lines — the same mechanism as the short-file
// case, just reached by scrolling there first instead of a file that fits
// on one screen from the start.
func TestYankLongFileFinalPageWalkedByArrows(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i))
	}
	p := newTestPager(t, lines, 80, 11) // pageSize=10, maxTop=10

	pressRune(p, 'G') // jump to bottom: topRow=maxTop=10 (lines K..T, idx 10..19)
	if p.topRow != 10 {
		t.Fatalf("topRow after G: got %d, want 10", p.topRow)
	}
	if p.curLine != 10 {
		t.Fatalf("curLine after G: got %d, want 10", p.curLine)
	}

	// walk down through the remaining 9 lines one at a time
	for want := 11; want <= 19; want++ {
		pressKey(p, tcell.KeyDown)
		if p.topRow != 10 {
			t.Fatalf("topRow should stay pinned at 10, got %d (want=%d)", p.topRow, want)
		}
		if p.curLine != want {
			t.Fatalf("curLine after Down: got %d, want %d", p.curLine, want)
		}
	}

	// one more Down past the last line should not go out of range
	pressKey(p, tcell.KeyDown)
	if p.curLine != 19 {
		t.Fatalf("curLine past EOF: got %d, want clamped at 19", p.curLine)
	}

	p.copyCurrentLine()
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != lines[19] {
		t.Fatalf("yanked line:\n got  %q\n want %q", got, lines[19])
	}
}

// TestYankShortFileJumpToLine is the exact bug report: a 2-line file (like
// C:\Users\na\JKBIN\google-java-format.cmd), "2g" then "y" must yank line 2,
// not line 1. Before curLine existed, clampTop() forced topRow back to 0
// because the whole 2-line file already fits on screen, and 'y' (reading
// straight off topRow) silently yanked the wrong line.
func TestYankShortFileJumpToLine(t *testing.T) {
	lines := []string{
		`@ECHO OFF`,
		`"C:\Program Files\Eclipse Adoptium\jdk-21.0.6.7-hotspot\bin\java.exe" -jar %~d0%~p0google-java-format-1.32.0-all-deps.jar %*`,
	}
	p := newTestPager(t, lines, 80, 30) // page taller than the 2-line file

	pressRune(p, '2')
	pressRune(p, 'g')

	if p.curLine != 1 {
		t.Fatalf("curLine after 2g: got %d, want 1", p.curLine)
	}
	if p.topRow != 0 {
		t.Fatalf("expected topRow still clamped to 0 (file fits on one screen), got %d", p.topRow)
	}

	p.copyCurrentLine()
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != lines[1] {
		t.Fatalf("yanked line:\n got  %q\n want %q", got, lines[1])
	}
}

// TestYankLongFileFinalPage covers the broader case: a line within the
// document's final page (topRow can't reach it either) on an otherwise
// long, normal file — not just a whole-file-fits-on-one-screen file.
func TestYankLongFileFinalPage(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i))
	}
	height := 11 // pageSize = height-1 = 10; maxTop for 20 lines = 10
	p := newTestPager(t, lines, 80, height)

	// jump to line 15 (0-based idx 14) — within the final page, topRow
	// can only reach maxTop=10 (line 11), never 14.
	pressRune(p, '1')
	pressRune(p, '5')
	pressRune(p, 'g')

	if p.curLine != 14 {
		t.Fatalf("curLine after 15g: got %d, want 14", p.curLine)
	}
	if p.topRow == 14 {
		t.Fatalf("expected topRow to NOT reach 14 (clamped), but it did — test setup is wrong")
	}

	p.copyCurrentLine()
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != lines[14] {
		t.Fatalf("yanked line:\n got  %q\n want %q", got, lines[14])
	}
}

// TestYankOrdinaryScrollStillTopOfScreen is the regression guard: plain
// scrolling (no line-specific jump) must keep yanking whatever is visibly at
// the top of the screen, exactly as before curLine was introduced.
func TestYankOrdinaryScrollStillTopOfScreen(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i))
	}
	p := newTestPager(t, lines, 80, 11) // pageSize=10

	pressRune(p, 'j') // scroll down one line -> topRow=1, line B at top
	pressRune(p, 'j') // topRow=2, line C at top

	if p.topRow != 2 {
		t.Fatalf("topRow after 2x 'j': got %d, want 2", p.topRow)
	}

	p.copyCurrentLine()
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != lines[2] {
		t.Fatalf("yanked line after ordinary scroll:\n got  %q\n want %q", got, lines[2])
	}
}

// TestYankAfterSearchMatch: 'y' after a search jump should yank the matched
// line, even when the match falls within the document's final page.
func TestYankAfterSearchMatch(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "filler"
	}
	lines[16] = "NEEDLE"
	p := newTestPager(t, lines, 80, 11) // pageSize=10, maxTop=10 — line 16 is in the final page

	p.searchPat = "NEEDLE"
	p.searchRe = regexp.MustCompile("NEEDLE")
	p.findNext(true)

	if p.curLine != 16 {
		t.Fatalf("curLine after search match: got %d, want 16", p.curLine)
	}

	p.copyCurrentLine()
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != "NEEDLE" {
		t.Fatalf("yanked line after search:\n got  %q\n want %q", got, "NEEDLE")
	}
}
