package singboxadapter

import (
	"encoding/json"
	"errors"
	"strings"
)

// MergeManagedRelayFragment performs an additive, managed-tag-only merge. It
// preserves legacy inbounds/outbounds/routes and removes only previous
// managed-* entries, so the caller can atomically write the resulting fragment
// into a dedicated config file without rebuilding the existing topology.
func MergeManagedRelayFragment(legacy map[string]any, fragment RelayFragment) (map[string]any, error) {
	if legacy == nil {
		legacy = map[string]any{}
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]any)
	if err := json.Unmarshal(encoded, &merged); err != nil {
		return nil, err
	}

	legacyInbounds, err := objectList(merged, "inbounds")
	if err != nil {
		return nil, err
	}
	keptInbounds := make([]any, 0, len(legacyInbounds)+len(fragment.Inbounds))
	for _, raw := range legacyInbounds {
		row := raw.(map[string]any)
		if isManagedTag(row["tag"]) {
			continue
		}
		keptInbounds = append(keptInbounds, raw)
	}
	for _, inbound := range fragment.Inbounds {
		value, err := jsonObject(inbound)
		if err != nil {
			return nil, err
		}
		keptInbounds = append(keptInbounds, value)
	}
	merged["inbounds"] = keptInbounds

	legacyOutbounds, err := objectList(merged, "outbounds")
	if err != nil {
		return nil, err
	}
	keptOutbounds := make([]any, 0, len(legacyOutbounds)+len(fragment.Outbounds))
	hasDirect := false
	for _, raw := range legacyOutbounds {
		row := raw.(map[string]any)
		if isManagedTag(row["tag"]) {
			continue
		}
		if row["tag"] == "direct" {
			hasDirect = true
		}
		keptOutbounds = append(keptOutbounds, raw)
	}
	for _, outbound := range fragment.Outbounds {
		if outbound.Tag == "direct" && hasDirect {
			continue
		}
		value, err := jsonObject(outbound)
		if err != nil {
			return nil, err
		}
		keptOutbounds = append(keptOutbounds, value)
	}
	merged["outbounds"] = keptOutbounds

	route, ok := merged["route"].(map[string]any)
	if !ok || route == nil {
		route = map[string]any{}
	}
	rules, err := objectListFrom(route, "rules")
	if err != nil {
		return nil, err
	}
	keptRules := make([]any, 0, len(rules)+len(fragment.RouteRules))
	for _, raw := range rules {
		row := raw.(map[string]any)
		if outbound, ok := row["outbound"].(string); ok && strings.HasPrefix(outbound, "managed-") {
			continue
		}
		keptRules = append(keptRules, raw)
	}
	managedRules := make([]any, 0, len(fragment.RouteRules))
	for _, rule := range fragment.RouteRules {
		value, err := jsonObject(rule)
		if err != nil {
			return nil, err
		}
		managedRules = append(managedRules, value)
	}
	keptRules = append(managedRules, keptRules...)
	route["rules"] = keptRules
	merged["route"] = route
	return merged, nil
}

func jsonObject(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func isManagedTag(value any) bool {
	tag, ok := value.(string)
	return ok && (strings.HasPrefix(tag, "managed-") || tag == "managed-relay")
}

func objectList(root map[string]any, key string) ([]any, error) {
	return objectListFrom(root, key)
}

func objectListFrom(root map[string]any, key string) ([]any, error) {
	value, exists := root[key]
	if !exists || value == nil {
		return []any{}, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New(key + " must be an array")
	}
	for _, raw := range list {
		if _, ok := raw.(map[string]any); !ok {
			return nil, errors.New(key + " entries must be objects")
		}
	}
	return list, nil
}
