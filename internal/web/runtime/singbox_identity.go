package runtime

import (
	"errors"
	"fmt"
	"strings"
)

type SingboxMappedUserTraffic struct {
	Email    string
	Uplink   int64
	Downlink int64
	Total    int64
}

func MapSingboxTrafficUsers(snapshot *SingboxTrafficSnapshot, canonicalEmails []string) ([]SingboxMappedUserTraffic, error) {
	if snapshot == nil {
		return nil, errors.New("sing-box traffic snapshot is nil")
	}
	if err := validateSingboxTrafficSnapshot(snapshot); err != nil {
		return nil, err
	}
	canonical := make(map[string]string, len(canonicalEmails))
	for _, email := range canonicalEmails {
		normalized := normalizeSingboxIdentity(email)
		if normalized == "" {
			return nil, errors.New("canonical client email is empty")
		}
		if _, exists := canonical[normalized]; exists {
			return nil, fmt.Errorf("duplicate canonical client email %q", normalized)
		}
		canonical[normalized] = strings.TrimSpace(email)
	}

	mapped := make([]SingboxMappedUserTraffic, 0, len(snapshot.Users))
	seen := make(map[string]struct{}, len(snapshot.Users))
	for _, user := range snapshot.Users {
		email := normalizeSingboxIdentity(user.Name)
		if email == "" {
			return nil, errors.New("sing-box user identity is empty")
		}
		if _, exists := canonical[email]; !exists {
			return nil, fmt.Errorf("sing-box user identity %q is not a canonical client", user.Name)
		}
		if _, exists := seen[email]; exists {
			return nil, fmt.Errorf("sing-box user identity collision for %q", email)
		}
		seen[email] = struct{}{}
		mapped = append(mapped, SingboxMappedUserTraffic{
			Email:    canonical[email],
			Uplink:   user.Uplink,
			Downlink: user.Downlink,
			Total:    user.Total,
		})
	}
	return mapped, nil
}

func normalizeSingboxIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
