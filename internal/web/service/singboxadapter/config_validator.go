package singboxadapter

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const maxSingboxCheckOutput = 64 * 1024

// ValidateSingBoxConfig runs the sing-box config checker without invoking a
// shell. The caller supplies the exact binary path and the candidate file path.
func ValidateSingBoxConfig(ctx context.Context, binaryPath, configPath string) error {
	if ctx == nil {
		return errors.New("sing-box validation context is nil")
	}
	binaryPath = strings.TrimSpace(binaryPath)
	configPath = strings.TrimSpace(configPath)
	if binaryPath == "" {
		return errors.New("sing-box binary path is required")
	}
	if configPath == "" {
		return errors.New("sing-box config path is required")
	}

	cmd := exec.CommandContext(ctx, binaryPath, "check", "-c", configPath)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("sing-box config validation canceled: %w", ctxErr)
	}
	return fmt.Errorf("sing-box config validation failed: %w: %s", err, boundedCheckOutput(output))
}

func boundedCheckOutput(output []byte) string {
	if len(output) <= maxSingboxCheckOutput {
		return strings.TrimSpace(string(output))
	}
	return strings.TrimSpace(string(output[:maxSingboxCheckOutput])) + "…"
}
