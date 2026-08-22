package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTokenFileTrimsAndRejectsInsecurePaths(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.token")
	if err := os.WriteFile(good, []byte("  token-value  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readTokenFile(good)
	if err != nil || got != "token-value" {
		t.Fatalf("readTokenFile good=(%q,%v)", got, err)
	}

	badMode := filepath.Join(dir, "bad-mode.token")
	if err := os.WriteFile(badMode, []byte("token"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(badMode); err == nil {
		t.Fatal("group-readable token file accepted")
	}

	badLink := filepath.Join(dir, "bad-link.token")
	if err := os.Symlink(good, badLink); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(badLink); err == nil {
		t.Fatal("symlink token path accepted")
	}
}
