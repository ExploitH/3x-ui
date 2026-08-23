package singboxadapter

import (
	"context"
	"encoding/json"
	"fmt"
)

func (h *ReadOnlyHandler) inboundSettings(inbound singboxInbound) (string, error) {
	if h.managedMutator == nil || inbound.Tag != h.managedInboundTag() {
		return sanitizedInboundSettings(inbound), nil
	}
	state, err := h.managedMutator.State.Load(context.Background())
	if err != nil {
		return "", fmt.Errorf("load managed relay state: %w", err)
	}
	clients := make([]map[string]any, 0, len(state.Users))
	for _, user := range state.Users {
		clients = append(clients, map[string]any{
			"email":  user.Email,
			"enable": user.Enabled && !user.Detached,
		})
	}
	encoded, err := json.Marshal(map[string]any{"clients": clients})
	if err != nil {
		return "", fmt.Errorf("encode managed relay inventory: %w", err)
	}
	return string(encoded), nil
}

func (h *ReadOnlyHandler) managedInboundTag() string {
	if h == nil || h.managedMutator == nil || h.managedMutator.State == nil {
		return ""
	}
	state, err := h.managedMutator.State.Load(context.Background())
	if err != nil {
		return ""
	}
	return state.InboundTag
}
