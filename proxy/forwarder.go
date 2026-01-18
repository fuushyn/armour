package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Forwarder struct {
	client *http.Client
}

func NewForwarder() *Forwarder {
	return &Forwarder{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (f *Forwarder) ForwardPOST(upstreamURL, sessionID string, reqBody io.Reader) (io.ReadCloser, int, error) {
	body, err := io.ReadAll(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(HeaderProtocolVersion, MCPProtocolVersion)
	req.Header.Set(HeaderSessionID, sessionID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to forward request: %w", err)
	}

	return resp.Body, resp.StatusCode, nil
}

func (f *Forwarder) ForwardGET(upstreamURL, sessionID string, lastEventID string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, upstreamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(HeaderProtocolVersion, MCPProtocolVersion)
	req.Header.Set(HeaderSessionID, sessionID)
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to forward request: %w", err)
	}

	return resp, nil
}

func (f *Forwarder) PipeSSE(w http.ResponseWriter, resp *http.Response) error {
	if resp.Header.Get("Content-Type") != "text/event-stream" && !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return fmt.Errorf("upstream response is not SSE")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support flushing")
	}

	defer resp.Body.Close()

	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("failed to copy response: %w", err)
	}

	flusher.Flush()
	return nil
}
