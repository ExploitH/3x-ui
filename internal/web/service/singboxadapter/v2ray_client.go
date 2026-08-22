package singboxadapter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/singboxadapter/v2rayapi"
)

const v2RayStatsServiceName = "v2ray.core.app.stats.command.StatsService"

var errV2RayAPIAddressRequired = errors.New("sing-box V2Ray API address is required")

// TrafficStat is one counter returned by the sing-box V2Ray stats API.
type TrafficStat struct {
	Name  string
	Value int64
}

type TrafficStatsProvider interface {
	Query(context.Context, []string) ([]TrafficStat, error)
}

type v2RayStatsClient struct {
	conn  grpc.ClientConnInterface
	close func() error
}

func newV2RayStatsClient(conn grpc.ClientConnInterface) *v2RayStatsClient {
	return &v2RayStatsClient{conn: conn}
}

func dialV2RayStatsClient(_ context.Context, address string) (*v2RayStatsClient, error) {
	if err := validateV2RayAPIAddress(address); err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient("passthrough:///"+address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial sing-box V2Ray API: %w", err)
	}
	return &v2RayStatsClient{conn: conn, close: conn.Close}, nil
}

func (c *v2RayStatsClient) Close() error {
	if c == nil || c.close == nil {
		return nil
	}
	return c.close()
}

func (c *v2RayStatsClient) Query(ctx context.Context, patterns []string) ([]TrafficStat, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("sing-box V2Ray API client is not initialized")
	}
	if len(patterns) == 0 {
		return nil, errors.New("sing-box V2Ray API query patterns are required")
	}
	response := new(v2rayapi.QueryStatsResponse)
	request := &v2rayapi.QueryStatsRequest{
		Patterns: append([]string(nil), patterns...),
		Regexp:   true,
		Reset_:   false,
	}
	if err := c.conn.Invoke(ctx,
		"/"+v2RayStatsServiceName+"/QueryStats",
		request,
		response,
	); err != nil {
		return nil, fmt.Errorf("query sing-box traffic stats: %w", err)
	}
	stats := make([]TrafficStat, 0, len(response.Stat))
	for _, stat := range response.Stat {
		if stat == nil || strings.TrimSpace(stat.Name) == "" {
			continue
		}
		stats = append(stats, TrafficStat{Name: stat.Name, Value: stat.Value})
	}
	return stats, nil
}

func validateV2RayAPIAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return errV2RayAPIAddressRequired
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(port) == "" {
		return fmt.Errorf("sing-box V2Ray API address must be host:port")
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("sing-box V2Ray API address must be loopback")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("sing-box V2Ray API address must use a valid TCP port")
	}
	return nil
}
