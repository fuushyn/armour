package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardPOST(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.Header.Get(HeaderProtocolVersion) != MCPProtocolVersion {
			t.Errorf("missing protocol version header")
		}

		if r.Header.Get(HeaderSessionID) != "test-session" {
			t.Errorf("missing session ID header")
		}

		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"jsonrpc":"2.0"}` {
			t.Errorf("unexpected body: %s", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":{"ok":true}}`))
	}))
	defer upstreamServer.Close()

	forwarder := NewForwarder()
	reqBody := bytes.NewReader([]byte(`{"jsonrpc":"2.0"}`))

	body, statusCode, err := forwarder.ForwardPOST(upstreamServer.URL, "test-session", reqBody)
	if err != nil {
		t.Fatalf("ForwardPOST failed: %v", err)
	}
	defer body.Close()

	if statusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", statusCode)
	}

	respBody, _ := io.ReadAll(body)
	if string(respBody) != `{"jsonrpc":"2.0","result":{"ok":true}}` {
		t.Errorf("unexpected response: %s", string(respBody))
	}
}

func TestForwardGET(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		if r.Header.Get(HeaderProtocolVersion) != MCPProtocolVersion {
			t.Errorf("missing protocol version header")
		}

		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("missing Accept header")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
	}))
	defer upstreamServer.Close()

	forwarder := NewForwarder()
	resp, err := forwarder.ForwardGET(upstreamServer.URL, "test-session", "")
	if err != nil {
		t.Fatalf("ForwardGET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestForwardGET_WithLastEventID(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Last-Event-ID") != "42" {
			t.Errorf("expected Last-Event-ID 42, got %s", r.Header.Get("Last-Event-ID"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	forwarder := NewForwarder()
	resp, err := forwarder.ForwardGET(upstreamServer.URL, "test-session", "42")
	if err != nil {
		t.Fatalf("ForwardGET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}
