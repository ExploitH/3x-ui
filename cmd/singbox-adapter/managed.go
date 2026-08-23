package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/singboxadapter"
)

func managedMutatorFromEnvironment(configPath string) (*singboxadapter.ManagedRelayMutator, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("SINGBOX_ADAPTER_MANAGED")), "true") {
		return nil, nil
	}
	statePath := envOr("SINGBOX_ADAPTER_MANAGED_STATE", "/etc/sing-box/managed/managed-relay-state.json")
	binaryPath := envOr("SINGBOX_ADAPTER_SINGBOX_BIN", "/usr/local/bin/sing-box")
	serviceName := envOr("SINGBOX_ADAPTER_SINGBOX_SERVICE", "sing-box.service")
	if strings.TrimSpace(configPath) == "" || strings.TrimSpace(statePath) == "" || strings.TrimSpace(binaryPath) == "" || strings.TrimSpace(serviceName) == "" {
		return nil, fmt.Errorf("managed adapter paths and service name are required")
	}
	mutator := &singboxadapter.ManagedRelayMutator{
		State:      singboxadapter.NewManagedRelayStateStore(statePath),
		Config:     singboxadapter.NewManagedConfigStore(configPath),
		BinaryPath: binaryPath,
		Reload: func(ctx context.Context) error {
			return runSystemctl(ctx, "reload", serviceName)
		},
		Health: func(ctx context.Context) error {
			if err := singboxCheck(ctx, binaryPath, configPath); err != nil {
				return err
			}
			return runSystemctl(ctx, "is-active", "--quiet", serviceName)
		},
	}
	if err := mutator.Ready(context.Background()); err != nil {
		return nil, fmt.Errorf("managed adapter is not ready: %w", err)
	}
	return mutator, nil
}

func runSystemctl(ctx context.Context, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl operation failed")
	}
	return nil
}

func singboxCheck(ctx context.Context, binaryPath, configPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binaryPath, "check", "-c", configPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sing-box config check failed")
	}
	return nil
}
