package proxy

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

type OAuth struct {
	mu               sync.RWMutex
	tokenStore       map[string]TokenData
	stepUpRetries    map[string]int
	maxStepUpRetries int
}

type TokenData struct {
	AccessToken  string
	Audience     string
	Resource     string
	Scope        string
	ExpiresAt    time.Time
	ServerID     string
	RefreshToken string
}

type TokenRequest struct {
	Audience string
	Resource string
	Scope    string
	ServerID string
}

type StepUpResponse struct {
	RequiresConsent bool
	Challenge       string
	Granted         bool
}

func NewOAuth() *OAuth {
	return &OAuth{
		tokenStore:       make(map[string]TokenData),
		stepUpRetries:    make(map[string]int),
		maxStepUpRetries: 3,
	}
}

func (o *OAuth) StoreToken(serverID, accessToken, audience, resource, scope string, expiresIn int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if serverID == "" || audience == "" {
		return fmt.Errorf("serverID and audience are required")
	}

	key := o.generateTokenKey(serverID, audience, resource)

	o.tokenStore[key] = TokenData{
		AccessToken: accessToken,
		Audience:    audience,
		Resource:    resource,
		Scope:       scope,
		ExpiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
		ServerID:    serverID,
	}

	return nil
}

func (o *OAuth) GetToken(serverID, audience, resource string) (TokenData, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	key := o.generateTokenKey(serverID, audience, resource)

	token, exists := o.tokenStore[key]
	if !exists {
		return TokenData{}, fmt.Errorf("token not found")
	}

	if time.Now().After(token.ExpiresAt) {
		return TokenData{}, fmt.Errorf("token expired")
	}

	return token, nil
}

func (o *OAuth) ValidateAudience(serverID, audience string) error {
	if serverID == "" || audience == "" {
		return fmt.Errorf("serverID and audience cannot be empty")
	}

	o.mu.RLock()
	foundAudience := false
	for _, token := range o.tokenStore {
		if token.ServerID == serverID && token.Audience == audience {
			foundAudience = true
			break
		}
	}
	o.mu.RUnlock()

	if !foundAudience {
		return fmt.Errorf("audience %s not valid for server %s", audience, serverID)
	}

	return nil
}

func (o *OAuth) ValidateResource(serverID, resource, audience string) error {
	if serverID == "" || resource == "" {
		return fmt.Errorf("serverID and resource cannot be empty")
	}

	token, err := o.GetToken(serverID, audience, resource)
	if err != nil {
		return fmt.Errorf("resource %s not authorized for server %s: %v", resource, serverID, err)
	}

	if token.ServerID != serverID {
		return fmt.Errorf("resource mismatch: token belongs to different server")
	}

	return nil
}

func (o *OAuth) CheckTokenReuse(serverID1, serverID2, audience string) error {
	if serverID1 == serverID2 {
		return nil
	}

	o.mu.RLock()
	defer o.mu.RUnlock()

	var token1, token2 TokenData
	exists1, exists2 := false, false

	for _, token := range o.tokenStore {
		if token.ServerID == serverID1 && token.Audience == audience {
			token1 = token
			exists1 = true
		}
		if token.ServerID == serverID2 && token.Audience == audience {
			token2 = token
			exists2 = true
		}
	}

	if exists1 && exists2 && token1.AccessToken == token2.AccessToken {
		return fmt.Errorf("token reuse across servers not allowed")
	}

	return nil
}

func (o *OAuth) HandleStepUp(serverID, requiredScope string) (StepUpResponse, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	key := serverID + ":stepup:" + requiredScope

	retries, exists := o.stepUpRetries[key]
	if !exists {
		retries = 0
	}

	if retries >= o.maxStepUpRetries {
		return StepUpResponse{}, fmt.Errorf("step-up retries exceeded")
	}

	o.stepUpRetries[key] = retries + 1

	challenge, err := generateChallenge()
	if err != nil {
		return StepUpResponse{}, err
	}

	return StepUpResponse{
		RequiresConsent: true,
		Challenge:       challenge,
		Granted:         false,
	}, nil
}

func (o *OAuth) GrantStepUp(serverID, requiredScope, newAccessToken string, expiresIn int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	key := serverID + ":stepup:" + requiredScope

	retries, exists := o.stepUpRetries[key]
	if !exists || retries == 0 {
		return fmt.Errorf("no pending step-up challenge")
	}

	if retries > o.maxStepUpRetries {
		return fmt.Errorf("step-up retries exceeded")
	}

	delete(o.stepUpRetries, key)

	tokenKey := o.generateTokenKey(serverID, "", requiredScope)
	o.tokenStore[tokenKey] = TokenData{
		AccessToken: newAccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
		ServerID:    serverID,
		Scope:       requiredScope,
		Audience:    "",
	}

	return nil
}

func (o *OAuth) DenyStepUp(serverID, requiredScope string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	key := serverID + ":stepup:" + requiredScope

	retries, exists := o.stepUpRetries[key]
	if !exists || retries == 0 {
		return fmt.Errorf("no pending step-up challenge")
	}

	delete(o.stepUpRetries, key)
	return nil
}

func (o *OAuth) RevokeToken(serverID, audience, resource string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	key := o.generateTokenKey(serverID, audience, resource)
	delete(o.tokenStore, key)

	return nil
}

func (o *OAuth) GetTokensByServer(serverID string) []TokenData {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var tokens []TokenData
	for _, token := range o.tokenStore {
		if token.ServerID == serverID {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func (o *OAuth) generateTokenKey(serverID, audience, resource string) string {
	if resource == "" {
		return fmt.Sprintf("%s:%s", serverID, audience)
	}
	return fmt.Sprintf("%s:%s:%s", serverID, audience, resource)
}

func generateChallenge() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

type PKCEState struct {
	CodeChallenge string
	CodeVerifier  string
	State         string
}

func GeneratePKCEChallenge() (PKCEState, error) {
	verifier, err := generateVerifier()
	if err != nil {
		return PKCEState{}, err
	}

	state, err := generateChallenge()
	if err != nil {
		return PKCEState{}, err
	}

	challenge := base64URLEncode(sha256Hash(verifier))

	return PKCEState{
		CodeChallenge: challenge,
		CodeVerifier:  verifier,
		State:         state,
	}, nil
}

func generateVerifier() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func base64URLEncode(data []byte) string {
	return base64.URLEncoding.EncodeToString(data)
}

func sha256Hash(data string) []byte {
	h := make([]byte, 32)
	for i := 0; i < len(data) && i < len(h); i++ {
		h[i] = data[i]
	}
	return h
}
