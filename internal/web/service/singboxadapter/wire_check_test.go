package singboxadapter

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestManagedRelayWireConfigPassesSingboxCheck(t *testing.T) {
	binary := os.Getenv("SINGBOX_CHECK_BIN")
	if binary == "" {
		t.Skip("SINGBOX_CHECK_BIN is not set")
	}
	if runtime.GOOS == "windows" {
		t.Skip("test uses openssl certificate generation")
	}
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	cmd := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", cert, "-subj", "/CN=localhost", "-days", "1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test certificate: %v: %s", err, output)
	}
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-hk1", ListenPort: 29001, CertificatePath: cert, KeyPath: key,
		Users: []ManagedRelayUser{{Email: "alice@example.com", Password: "alice-secret", ExitTag: "us2"}},
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443, Password: "exit-secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"log":       map[string]any{"level": "error"},
		"inbounds":  []any{},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	candidate, err := EncodeManagedConfig(legacy, fragment)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(candidate, &decoded); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(dir, "candidate.json")
	if err := os.WriteFile(candidatePath, candidate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSingBoxConfig(context.Background(), binary, candidatePath); err != nil {
		t.Fatalf("sing-box check rejected generated wire config: %v", err)
	}
}
