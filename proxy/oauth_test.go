package proxy

import (
	"testing"
	"time"
)

func TestTokenStorage(t *testing.T) {
	oauth := NewOAuth()

	err := oauth.StoreToken("server1", "access_token_123", "https://api.example.com", "https://example.com", "read write", 3600)
	if err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	token, err := oauth.GetToken("server1", "https://api.example.com", "https://example.com")
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	if token.AccessToken != "access_token_123" {
		t.Errorf("expected access token access_token_123, got %s", token.AccessToken)
	}

	if token.Audience != "https://api.example.com" {
		t.Errorf("expected audience https://api.example.com, got %s", token.Audience)
	}
}

func TestTokenWithAudience(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "token1", "aud1", "res1", "scope1", 3600)
	oauth.StoreToken("server1", "token2", "aud2", "res2", "scope2", 3600)

	token1, _ := oauth.GetToken("server1", "aud1", "res1")
	token2, _ := oauth.GetToken("server1", "aud2", "res2")

	if token1.AccessToken == token2.AccessToken {
		t.Errorf("expected different tokens for different audiences")
	}
}

func TestTokenExpiration(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "token1", "aud1", "res1", "scope1", 0)

	time.Sleep(10 * time.Millisecond)
	_, err := oauth.GetToken("server1", "aud1", "res1")

	if err == nil {
		t.Errorf("expected token to be expired")
	}

	if err == nil || err.Error() != "token expired" {
		t.Errorf("expected 'token expired' error, got: %v", err)
	}
}

func TestAudienceValidation(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "token1", "https://api.example.com", "https://example.com", "read", 3600)

	err := oauth.ValidateAudience("server1", "https://api.example.com")
	if err != nil {
		t.Errorf("expected audience validation to pass: %v", err)
	}

	err = oauth.ValidateAudience("server1", "https://other.com")
	if err == nil {
		t.Errorf("expected audience validation to fail for unknown audience")
	}
}

func TestResourceValidation(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "token1", "https://api.example.com", "https://example.com/resource1", "read", 3600)

	err := oauth.ValidateResource("server1", "https://example.com/resource1", "https://api.example.com")
	if err != nil {
		t.Errorf("expected resource validation to pass: %v", err)
	}

	err = oauth.ValidateResource("server1", "https://example.com/resource2", "https://api.example.com")
	if err == nil {
		t.Errorf("expected resource validation to fail for unknown resource")
	}
}

func TestTokenNoReusAcrossServers(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "same_token", "https://api.example.com", "https://example.com", "read", 3600)
	oauth.StoreToken("server2", "same_token", "https://api.example.com", "https://example.com", "read", 3600)

	err := oauth.CheckTokenReuse("server1", "server2", "https://api.example.com")
	if err == nil {
		t.Errorf("expected error for token reuse across servers")
	}
}

func TestStepUpChallenge(t *testing.T) {
	oauth := NewOAuth()

	resp, err := oauth.HandleStepUp("server1", "admin_scope")
	if err != nil {
		t.Fatalf("failed to handle step-up: %v", err)
	}

	if !resp.RequiresConsent {
		t.Errorf("expected RequiresConsent to be true")
	}

	if resp.Challenge == "" {
		t.Errorf("expected non-empty challenge")
	}

	if resp.Granted {
		t.Errorf("expected Granted to be false initially")
	}
}

func TestStepUpRetryLimit(t *testing.T) {
	oauth := NewOAuth()

	for i := 0; i < 3; i++ {
		resp, err := oauth.HandleStepUp("server1", "scope1")
		if err != nil {
			t.Fatalf("step-up %d failed: %v", i+1, err)
		}
		if !resp.RequiresConsent {
			t.Errorf("step-up %d: expected RequiresConsent", i+1)
		}
	}

	_, err := oauth.HandleStepUp("server1", "scope1")
	if err == nil {
		t.Errorf("expected error after exceeding retry limit")
	}

	if err.Error() != "step-up retries exceeded" {
		t.Errorf("expected 'step-up retries exceeded' error, got %v", err)
	}
}

func TestStepUpGrant(t *testing.T) {
	oauth := NewOAuth()

	oauth.HandleStepUp("server1", "admin_scope")

	err := oauth.GrantStepUp("server1", "admin_scope", "new_token_123", 7200)
	if err != nil {
		t.Fatalf("failed to grant step-up: %v", err)
	}

	token, _ := oauth.GetToken("server1", "", "admin_scope")
	if token.AccessToken != "new_token_123" {
		t.Errorf("expected access token new_token_123, got %s", token.AccessToken)
	}
}

func TestStepUpDeny(t *testing.T) {
	oauth := NewOAuth()

	oauth.HandleStepUp("server1", "admin_scope")

	err := oauth.DenyStepUp("server1", "admin_scope")
	if err != nil {
		t.Fatalf("failed to deny step-up: %v", err)
	}

	newResp, err := oauth.HandleStepUp("server1", "admin_scope")
	if err != nil {
		t.Fatalf("expected to create new step-up after denial: %v", err)
	}

	if !newResp.RequiresConsent {
		t.Errorf("expected new step-up challenge")
	}
}

func TestTokenRevocation(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "token1", "aud1", "res1", "scope1", 3600)

	_, err := oauth.GetToken("server1", "aud1", "res1")
	if err != nil {
		t.Fatalf("expected token to exist before revocation")
	}

	oauth.RevokeToken("server1", "aud1", "res1")

	_, err = oauth.GetToken("server1", "aud1", "res1")
	if err == nil {
		t.Errorf("expected token to be revoked")
	}
}

func TestGetTokensByServer(t *testing.T) {
	oauth := NewOAuth()

	oauth.StoreToken("server1", "token1", "aud1", "res1", "scope1", 3600)
	oauth.StoreToken("server1", "token2", "aud2", "res2", "scope2", 3600)
	oauth.StoreToken("server2", "token3", "aud3", "res3", "scope3", 3600)

	tokens := oauth.GetTokensByServer("server1")

	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens for server1, got %d", len(tokens))
	}

	for _, token := range tokens {
		if token.ServerID != "server1" {
			t.Errorf("expected server1, got %s", token.ServerID)
		}
	}
}

func TestPKCEChallenge(t *testing.T) {
	pkce, err := GeneratePKCEChallenge()
	if err != nil {
		t.Fatalf("failed to generate PKCE challenge: %v", err)
	}

	if pkce.CodeChallenge == "" {
		t.Errorf("expected non-empty code challenge")
	}

	if pkce.CodeVerifier == "" {
		t.Errorf("expected non-empty code verifier")
	}

	if pkce.State == "" {
		t.Errorf("expected non-empty state")
	}
}

func TestTokenMissingRequiredFields(t *testing.T) {
	oauth := NewOAuth()

	err := oauth.StoreToken("", "token1", "aud1", "res1", "scope1", 3600)
	if err == nil {
		t.Errorf("expected error for missing serverID")
	}

	err = oauth.StoreToken("server1", "token1", "", "res1", "scope1", 3600)
	if err == nil {
		t.Errorf("expected error for missing audience")
	}
}
