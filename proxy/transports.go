package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// SSE event queue buffer size
	sseEventQueueBuffer = 100
	// HTTP client timeout for SSE connections
	sseHTTPClientTimeout = 30 * time.Second
)

type Transport interface {
	SendMessage(msg []byte) error
	ReceiveMessage() ([]byte, error)
	Close() error
	SupportsServerToClient() bool
}

type SSETransport struct {
	client       *http.Client
	url          string
	sessionID    string
	streamID     string
	lastEventID  int
	eventQueue   chan string
	mu           sync.Mutex
	closed       bool
	primed       bool
	receivedIDs  map[int]bool
	httpResp     *http.Response
	scanner      *bufio.Scanner
	headers      map[string]string // For custom headers (e.g., API keys)
	lastResponse []byte            // For storing POST response
	responseReady bool              // Whether lastResponse is ready to read
	ctx          context.Context   // Context for cancellation and timeouts
	cancel       context.CancelFunc // Cancel function for cleanup
	wg           sync.WaitGroup     // Track readLoop goroutine
}

func NewSSETransport(url string) *SSETransport {
	return NewSSETransportWithContext(context.Background(), url)
}

func NewSSETransportWithContext(ctx context.Context, url string) *SSETransport {
	// Normalize URL: add http:// if scheme is missing
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	// Generate a session ID for the SSE connection
	sessionID := fmt.Sprintf("%x", rand.Uint64())

	// Create a context with timeout for the transport
	transportCtx, cancel := context.WithTimeout(ctx, sseHTTPClientTimeout)

	return &SSETransport{
		client: &http.Client{
			Timeout: sseHTTPClientTimeout,
		},
		url:         url,
		sessionID:   sessionID,
		eventQueue:  make(chan string, sseEventQueueBuffer),
		receivedIDs: make(map[int]bool),
		headers:     make(map[string]string),
		ctx:         transportCtx,
		cancel:      cancel,
	}
}

func (s *SSETransport) SetHeaders(headers map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = headers
}

func (s *SSETransport) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, err := http.NewRequestWithContext(s.ctx, "GET", s.url, nil)
	if err != nil {
		return err
	}

	// Set MCP protocol headers
	req.Header.Set(HeaderProtocolVersion, MCPProtocolVersion)
	req.Header.Set(HeaderSessionID, s.sessionID)
	// Some servers require clients to accept both JSON and SSE content types
	req.Header.Set("Accept", "application/json, text/event-stream, */*")

	if s.lastEventID > 0 {
		req.Header.Set("Last-Event-ID", strconv.Itoa(s.lastEventID))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("SSE server returned status %d", resp.StatusCode)
	}

	s.httpResp = resp
	s.scanner = bufio.NewScanner(resp.Body)

	s.wg.Add(1)
	go s.readLoop()

	return nil
}

func (s *SSETransport) readLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		if !s.scanner.Scan() {
			return
		}

		line := s.scanner.Text()

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ":") {
			if !s.primed && line == ": priming empty event" {
				s.mu.Lock()
				s.primed = true
				s.mu.Unlock()
			}
			continue
		}

		if strings.HasPrefix(line, "id: ") {
			idStr := strings.TrimPrefix(line, "id: ")
			id, err := strconv.Atoi(idStr)
			if err == nil {
				s.mu.Lock()
				s.lastEventID = id
				s.receivedIDs[id] = true
				s.mu.Unlock()
			}
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			select {
			case s.eventQueue <- data:
			case <-s.ctx.Done():
				return
			}
		}

		if strings.HasPrefix(line, "retry: ") {
			retryStr := strings.TrimPrefix(line, "retry: ")
			if _, err := strconv.Atoi(retryStr); err == nil {
			}
		}
	}
}

func (s *SSETransport) SendMessage(msg []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	sessionID := s.sessionID
	headers := s.headers
	s.mu.Unlock()

	// Per MCP spec: POST request to send JSON-RPC messages
	// Server responds with SSE stream containing the response
	req, err := http.NewRequest("POST", s.url, strings.NewReader(string(msg)))
	if err != nil {
		return err
	}

	// Set MCP protocol headers per spec
	req.Header.Set(HeaderProtocolVersion, MCPProtocolVersion)
	if sessionID != "" {
		req.Header.Set(HeaderSessionID, sessionID)
	}
	req.Header.Set("Content-Type", "application/json")
	// MCP Streamable HTTP requires clients to accept both JSON and SSE responses
	req.Header.Set("Accept", "application/json, text/event-stream, */*")

	// Set custom headers (e.g., API keys)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("HTTP error %d: session not found, must re-initialize: %s", resp.StatusCode, string(bodyBytes))
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Store response body for ReceiveMessage() - always set responseReady when we have a response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}
	respBody = normalizeSSEBody(resp.Header.Get("Content-Type"), respBody)

	s.mu.Lock()
	s.lastResponse = respBody
	s.responseReady = true
	s.mu.Unlock()

	return nil
}

