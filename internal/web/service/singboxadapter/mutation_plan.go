package singboxadapter

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// PlanManagedRelayUserState returns a managed-tag-only fragment mutation plan.
// It never writes a config or restarts sing-box. Enabling requires the canonical
// user credential/exit spec because a disabled fragment no longer contains it.
func PlanManagedRelayUserState(fragment RelayFragment, user ManagedRelayUser, enable bool) (RelayFragment, error) {
	key := strings.ToLower(strings.TrimSpace(user.Email))
	if key == "" {
		return RelayFragment{}, errors.New("managed relay user email is required")
	}
	planned := cloneRelayFragment(fragment)
	found := false
	managedInbound := -1
	for i := range planned.Inbounds {
		if strings.HasPrefix(planned.Inbounds[i].Tag, "managed-relay") && managedInbound < 0 {
			managedInbound = i
		}
		kept := make([]ManagedRelayInboundUser, 0, len(planned.Inbounds[i].Users))
		for _, existing := range planned.Inbounds[i].Users {
			if canonicalRelayEmail(existing.Name) == key {
				found = true
				continue
			}
			kept = append(kept, existing)
		}
		planned.Inbounds[i].Users = kept
	}

	if !enable {
		if !found {
			return RelayFragment{}, fmt.Errorf("managed relay user %q not found", user.Email)
		}
		planned.RouteRules = removeManagedRelayUserRules(planned.RouteRules, key)
		return planned, nil
	}

	if strings.TrimSpace(user.Password) == "" || strings.TrimSpace(user.ExitTag) == "" {
		return RelayFragment{}, errors.New("managed relay enable requires user password and exit tag")
	}
	if managedInbound < 0 {
		return RelayFragment{}, errors.New("managed relay inbound not found")
	}
	outboundTag := "managed-" + user.ExitTag
	outboundFound := false
	for _, outbound := range planned.Outbounds {
		if outbound.Tag == outboundTag {
			outboundFound = true
			break
		}
	}
	if !outboundFound {
		return RelayFragment{}, fmt.Errorf("managed relay exit %q not found", user.ExitTag)
	}
	planned.Inbounds[managedInbound].Users = append(planned.Inbounds[managedInbound].Users, ManagedRelayInboundUser{Name: user.Email, Password: user.Password})
	sort.Slice(planned.Inbounds[managedInbound].Users, func(i, j int) bool {
		return planned.Inbounds[managedInbound].Users[i].Name < planned.Inbounds[managedInbound].Users[j].Name
	})
	planned.RouteRules = removeManagedRelayUserRules(planned.RouteRules, key)
	planned.RouteRules = append(planned.RouteRules, ManagedRelayRouteRule{
		Inbound: []string{planned.Inbounds[managedInbound].Tag}, AuthUser: user.Email, Outbound: outboundTag,
	})
	sort.Slice(planned.RouteRules, func(i, j int) bool { return planned.RouteRules[i].AuthUser < planned.RouteRules[j].AuthUser })
	return planned, nil
}

func cloneRelayFragment(fragment RelayFragment) RelayFragment {
	clone := RelayFragment{
		Outbounds:  append([]ManagedRelayOutbound(nil), fragment.Outbounds...),
		RouteRules: append([]ManagedRelayRouteRule(nil), fragment.RouteRules...),
	}
	clone.Inbounds = make([]ManagedRelayInbound, len(fragment.Inbounds))
	for i, inbound := range fragment.Inbounds {
		clone.Inbounds[i] = inbound
		clone.Inbounds[i].Users = append([]ManagedRelayInboundUser(nil), inbound.Users...)
	}
	for i := range clone.RouteRules {
		clone.RouteRules[i].Inbound = append([]string(nil), clone.RouteRules[i].Inbound...)
	}
	return clone
}

func removeManagedRelayUserRules(rules []ManagedRelayRouteRule, email string) []ManagedRelayRouteRule {
	kept := make([]ManagedRelayRouteRule, 0, len(rules))
	for _, rule := range rules {
		if strings.ToLower(strings.TrimSpace(rule.AuthUser)) == email {
			continue
		}
		kept = append(kept, rule)
	}
	return kept
}
