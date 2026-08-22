package singboxadapter

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/singboxadapter/v2rayapi"
)

func TestV2RayStatsClientQueriesWithoutResetUsingCompatServiceName(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	var got *v2rayapi.QueryStatsRequest
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "v2ray.core.app.stats.command.StatsService",
		HandlerType: (*statsServiceForTest)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "QueryStats",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
				request := new(v2rayapi.QueryStatsRequest)
				if err := dec(request); err != nil {
					return nil, err
				}
				got = request
				return &v2rayapi.QueryStatsResponse{Stat: []*v2rayapi.Stat{
					{Name: "user>>>alice>>>traffic>>>uplink", Value: 123},
					{Name: "user>>>alice>>>traffic>>>downlink", Value: 456},
				}}, nil
			},
		}},
	}, testStatsService{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })

	conn, err := grpc.NewClient("passthrough:///bufconn", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	client := newV2RayStatsClient(conn)
	stats, err := client.Query(context.Background(), []string{`^user>>>.*>>>traffic>>>.*$`})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got == nil || !got.Regexp || got.Reset_ || len(got.Patterns) != 1 {
		t.Fatalf("request=%+v, want regexp=true reset=false one pattern", got)
	}
	if len(stats) != 2 || stats[0].Name != "user>>>alice>>>traffic>>>uplink" || stats[1].Value != 456 {
		t.Fatalf("stats=%+v", stats)
	}
}

type (
	statsServiceForTest interface{}
	testStatsService    struct{}
)

func TestValidateV2RayAPIAddressRequiresLoopbackAndValidPort(t *testing.T) {
	for _, address := range []string{"127.0.0.1:19085", "[::1]:19085"} {
		if err := validateV2RayAPIAddress(address); err != nil {
			t.Fatalf("loopback address %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{
		"0.0.0.0:19085",
		"192.0.2.10:19085",
		"stats.example:19085",
		"19085",
		"127.0.0.1:0",
		"127.0.0.1:65536",
		"127.0.0.1:not-a-port",
	} {
		if err := validateV2RayAPIAddress(address); err == nil {
			t.Fatalf("invalid address %q accepted", address)
		}
	}
	if !errors.Is(validateV2RayAPIAddress(""), errV2RayAPIAddressRequired) {
		t.Fatal("empty address should return errV2RayAPIAddressRequired")
	}
}
