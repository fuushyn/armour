package fixtures

import (
	"net/http"
	"testing"
	"time"
)

func TestFakeSSEServerPrimingEvent(t *testing.T) {
	server := NewFakeSSEServer(t)
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL()+"?stream=test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to fetch SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestStdioStubPassThrough(t *testing.T) {
	testData := `{"jsonrpc":"2.0","method":"initialize","params":{}}`
	stub := NewStdioStub(testData)

	buf := make([]byte, len(testData))
	n, err := stub.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	if n != len(testData) {
		t.Errorf("expected to read %d bytes, got %d", len(testData), n)
	}

	if string(buf) != testData {
		t.Errorf("expected %q, got %q", testData, string(buf))
	}
}

func TestTokenStoreBasic(t *testing.T) {
	ts := NewTokenStore()

	record := TokenRecord{
		Token:     "test-token",
		Aud:       "https://api.example.com",
		Resource:  "https://example.com",
		Scope:     "read write",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	ts.Store("key1", record)

	got, ok := ts.Get("key1")
	if !ok {
		t.Fatalf("token not found")
	}

	if got.Token != record.Token {
		t.Errorf("expected token %s, got %s", record.Token, got.Token)
	}
	if got.Aud != record.Aud {
		t.Errorf("expected aud %s, got %s", record.Aud, got.Aud)
	}
}

func TestInMemorySQLiteInit(t *testing.T) {
	db, err := NewInMemorySQLite()
	if err != nil {
		t.Fatalf("failed to create in-memory SQLite: %v", err)
	}
	defer db.Close()

	if err := InitSchema(db); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'").Scan(&tableName)
	if err != nil {
		t.Fatalf("sessions table not found: %v", err)
	}

	if tableName != "sessions" {
		t.Errorf("expected table 'sessions', got %q", tableName)
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("failed to generate session ID: %v", err)
	}

	id2, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("failed to generate session ID: %v", err)
	}

	if id1 == id2 {
		t.Errorf("expected unique IDs, got same ID twice")
	}

	if len(id1) == 0 {
		t.Errorf("expected non-empty session ID")
	}
}

func TestUnixSocketHelper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping unix socket test in short mode")
	}

	helper := NewUnixSocketHelper(t)
	defer helper.Close()

	path := helper.Path()
	if path == "" {
		t.Errorf("expected non-empty socket path")
	}

	if helper.Listener() == nil {
		t.Errorf("expected non-nil listener")
	}
}
