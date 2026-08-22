package singboxadapter

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type ManagedRelayUser struct {
	Email   string `json:"email"`
	UUID    string `json:"uuid"`
	ExitTag string `json:"exitTag"`
}

type ManagedRelayExit struct {
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"serverPort"`
}

type ManagedRelayInbound struct {
	Type       string             `json:"type"`
	Tag        string             `json:"tag"`
	Listen     string             `json:"listen"`
	ListenPort int                `json:"listen_port"`
	Users      []ManagedRelayUser `json:"users"`
}

type ManagedRelayOutbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	Detour     string `json:"detour,omitempty"`
}

type ManagedRelayRouteRule struct {
	Inbound  []string `json:"inbound"`
	AuthUser string   `json:"auth_user"`
	Outbound string   `json:"outbound"`
}

type RelayFragment struct {
	Inbounds   []ManagedRelayInbound   `json:"inbounds"`
	Outbounds  []ManagedRelayOutbound  `json:"outbounds"`
	RouteRules []ManagedRelayRouteRule `json:"route_rules"`
}

type RelayFragmentInput struct {
	InboundTag string
	ListenPort int
	Users      []ManagedRelayUser
	Exits      []ManagedRelayExit
}

func BuildManagedRelayFragment(input RelayFragmentInput) (RelayFragment, error) {
	if strings.TrimSpace(input.InboundTag) == "" || input.ListenPort <= 0 || input.ListenPort > 65535 {
		return RelayFragment{}, errors.New("managed relay inbound tag and port are required")
	}
	exits := make(map[string]ManagedRelayExit, len(input.Exits))
	for _, exit := range input.Exits {
		if strings.TrimSpace(exit.Tag) == "" || strings.TrimSpace(exit.Server) == "" || exit.ServerPort <= 0 || exit.ServerPort > 65535 {
			return RelayFragment{}, fmt.Errorf("invalid exit %q", exit.Tag)
		}
		if _, exists := exits[exit.Tag]; exists {
			return RelayFragment{}, fmt.Errorf("duplicate exit %q", exit.Tag)
		}
		exits[exit.Tag] = exit
	}
	users := append([]ManagedRelayUser(nil), input.Users...)
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	seenEmail := make(map[string]struct{}, len(users))
	seenUUID := make(map[string]struct{}, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.Email) == "" || strings.TrimSpace(user.UUID) == "" {
			return RelayFragment{}, errors.New("managed relay user email and uuid are required")
		}
		if _, exists := seenEmail[user.Email]; exists {
			return RelayFragment{}, fmt.Errorf("duplicate managed relay user %q", user.Email)
		}
		if _, exists := seenUUID[user.UUID]; exists {
			return RelayFragment{}, fmt.Errorf("duplicate managed relay uuid for %q", user.Email)
		}
		if _, exists := exits[user.ExitTag]; !exists {
			return RelayFragment{}, fmt.Errorf("user %q references unknown exit %q", user.Email, user.ExitTag)
		}
		seenEmail[user.Email] = struct{}{}
		seenUUID[user.UUID] = struct{}{}
	}
	fragment := RelayFragment{
		Inbounds:  []ManagedRelayInbound{{Type: "hysteria2", Tag: input.InboundTag, Listen: "::", ListenPort: input.ListenPort, Users: users}},
		Outbounds: []ManagedRelayOutbound{{Type: "direct", Tag: "direct"}},
	}
	usedExit := make(map[string]struct{})
	for _, user := range users {
		if _, ok := usedExit[user.ExitTag]; ok {
			continue
		}
		exit := exits[user.ExitTag]
		fragment.Outbounds = append(fragment.Outbounds, ManagedRelayOutbound{
			Type: "hysteria2", Tag: "managed-" + exit.Tag, Server: exit.Server, ServerPort: exit.ServerPort,
		})
		usedExit[user.ExitTag] = struct{}{}
	}
	for _, user := range users {
		fragment.RouteRules = append(fragment.RouteRules, ManagedRelayRouteRule{Inbound: []string{input.InboundTag}, AuthUser: user.Email, Outbound: "managed-" + user.ExitTag})
	}
	sort.Slice(fragment.RouteRules, func(i, j int) bool { return fragment.RouteRules[i].AuthUser < fragment.RouteRules[j].AuthUser })
	return fragment, nil
}
