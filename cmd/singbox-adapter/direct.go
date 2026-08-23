package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/singboxadapter"
)

func directMutatorFromEnvironment(configPath string) (*singboxadapter.DirectInboundMutator, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("SINGBOX_ADAPTER_DIRECT_MANAGED")), "true") {
		return nil, nil
	}
	statePath := envOr("SINGBOX_ADAPTER_DIRECT_STATE", "/etc/sing-box/managed/direct-client-state.json")
	binaryPath := envOr("SINGBOX_ADAPTER_SINGBOX_BIN", "/usr/local/bin/sing-box")
	serviceName := envOr("SINGBOX_ADAPTER_SINGBOX_SERVICE", "sing-box.service")
	rawTags := strings.Split(os.Getenv("SINGBOX_ADAPTER_DIRECT_INBOUND_TAGS"), ",")
	tags := make([]string, 0, len(rawTags))
	for _, raw := range rawTags {
		if tag := strings.TrimSpace(raw); tag != "" {
			tags = append(tags, tag)
		}
	}
	if strings.TrimSpace(configPath) == "" || strings.TrimSpace(statePath) == "" || strings.TrimSpace(binaryPath) == "" || strings.TrimSpace(serviceName) == "" || len(tags) == 0 {
		return nil, fmt.Errorf("direct adapter paths, service, and inbound tags are required")
	}
	mutator := &singboxadapter.DirectInboundMutator{
		StatePath: statePath, Config: singboxadapter.NewManagedConfigStore(configPath),
		BinaryPath: binaryPath, InboundTags: tags,
		Reload: func(ctx context.Context) error { return runSystemctl(ctx, "reload-or-restart", serviceName) },
		Health: func(ctx context.Context) error {
			if err := singboxCheck(ctx, binaryPath, configPath); err != nil {
				return err
			}
			return runSystemctl(ctx, "is-active", "--quiet", serviceName)
		},
	}
	if err := mutator.Ready(context.Background()); err != nil {
		return nil, fmt.Errorf("direct adapter is not ready: %w", err)
	}
	return mutator, nil
}
