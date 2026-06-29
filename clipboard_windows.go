package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
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

func writeClipboard(text string) error {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
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
	dst := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:len(utf16):len(utf16)]
	copy(dst, utf16)
	procGlobalUnlock.Call(h) //nolint:errcheck

	r, _, err = procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		return err
	}
	return nil
}
