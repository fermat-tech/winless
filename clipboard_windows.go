package main

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// openClipboard retries OpenClipboard briefly: another process — Windows'
// own clipboard history, PowerToys, a clipboard manager — can transiently
// hold the clipboard, and OpenClipboard has no built-in wait.
func openClipboard() error {
	var err error
	for i := 0; i < 10; i++ {
		var r uintptr
		r, _, err = procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}

func readClipboard() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call() //nolint:errcheck

	h, _, err := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", err
	}
	ptr, _, err := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", err
	}
	defer procGlobalUnlock.Call(h) //nolint:errcheck

	// find null terminator to determine length
	base := unsafe.Add(unsafe.Pointer(nil), ptr)
	p := (*[1 << 20]uint16)(base)
	var n int
	for n = 0; n < len(p) && p[n] != 0; n++ {
	}
	return syscall.UTF16ToString(p[:n]), nil
}

func writeClipboard(text string) error {
	// CF_UNICODETEXT lines are conventionally CRLF-terminated (MSDN); a bare
	// LF is a deviation some paste consumers mishandle or drop entirely.
	// Normalize first so a prior CRLF isn't doubled to CRCRLF.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")

	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call() //nolint:errcheck

	procEmptyClipboard.Call() //nolint:errcheck

	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(utf16) * 2)
	h, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return err
	}
	ptr, _, err := procGlobalLock.Call(h)
	if ptr == 0 {
		return err
	}
	base := unsafe.Add(unsafe.Pointer(nil), ptr)
	dst := (*[1 << 20]uint16)(base)[:len(utf16):len(utf16)]
	copy(dst, utf16)
	procGlobalUnlock.Call(h) //nolint:errcheck

	r, _, err := procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		return err
	}
	return nil
}
