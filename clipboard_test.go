package main

import "testing"

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
