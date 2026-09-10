package main

import (
	"strings"
	"testing"
)

func TestClipboardRoundTrip(t *testing.T) {
	want := "winless clipboard round-trip test — with unicode: héllo wörld 你好"
	if err := writeClipboard(want); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch:\n want %q\n got  %q", want, got)
	}
}

// TestClipboardMultilineUsesCRLF pins the fix for multi-line copies being
// joined into one line on paste: CF_UNICODETEXT is conventionally
// CRLF-terminated, and some paste consumers mishandle or drop a bare LF.
func TestClipboardMultilineUsesCRLF(t *testing.T) {
	input := "line one\nline two\nline three"
	if err := writeClipboard(input); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	want := "line one\r\nline two\r\nline three"
	if got != want {
		t.Fatalf("expected CRLF line endings on the clipboard:\n want %q\n got  %q", want, got)
	}
	if strings.Contains(got, "\n") && !strings.Contains(got, "\r\n") {
		t.Fatalf("found bare LF without CR: %q", got)
	}
}

// TestClipboardDoesNotDoubleExistingCRLF guards against \r\n becoming \r\r\n
// if a caller ever passes text that's already CRLF-terminated.
func TestClipboardDoesNotDoubleExistingCRLF(t *testing.T) {
	input := "already\r\ncrlf\r\ntext"
	if err := writeClipboard(input); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard: %v", err)
	}
	if got != input {
		t.Fatalf("CRLF should be idempotent:\n want %q\n got  %q", input, got)
	}
}
