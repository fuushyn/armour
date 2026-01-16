package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdioBlocksServerToClient(t *testing.T) {
	reader := strings.NewReader(`{"jsonrpc":"2.0","method":"initialize"}`)
	writer := &bytes.Buffer{}

	transport := NewStdioTransport(reader, writer)
	defer transport.Close()

	if transport.SupportsServerToClient() {
		t.Errorf("stdio transport should not support server-to-client")
	}
}

func TestStdioRequestResponseOnly(t *testing.T) {
	testData := `{"jsonrpc":"2.0","id":1,"method":"test_method"}`
	reader := strings.NewReader(testData)
	writer := &bytes.Buffer{}

	transport := NewStdioTransport(reader, writer)
	defer transport.Close()

	msg, err := transport.ReceiveMessage()
	if err != nil {
		t.Fatalf("failed to receive message: %v", err)
	}

	if string(msg) != testData {
		t.Errorf("expected message %q, got %q", testData, string(msg))
	}

	responseData := `{"jsonrpc":"2.0","id":1,"result":{}}`
	err = transport.SendMessage([]byte(responseData))
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	if !bytes.Contains(writer.Bytes(), []byte(responseData)) {
		t.Errorf("expected response written to writer")
	}
}

func TestStdioDowngradeCapabilities(t *testing.T) {
	reader := strings.NewReader("")
	writer := &bytes.Buffer{}

	transport := NewStdioTransport(reader, writer)
	defer transport.Close()

	if transport.SupportsServerToClient() {
		t.Errorf("stdio should not support server-to-client capabilities")
	}
}

func TestHTTPSessionIDRequired(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		sessionID := r.Header.Get(HeaderSessionID)

		// First request (initialize) shouldn't require session ID
		if requestCount == 1 {
			w.Header().Set(HeaderSessionID, "test-session-id")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Subsequent requests should have session ID
		if sessionID == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("missing session ID"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL)
	defer transport.Close()

	// First request (initialize) - no session ID needed yet
	err := transport.SendMessage([]byte(`{"jsonrpc":"2.0"}`))
	if err != nil {
		t.Errorf("expected successful first request without session ID: %v", err)
	}

	// After first request, session ID should be set from response header
	if transport.sessionID != "test-session-id" {
		t.Errorf("expected sessionID to be set from response header, got: %s", transport.sessionID)
	}
}

func TestHTTPSessionIDMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL)
	defer transport.Close()

	err := transport.SendMessage([]byte(`{"jsonrpc":"2.0"}`))
	if err == nil {
		t.Errorf("expected error when session ID missing")
	}
}

func TestHTTPSessionNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL)
	defer transport.Close()

	err := transport.SendMessage([]byte(`{"jsonrpc":"2.0"}`))
	if err == nil || !strings.Contains(err.Error(), "must re-initialize") {
		t.Errorf("expected re-initialize error for 404, got: %v", err)
	}
}

func TestHTTPProtocolVersionHeader(t *testing.T) {
	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get(HeaderProtocolVersion)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL)
	defer transport.Close()

	transport.SendMessage([]byte(`{"jsonrpc":"2.0"}`))

	if receivedHeader != MCPProtocolVersion {
		t.Errorf("expected protocol version header %s, got %s", MCPProtocolVersion, receivedHeader)
	}
}

func TestSSESupportsServerToClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, ": priming\nid: 0\n\n")
	}))
	defer server.Close()

	transport := NewSSETransport(server.URL)
	if !transport.SupportsServerToClient() {
		t.Errorf("SSE should support server-to-client")
	}
}

func TestHTTPDoesNotSupportServerToClient(t *testing.T) {
	transport := NewHTTPTransport("http://localhost")
	if transport.SupportsServerToClient() {
		t.Errorf("HTTP should not support server-to-client streaming")
	}
}

func TestSSETransportType(t *testing.T) {
	transport := NewSSETransport("http://example.com")
	if transport == nil {
		t.Errorf("expected non-nil SSE transport")
	}
}

func TestHTTPTransportType(t *testing.T) {
	transport := NewHTTPTransport("http://example.com")
	if transport == nil {
		t.Errorf("expected non-nil HTTP transport")
	}

	// Session ID should be empty initially, set by server in initialize response
	if transport.sessionID != "" {
		t.Errorf("expected empty initial sessionID, got %s", transport.sessionID)
	}
}

func TestStdioTransportType(t *testing.T) {
	reader := strings.NewReader("")
	writer := &bytes.Buffer{}
	transport := NewStdioTransport(reader, writer)
	if transport == nil {
		t.Errorf("expected non-nil Stdio transport")
	}
}
