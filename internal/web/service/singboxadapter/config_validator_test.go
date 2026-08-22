package singboxadapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateSingBoxConfigRunsExactArgumentsAndReturnsSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX executable")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "args")
	binary := filepath.Join(dir, "sing-box-check")
	script := "#!/bin/sh\n" + "printf '%s\\n' \"$@\" > " + shellQuoteForTest(marker) + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "candidate.json")
	if err := os.WriteFile(config, []byte(`{"log":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSingBoxConfig(context.Background(), binary, config); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(lines) != 3 || lines[0] != "check" || lines[1] != "-c" || lines[2] != config {
		t.Fatalf("args=%q", lines)
	}
}

func TestValidateSingBoxConfigReturnsOutputAndHonorsCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX executable")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "sing-box-check")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'bad config\\n' >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := ValidateSingBoxConfig(context.Background(), binary, filepath.Join(dir, "candidate.json"))
	if err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("validation err=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if err := ValidateSingBoxConfig(ctx, binary, "candidate.json"); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("canceled err=%v", err)
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
