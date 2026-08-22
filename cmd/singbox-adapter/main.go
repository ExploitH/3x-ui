package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/singboxadapter"
)

func main() {
	configPath := envOr("SINGBOX_ADAPTER_CONFIG", "/etc/sing-box/config.json")
	tokenPath := envOr("SINGBOX_ADAPTER_TOKEN_FILE", "/etc/sing-box/adapter.token")
	listen := envOr("SINGBOX_ADAPTER_LISTEN", "127.0.0.1:23854")
	basePath := envOr("SINGBOX_ADAPTER_BASE_PATH", "/adapter/")
	v2rayAPIAddress := strings.TrimSpace(os.Getenv("SINGBOX_ADAPTER_V2RAY_API"))

	token, err := readTokenFile(tokenPath)
	if err != nil {
		log.Fatalf("read adapter token file: %v", err)
	}

	handler, err := singboxadapter.NewReadOnlyHandler(singboxadapter.ReadOnlyOptions{
		ConfigPath:      configPath,
		Token:           token,
		BasePath:        basePath,
		V2RayAPIAddress: v2rayAPIAddress,
	})
	if err != nil {
		log.Fatalf("initialize read-only adapter: %v", err)
	}

	server := &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("sing-box read-only adapter listening on %s", listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("adapter server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("adapter shutdown: %v", err)
	}
	if err := handler.Close(); err != nil {
		log.Printf("close V2Ray API client: %v", err)
	}
}

func readTokenFile(path string) (string, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !linkInfo.Mode().IsRegular() {
		return "", fmt.Errorf("token path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o177 != 0 {
		return "", fmt.Errorf("token file has insecure type or mode %o", info.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	if len(data) > 4096 {
		return "", fmt.Errorf("token file exceeds 4096 bytes")
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file is empty")
	}
	return token, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
