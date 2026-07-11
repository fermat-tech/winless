package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

// ── follow-mode event ─────────────────────────────────────────────────────────

// eventFollow is posted by the follower goroutine when new lines arrive.
type eventFollow struct {
	tcell.EventTime
	lines []string // newly appended lines
}

// ── follower goroutine ────────────────────────────────────────────────────────

// follower polls a file for new content and posts eventFollow to the screen.
// It exits when stopCh is closed.
func follower(screen tcell.Screen, path string, startOffset int64, stopCh <-chan struct{}) {
	offset := startOffset
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			info, err := f.Stat()
			if err != nil {
				f.Close()
				continue
			}
			if info.Size() <= offset {
				f.Close()
				continue
			}
			// seek to where we left off
			if _, err = f.Seek(offset, io.SeekStart); err != nil {
				f.Close()
				continue
			}
			var newLines []string
			br := bufio.NewReaderSize(f, 64*1024)
			for {
				line, err := br.ReadString('\n')
				if len(line) > 0 {
					newLines = append(newLines, strings.TrimRight(line, "\r\n"))
				}
				if err != nil {
					break
				}
			}
			newOffset, _ := f.Seek(0, io.SeekCurrent)
			f.Close()

			if len(newLines) == 0 {
				continue
			}
			offset = newOffset

			ev := &eventFollow{lines: newLines}
			ev.SetEventNow()
			screen.PostEvent(ev) //nolint:errcheck
		}
	}
}
