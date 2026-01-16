package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/mcp-go-proxy/proxy"
	_ "modernc.org/sqlite"
)

func TestHealthCheckEndpoint(t *testing.T) {
	config, upstream := makeTestConfig(t, getAvailableAddr(t))
	defer upstream.Close()

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go server.ListenAndServe(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://" + config.ListenAddr + "/healthz")
	if err != nil {
		t.Fatalf("failed to request health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", result["status"])
	}
}

func TestMCPEndpointMissingSessionID(t *testing.T) {
	config, upstream := makeTestConfig(t, getAvailableAddr(t))
	defer upstream.Close()

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go server.ListenAndServe(ctx)
	time.Sleep(100 * time.Millisecond)

	req, err := http.NewRequest(http.MethodPost, "http://"+config.ListenAddr+"/mcp", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestMCPEndpointWithSessionID(t *testing.T) {
	config, upstream := makeTestConfig(t, getAvailableAddr(t))
	defer upstream.Close()

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go server.ListenAndServe(ctx)
	time.Sleep(100 * time.Millisecond)

	req, err := http.NewRequest(http.MethodPost, "http://"+config.ListenAddr+"/mcp", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set(proxy.HeaderSessionID, "test-session-123")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get(proxy.HeaderProtocolVersion) != proxy.MCPProtocolVersion {
		t.Errorf("expected protocol version header %s, got %s",
			proxy.MCPProtocolVersion, resp.Header.Get(proxy.HeaderProtocolVersion))
	}

	if resp.Header.Get(proxy.HeaderSessionID) != "test-session-123" {
		t.Errorf("expected session ID test-session-123, got %s", resp.Header.Get(proxy.HeaderSessionID))
	}
}

func TestOriginValidation(t *testing.T) {
	config, upstream := makeTestConfig(t, getAvailableAddr(t))
	defer upstream.Close()
	config.AllowedOrigins = []string{"https://example.com"}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go server.ListenAndServe(ctx)
	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		name       string
		origin     string
		expectCode int
	}{
		{"allowed origin", "https://example.com", http.StatusOK},
		{"disallowed origin", "https://evil.com", http.StatusForbidden},
		{"no origin", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "http://"+config.ListenAddr+"/mcp", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			req.Header.Set(proxy.HeaderSessionID, "test-session")
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("failed to request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectCode {
				t.Errorf("expected status %d, got %d", tt.expectCode, resp.StatusCode)
			}
		})
	}
}

func TestMethodValidation(t *testing.T) {
	config, upstream := makeTestConfig(t, getAvailableAddr(t))
	defer upstream.Close()

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go server.ListenAndServe(ctx)
	time.Sleep(100 * time.Millisecond)

	req, err := http.NewRequest(http.MethodDelete, "http://"+config.ListenAddr+"/mcp", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServerShutdown(t *testing.T) {
	config, upstream := makeTestConfig(t, getAvailableAddr(t))
	defer upstream.Close()

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.ListenAndServe(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-time.After(2 * time.Second):
		t.Errorf("server did not shut down in time")
	case err := <-errChan:
		if err != nil {
			t.Logf("shutdown error: %v", err)
		}
	}
}

func getAvailableAddr(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get available port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func makeTestConfig(t *testing.T, addr string) (Config, *httptest.Server) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":{"ok":true}}`))
	}))

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")

	configJSON := fmt.Sprintf(`{
  "servers": [
    {"name": "test", "transport": "http", "url": "%s"}
  ]
}`, upstream.URL)

	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	return Config{
		ListenAddr: addr,
		ConfigPath: configPath,
		Mode:       "http",
	}, upstream
}
