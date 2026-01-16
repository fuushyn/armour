package fixtures

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type FakeSSEServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	eventID  int
	events   map[string][]string
	lastSeen map[string]int
	retries  map[string]int
}

func NewFakeSSEServer(t *testing.T) *FakeSSEServer {
	f := &FakeSSEServer{
		events:   make(map[string][]string),
		lastSeen: make(map[string]int),
		retries:  make(map[string]int),
		eventID:  0,
	}

	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Accept"), "text/event-stream") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		streamID := r.Header.Get("X-Stream-ID")
		if streamID == "" {
			streamID = fmt.Sprintf("stream-%d", time.Now().UnixNano())
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		lastEventID := r.Header.Get("Last-Event-ID")
		startFrom := 0
		if lastEventID != "" {
			fmt.Sscanf(lastEventID, "%d", &startFrom)
		}

		f.mu.Lock()
		retryMs := f.retries[streamID]
		if retryMs > 0 {
			fmt.Fprintf(w, "retry: %d\n\n", retryMs)
			flusher.Flush()
		}
		f.lastSeen[streamID] = startFrom
		f.mu.Unlock()

		fmt.Fprintf(w, ": priming empty event\nid: 0\n\n")
		flusher.Flush()

		f.mu.Lock()
		events := f.events[streamID]
		f.mu.Unlock()

		for i := startFrom; i < len(events); i++ {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", i+1, events[i])
			flusher.Flush()
		}
	}))

	return f
}

func (f *FakeSSEServer) AddEvent(streamID, data string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events[streamID] == nil {
		f.events[streamID] = []string{}
	}
	f.events[streamID] = append(f.events[streamID], data)
}

func (f *FakeSSEServer) SetRetry(streamID string, ms int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries[streamID] = ms
}

func (f *FakeSSEServer) URL() string {
	return f.server.URL
}

func (f *FakeSSEServer) Close() {
	f.server.Close()
}

type StdioStub struct {
	reqReader  io.Reader
	respWriter io.Writer
}

func NewStdioStub(reqData string) *StdioStub {
	return &StdioStub{
		reqReader:  strings.NewReader(reqData),
		respWriter: io.Discard,
	}
}

func (s *StdioStub) Read(p []byte) (int, error) {
	return s.reqReader.Read(p)
}

func (s *StdioStub) Write(p []byte) (int, error) {
	return s.respWriter.Write(p)
}

func (s *StdioStub) Close() error {
	return nil
}

type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]TokenRecord
}

type TokenRecord struct {
	Token     string
	Aud       string
	Resource  string
	Scope     string
	ExpiresAt time.Time
}

func NewTokenStore() *TokenStore {
	return &TokenStore{
		tokens: make(map[string]TokenRecord),
	}
}

func (ts *TokenStore) Store(key string, record TokenRecord) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tokens[key] = record
}

func (ts *TokenStore) Get(key string) (TokenRecord, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	rec, ok := ts.tokens[key]
	return rec, ok
}

func (ts *TokenStore) Delete(key string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.tokens, key)
}

func NewInMemorySQLite() (*sql.DB, error) {
	return sql.Open("sqlite", "file:memdb?mode=memory&cache=shared")
}

func InitSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS capabilities (
		server_id TEXT NOT NULL,
		capability TEXT NOT NULL,
		PRIMARY KEY (server_id, capability)
	);
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT,
		agent_id TEXT,
		server_id TEXT,
		method TEXT,
		capability TEXT,
		session_id TEXT,
		transport TEXT,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(schema)
	return err
}

func GenerateSessionID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

type UnixSocketHelper struct {
	listener net.Listener
	path     string
}

func NewUnixSocketHelper(t *testing.T) *UnixSocketHelper {
	path := fmt.Sprintf("/tmp/mcp-test-%d.sock", time.Now().UnixNano())
	os.Remove(path)

	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("failed to create unix socket: %v", err)
	}

	return &UnixSocketHelper{
		listener: listener,
		path:     path,
	}
}

func (u *UnixSocketHelper) Path() string {
	return u.path
}

func (u *UnixSocketHelper) Listener() net.Listener {
	return u.listener
}

func (u *UnixSocketHelper) Close() error {
	u.listener.Close()
	os.Remove(u.path)
	return nil
}