func (s *SSETransport) ReceiveMessage() ([]byte, error) {
	// Check if we have a stored POST response
	s.mu.Lock()
	if s.responseReady {
		defer s.mu.Unlock()
		s.responseReady = false
		return s.lastResponse, nil
	}
	s.mu.Unlock()

	// If we have an SSE stream open, read from it
	s.mu.Lock()
	if s.scanner != nil {
		s.mu.Unlock()
		if s.scanner.Scan() {
			return s.scanner.Bytes(), nil
		}
		if err := s.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	s.mu.Unlock()

	return nil, fmt.Errorf("no response available")
}

func (s *SSETransport) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}

	s.closed = true
	close(s.eventQueue)

	if s.httpResp != nil {
		s.httpResp.Body.Close()
	}

	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	// Wait for readLoop goroutine to exit
	s.wg.Wait()

	return nil
}

func (s *SSETransport) SupportsServerToClient() bool {
	return true
}

func (s *SSETransport) HasDuplicates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return false
}

type StdioTransport struct {
	reader  io.Reader
	writer  io.Writer
	scanner *bufio.Scanner
	mu      sync.Mutex
	closed  bool
}

func NewStdioTransport(reader io.Reader, writer io.Writer) *StdioTransport {
	return &StdioTransport{
		reader:  reader,
		writer:  writer,
		scanner: bufio.NewScanner(reader),
	}
}

func (s *StdioTransport) SendMessage(msg []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("transport is closed")
	}

	_, err := s.writer.Write(msg)
	return err
}

func (s *StdioTransport) ReceiveMessage() ([]byte, error) {
	if s.scanner.Scan() {
		return s.scanner.Bytes(), nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, err
	}

	return nil, io.EOF
}

func (s *StdioTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	return nil
}

func (s *StdioTransport) SupportsServerToClient() bool {
	return false
}

type HTTPTransport struct {
	url             string
	sessionID       string // Set by server in initialize response header
	client          *http.Client
	headers         map[string]string
	mu              sync.Mutex
	closed          bool
	lastResponse    []byte
	responseReady   bool
	lastRespHeaders http.Header // Store response headers to extract session ID
}

func NewHTTPTransport(url string) *HTTPTransport {
	// Normalize URL: add http:// if scheme is missing
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	return &HTTPTransport{
		url:       url,
		sessionID: "", // Will be set from server's initialize response header
		client: &http.Client{
			Timeout: sseHTTPClientTimeout,
		},
		headers: make(map[string]string),
	}
}

func (h *HTTPTransport) SetHeaders(headers map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.headers = headers
}

func (h *HTTPTransport) SetSessionID(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionID = sessionID
}

func (h *HTTPTransport) SendMessage(msg []byte) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	headers := h.headers
	sessionID := h.sessionID
	h.mu.Unlock()

	req, err := http.NewRequest("POST", h.url, strings.NewReader(string(msg)))
	if err != nil {
		return err
	}

	// Set MCP protocol headers per spec
	req.Header.Set(HeaderProtocolVersion, MCPProtocolVersion)
	if sessionID != "" {
		req.Header.Set(HeaderSessionID, sessionID)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	// Set custom headers (e.g., API keys)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("HTTP error %d: session not found, must re-initialize: %s", resp.StatusCode, string(bodyBytes))
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read response body - always store it for ReceiveMessage() to return
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}
	respBody = normalizeSSEBody(resp.Header.Get("Content-Type"), respBody)

	// Store response and mark as ready - regardless of content-type
	// The response data will be parsed by the caller based on actual content
	h.mu.Lock()
	h.lastResponse = respBody
	h.responseReady = true
	h.lastRespHeaders = resp.Header

	// Extract session ID from response header if present (per MCP Streamable HTTP spec)
	if newSessionID := resp.Header.Get(HeaderSessionID); newSessionID != "" {
		h.sessionID = newSessionID
	}
	h.mu.Unlock()

	return nil
}

func (h *HTTPTransport) ReceiveMessage() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.responseReady {
		return nil, fmt.Errorf("no response available")
	}

	// Get the response but don't clear the flag - SendMessage() will set it for next request
	resp := h.lastResponse
	h.responseReady = false
	return resp, nil
}

func (h *HTTPTransport) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

func (h *HTTPTransport) SupportsServerToClient() bool {
	return false
}

func normalizeSSEBody(contentType string, body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	lowerType := strings.ToLower(contentType)
	trimmed := bytes.TrimSpace(body)

	if strings.Contains(lowerType, "text/event-stream") ||
		bytes.HasPrefix(trimmed, []byte("event:")) ||
		bytes.HasPrefix(trimmed, []byte("data:")) {
		if parsed := extractSSEPayload(trimmed); len(parsed) > 0 {
			return parsed
		}
	}

	return body
}

func extractSSEPayload(body []byte) []byte {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) > 0 {
				break
			}
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if len(dataLines) == 0 {
		return nil
	}

	return []byte(strings.Join(dataLines, "\n"))
}
