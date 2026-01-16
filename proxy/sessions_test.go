package proxy

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSessionCreation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	InitSchema(db)

	sm := NewSessionManager(db)
	sessionID, err := sm.CreateSession("server1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if sessionID == "" {
		t.Errorf("expected non-empty session ID")
	}

	if !isValidSessionID(sessionID) {
		t.Errorf("session ID contains invalid characters: %s", sessionID)
	}
}

func TestSessionIDUniqueness(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	InitSchema(db)

	sm := NewSessionManager(db)
	sessionID1, err := sm.CreateSession("server1")
	if err != nil {
		t.Fatalf("failed to create first session: %v", err)
	}

	sessionID2, err := sm.CreateSession("server1")
	if err != nil {
		t.Fatalf("failed to create second session: %v", err)
	}

	if sessionID1 == sessionID2 {
		t.Errorf("expected unique session IDs, got same ID")
	}
}

func TestSessionValidation(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		serverID   string
		shouldFail bool
	}{
		{"valid session", "", "server1", false},
		{"invalid session", "nonexistent", "server1", true},
		{"session wrong server", "", "server2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()
			InitSchema(db)

			sm := NewSessionManager(db)
			sessionID, _ := sm.CreateSession("server1")

			testSessionID := sessionID
			if tt.sessionID != "" {
				testSessionID = tt.sessionID
			}
			testServerID := "server1"
			if tt.serverID != "" {
				testServerID = tt.serverID
			}

			err := sm.ValidateSession(testSessionID, testServerID)
			if (err != nil) != tt.shouldFail {
				t.Errorf("validate failed=%v, expected failed=%v: %v", err != nil, tt.shouldFail, err)
			}
		})
	}
}

func TestSessionPropagation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	InitSchema(db)

	sm := NewSessionManager(db)
	sessionID, err := sm.CreateSession("server1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	session, err := sm.GetSession(sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if session.ID != sessionID {
		t.Errorf("expected session ID %s, got %s", sessionID, session.ID)
	}

	if session.ServerID != "server1" {
		t.Errorf("expected server ID server1, got %s", session.ServerID)
	}
}

func TestSessionRotation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	InitSchema(db)

	sm := NewSessionManager(db)
	oldSessionID, err := sm.CreateSession("server1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	newSessionID, err := sm.RotateSession(oldSessionID, "server1")
	if err != nil {
		t.Fatalf("failed to rotate session: %v", err)
	}

	if newSessionID == oldSessionID {
		t.Errorf("expected different session ID after rotation")
	}

	if err := sm.ValidateSession(newSessionID, "server1"); err != nil {
		t.Errorf("new session should be valid: %v", err)
	}
}

func TestSessionRotationWrongServer(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	InitSchema(db)

	sm := NewSessionManager(db)
	sessionID, err := sm.CreateSession("server1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	_, err = sm.RotateSession(sessionID, "server2")
	if err == nil {
		t.Errorf("expected rotation to fail for wrong server")
	}
}

func TestMissingSessionReturns404(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	InitSchema(db)

	sm := NewSessionManager(db)

	err := sm.ValidateSession("nonexistent", "server1")
	if err == nil {
		t.Errorf("expected validation error for nonexistent session")
	}

	if err.Error() != "session not found" {
		t.Errorf("expected 'session not found' error, got: %v", err)
	}
}

func TestSessionIDCharacters(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		valid     bool
	}{
		{"hex characters", "abc123def456", true},
		{"with hyphen", "abc-123-def", true},
		{"with underscore", "abc_123_def", true},
		{"with dot", "abc.123.def", true},
		{"with space", "abc 123", false},
		{"with special char", "abc@123", false},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := isValidSessionID(tt.sessionID)
			if valid != tt.valid {
				t.Errorf("isValidSessionID(%q) = %v, expected %v", tt.sessionID, valid, tt.valid)
			}
		})
	}
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
