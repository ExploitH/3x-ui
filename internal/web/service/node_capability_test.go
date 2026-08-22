package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

type capabilityReaderStub struct {
	caps *runtime.NodeCapabilities
	err  error
}

func (s capabilityReaderStub) FetchCapabilities(context.Context) (*runtime.NodeCapabilities, error) {
	return s.caps, s.err
}

func TestValidateNodeEnableCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stub    capabilityReaderStub
		wantErr bool
	}{
		{
			name:    "readonly adapter rejected",
			stub:    capabilityReaderStub{caps: &runtime.NodeCapabilities{Mode: "readonly", PerClientTraffic: false}},
			wantErr: true,
		},
		{
			name:    "managed traffic node allowed",
			stub:    capabilityReaderStub{caps: &runtime.NodeCapabilities{Mode: "managed", PerClientTraffic: true}},
			wantErr: false,
		},
		{
			name:    "legacy node remains compatible",
			stub:    capabilityReaderStub{err: runtime.ErrCapabilitiesUnsupported},
			wantErr: false,
		},
		{
			name:    "unexpected capability error fails closed",
			stub:    capabilityReaderStub{err: errors.New("connection refused")},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateNodeEnableCapabilities(context.Background(), tc.stub); (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
