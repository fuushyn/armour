package proxy

import (
	"fmt"
	"sync"
)

type ElicitationManager struct {
	mu           sync.RWMutex
	policies     map[string]bool
	blockedTypes map[string]bool
	logPayloads  bool
}

type ElicitationRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id,omitempty"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
}

type ElicitationApproval struct {
	Approved bool
	Message  string
}

func NewElicitationManager() *ElicitationManager {
	return &ElicitationManager{
		policies:     make(map[string]bool),
		blockedTypes: make(map[string]bool),
		logPayloads:  true,
	}
}

func (em *ElicitationManager) AllowElicitationType(elicitationType string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.policies[elicitationType] = true
}

func (em *ElicitationManager) BlockElicitationType(elicitationType string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.policies, elicitationType)
	em.blockedTypes[elicitationType] = true
}

func (em *ElicitationManager) IsElicitationAllowed(elicitationType string) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.policies[elicitationType]
}

func (em *ElicitationManager) IsElicitationBlocked(elicitationType string) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.blockedTypes[elicitationType]
}

func (em *ElicitationManager) EnablePayloadLogging(enable bool) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.logPayloads = enable
}

func (em *ElicitationManager) ValidateElicitationRequest(req ElicitationRequest, proxy *Proxy, transport string) error {
	method := req.Method

	if !proxy.CanElicit() {
		return fmt.Errorf("elicitation capability not available")
	}

	if transport == "stdio" {
		return fmt.Errorf("elicitation not supported on stdio transport")
	}

	if !em.IsElicitationAllowed(method) {
		if em.IsElicitationBlocked(method) {
			return fmt.Errorf("elicitation type %s is blocked", method)
		}
		return fmt.Errorf("elicitation type %s not allowed", method)
	}

	return nil
}

type AuditLog struct {
	mu   sync.RWMutex
	logs []AuditEntry
}

type AuditEntry struct {
	UserID     string
	AgentID    string
	ServerID   string
	Method     string
	Capability string
	SessionID  string
	Transport  string
	Timestamp  int64
	Details    map[string]string
}

func NewAuditLog() *AuditLog {
	return &AuditLog{
		logs: make([]AuditEntry, 0),
	}
}

func (al *AuditLog) Log(entry AuditEntry) {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.logs = append(al.logs, entry)
}

func (al *AuditLog) GetLogs() []AuditEntry {
	al.mu.RLock()
	defer al.mu.RUnlock()

	result := make([]AuditEntry, len(al.logs))
	copy(result, al.logs)
	return result
}

func (al *AuditLog) GetLogsByUserID(userID string) []AuditEntry {
	al.mu.RLock()
	defer al.mu.RUnlock()

	var result []AuditEntry
	for _, entry := range al.logs {
		if entry.UserID == userID {
			result = append(result, entry)
		}
	}
	return result
}

func (al *AuditLog) GetLogsByServerID(serverID string) []AuditEntry {
	al.mu.RLock()
	defer al.mu.RUnlock()

	var result []AuditEntry
	for _, entry := range al.logs {
		if entry.ServerID == serverID {
			result = append(result, entry)
		}
	}
	return result
}

type SecurityManager struct {
	mu             sync.RWMutex
	allowedOrigins map[string]bool
	localHostOnly  bool
}

func NewSecurityManager() *SecurityManager {
	return &SecurityManager{
		allowedOrigins: make(map[string]bool),
		localHostOnly:  false,
	}
}

func (sm *SecurityManager) AddAllowedOrigin(origin string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.allowedOrigins[origin] = true
}

func (sm *SecurityManager) RemoveAllowedOrigin(origin string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.allowedOrigins, origin)
}

func (sm *SecurityManager) IsOriginAllowed(origin string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.allowedOrigins[origin]
}

func (sm *SecurityManager) SetLocalHostOnly(localOnly bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.localHostOnly = localOnly
}

func (sm *SecurityManager) IsLocalHostOnly() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.localHostOnly
}

func (sm *SecurityManager) ValidateOrigin(origin string) error {
	if !sm.IsOriginAllowed(origin) {
		return fmt.Errorf("origin %s is not allowed", origin)
	}
	return nil
}
