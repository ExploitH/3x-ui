package singboxadapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManagedConfigStoreApplyWithSingBoxCheckRejectsCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX executable")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	original := []byte(`{"log":{"level":"info"}}\n`)
	if err := os.WriteFile(config, original, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "sing-box-check")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'invalid candidate\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := NewManagedConfigStore(config).ApplyWithSingBoxCheck(context.Background(), []byte(`{"log":{"level":"warn"}}\n`), binary)
	if err == nil || !strings.Contains(err.Error(), "invalid candidate") {
		t.Fatalf("err=%v", err)
	}
	got, readErr := os.ReadFile(config)
	if readErr != nil || string(got) != string(original) {
		t.Fatalf("config=%q err=%v", got, readErr)
	}
}
