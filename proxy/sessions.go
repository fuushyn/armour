package proxy

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"regexp"
	"sync"
	"time"
)

const (
	// Session ID byte length for cryptographic randomness
	sessionIDByteLength = 24
)

type SessionManager struct {
	db               *sql.DB
	mu               sync.RWMutex
	activeSessions   map[string]SessionRecord
	privilegeChanges map[string]time.Time
}

type SessionRecord struct {
	ID         string
	ServerID   string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

func NewSessionManager(db *sql.DB) *SessionManager {
	return &SessionManager{
		db:               db,
		activeSessions:   make(map[string]SessionRecord),
		privilegeChanges: make(map[string]time.Time),
	}
}

func (sm *SessionManager) CreateSession(serverID string) (string, error) {
	sessionID, err := generateCryptoSessionID()
	if err != nil {
		return "", err
	}

	if !isValidSessionID(sessionID) {
		return "", fmt.Errorf("generated session ID contains invalid characters")
	}

	record := SessionRecord{
		ID:         sessionID,
		ServerID:   serverID,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
	}

	sm.mu.Lock()
	sm.activeSessions[sessionID] = record
	sm.mu.Unlock()

	_, err = sm.db.Exec(`
		INSERT INTO sessions (id, server_id, created_at)
		VALUES (?, ?, ?)
	`, sessionID, serverID, record.CreatedAt)

	if err != nil {
		sm.mu.Lock()
		delete(sm.activeSessions, sessionID)
		sm.mu.Unlock()
		return "", err
	}

	return sessionID, nil
}

func (sm *SessionManager) ValidateSession(sessionID, serverID string) error {
	sm.mu.RLock()
	record, exists := sm.activeSessions[sessionID]
	sm.mu.RUnlock()

	if !exists {
		var dbServerID string
		err := sm.db.QueryRow(`
			SELECT server_id FROM sessions WHERE id = ?
		`, sessionID).Scan(&dbServerID)

		if err == sql.ErrNoRows {
			return fmt.Errorf("session not found")
		}
		if err != nil {
			return err
		}

		record = SessionRecord{ID: sessionID, ServerID: dbServerID}
	}

	if record.ServerID != serverID {
		return fmt.Errorf("session belongs to different server")
	}

	sm.mu.Lock()
	if sess, ok := sm.activeSessions[sessionID]; ok {
		sess.LastUsedAt = time.Now()
		sm.activeSessions[sessionID] = sess
	}
	sm.mu.Unlock()

	return nil
}

func (sm *SessionManager) RotateSession(oldSessionID, serverID string) (string, error) {
	if err := sm.ValidateSession(oldSessionID, serverID); err != nil {
		return "", err
	}

	newSessionID, err := sm.CreateSession(serverID)
	if err != nil {
		return "", err
	}

	sm.mu.Lock()
	sm.privilegeChanges[newSessionID] = time.Now()
	sm.mu.Unlock()

	return newSessionID, nil
}

func (sm *SessionManager) GetSession(sessionID string) (SessionRecord, error) {
	sm.mu.RLock()
	record, exists := sm.activeSessions[sessionID]
	sm.mu.RUnlock()

	if exists {
		return record, nil
	}

	var createdAt time.Time
	var serverID string
	err := sm.db.QueryRow(`
		SELECT id, server_id, created_at FROM sessions WHERE id = ?
	`, sessionID).Scan(&sessionID, &serverID, &createdAt)

	if err == sql.ErrNoRows {
		return SessionRecord{}, fmt.Errorf("session not found")
	}
	if err != nil {
		return SessionRecord{}, err
	}

	return SessionRecord{
		ID:        sessionID,
		ServerID:  serverID,
		CreatedAt: createdAt,
	}, nil
}

func generateCryptoSessionID() (string, error) {
	b := make([]byte, sessionIDByteLength)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	sessionID := ""
	for _, byte := range b {
		sessionID += fmt.Sprintf("%x", byte)
	}
	return sessionID, nil
}

func isValidSessionID(sessionID string) bool {
	validPattern := regexp.MustCompile(`^[a-zA-Z0-9._\-]*$`)
	return validPattern.MatchString(sessionID)
}
