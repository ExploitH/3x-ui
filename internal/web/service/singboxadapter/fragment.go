package singboxadapter

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ManagedRelayUser is the canonical control-plane identity. It is never
// serialized directly as a sing-box inbound user: ExitTag is routing metadata,
// while Password is the Hysteria2 authentication password.
type ManagedRelayUser struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	ExitTag  string `json:"exitTag"`
}

type ManagedRelayExit struct {
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"serverPort"`
	Password   string `json:"password"`
	ServerName string `json:"serverName,omitempty"`
}

type ManagedRelayInboundUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type ManagedRelayInboundTLS struct {
	Enabled         bool   `json:"enabled"`
	CertificatePath string `json:"certificate_path"`
	KeyPath         string `json:"key_path"`
}

type ManagedRelayInbound struct {
	Type       string                    `json:"type"`
	Tag        string                    `json:"tag"`
	Listen     string                    `json:"listen"`
	ListenPort int                       `json:"listen_port"`
	TLS        ManagedRelayInboundTLS    `json:"tls"`
	Users      []ManagedRelayInboundUser `json:"users"`
}

type ManagedRelayOutboundTLS struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name,omitempty"`
}

type ManagedRelayOutbound struct {
	Type       string                   `json:"type"`
	Tag        string                   `json:"tag"`
	Server     string                   `json:"server,omitempty"`
	ServerPort int                      `json:"server_port,omitempty"`
	Password   string                   `json:"password,omitempty"`
	TLS        *ManagedRelayOutboundTLS `json:"tls,omitempty"`
	Detour     string                   `json:"detour,omitempty"`
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
	InboundTag      string
	ListenPort      int
	CertificatePath string
	KeyPath         string
	Users           []ManagedRelayUser
	Exits           []ManagedRelayExit
}

func BuildManagedRelayFragment(input RelayFragmentInput) (RelayFragment, error) {
	if strings.TrimSpace(input.InboundTag) == "" || input.ListenPort <= 0 || input.ListenPort > 65535 {
		return RelayFragment{}, errors.New("managed relay inbound tag and port are required")
	}
	certificatePath := strings.TrimSpace(input.CertificatePath)
	keyPath := strings.TrimSpace(input.KeyPath)
	if certificatePath == "" || keyPath == "" {
		return RelayFragment{}, errors.New("managed relay TLS certificate and key paths are required")
	}
	exits := make(map[string]ManagedRelayExit, len(input.Exits))
	for _, rawExit := range input.Exits {
		exit := rawExit
		exit.Tag = strings.TrimSpace(exit.Tag)
		exit.Server = strings.TrimSpace(exit.Server)
		exit.Password = strings.TrimSpace(exit.Password)
		exit.ServerName = strings.TrimSpace(exit.ServerName)
		if exit.ServerName == "" {
			exit.ServerName = exit.Server
		}
		if exit.Tag == "" || exit.Server == "" || exit.Password == "" || exit.ServerPort <= 0 || exit.ServerPort > 65535 || exit.ServerName == "" {
			return RelayFragment{}, fmt.Errorf("invalid exit %q", rawExit.Tag)
		}
		if _, exists := exits[exit.Tag]; exists {
			return RelayFragment{}, fmt.Errorf("duplicate exit %q", exit.Tag)
		}
		exits[exit.Tag] = exit
	}
	users := make([]ManagedRelayUser, 0, len(input.Users))
	seenEmail := make(map[string]struct{}, len(input.Users))
	seenPassword := make(map[string]struct{}, len(input.Users))
	for _, rawUser := range input.Users {
		user := rawUser
		user.Email = canonicalRelayEmail(user.Email)
		user.Password = strings.TrimSpace(user.Password)
		user.ExitTag = strings.TrimSpace(user.ExitTag)
		if user.Email == "" || user.Password == "" {
			return RelayFragment{}, errors.New("managed relay user email and password are required")
		}
		if _, exists := seenEmail[user.Email]; exists {
			return RelayFragment{}, fmt.Errorf("duplicate managed relay user %q", user.Email)
		}
		if _, exists := seenPassword[user.Password]; exists {
			return RelayFragment{}, fmt.Errorf("duplicate managed relay password for %q", user.Email)
		}
		if _, exists := exits[user.ExitTag]; !exists {
			return RelayFragment{}, fmt.Errorf("user %q references unknown exit %q", user.Email, user.ExitTag)
		}
		seenEmail[user.Email] = struct{}{}
		seenPassword[user.Password] = struct{}{}
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	fragment := RelayFragment{
		Inbounds: []ManagedRelayInbound{{
			Type: "hysteria2", Tag: input.InboundTag, Listen: "::", ListenPort: input.ListenPort,
			TLS: ManagedRelayInboundTLS{Enabled: true, CertificatePath: certificatePath, KeyPath: keyPath},
		}},
		Outbounds: []ManagedRelayOutbound{{Type: "direct", Tag: "direct"}},
	}
	usedExit := make(map[string]struct{})
	for _, user := range users {
		fragment.Inbounds[0].Users = append(fragment.Inbounds[0].Users, ManagedRelayInboundUser{Name: user.Email, Password: user.Password})
		if _, ok := usedExit[user.ExitTag]; ok {
			continue
		}
		exit := exits[user.ExitTag]
		fragment.Outbounds = append(fragment.Outbounds, ManagedRelayOutbound{
			Type: "hysteria2", Tag: "managed-" + exit.Tag, Server: exit.Server, ServerPort: exit.ServerPort,
			Password: exit.Password, TLS: &ManagedRelayOutboundTLS{Enabled: true, ServerName: exit.ServerName},
		})
		usedExit[user.ExitTag] = struct{}{}
	}
	for _, user := range users {
		fragment.RouteRules = append(fragment.RouteRules, ManagedRelayRouteRule{Inbound: []string{input.InboundTag}, AuthUser: user.Email, Outbound: "managed-" + user.ExitTag})
	}
	return fragment, nil
}
