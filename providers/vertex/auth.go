package vertex

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// serviceAccountKey represents the relevant fields from a Google service account
// JSON key file.
type serviceAccountKey struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// tokenSource manages OAuth2 access tokens for Google Cloud APIs.
// It supports both static tokens and automatic JWT-based token refresh
// using service account credentials.
type tokenSource struct {
	mu          sync.Mutex
	accessToken string
	expiry      time.Time
	saKey       *serviceAccountKey
	staticToken string
}

// newTokenSource creates a token source. If a static access token is provided,
// it is used directly. Otherwise, service account JSON credentials are parsed
// for automatic token management.
func newTokenSource(accessToken string, credentialsJSON []byte) (*tokenSource, error) {
	ts := &tokenSource{}

	if accessToken != "" {
		ts.staticToken = accessToken
		return ts, nil
	}

	if len(credentialsJSON) > 0 {
		var key serviceAccountKey
		if err := json.Unmarshal(credentialsJSON, &key); err != nil {
			return nil, fmt.Errorf("vertex: parse service account key: %w", err)
		}
		if key.TokenURI == "" {
			key.TokenURI = "https://oauth2.googleapis.com/token"
		}
		ts.saKey = &key
		return ts, nil
	}

	return nil, fmt.Errorf("vertex: no credentials provided; use WithAccessToken or WithCredentialsJSON")
}

// Token returns a valid access token, refreshing if necessary.
func (ts *tokenSource) Token() (string, error) {
	if ts.staticToken != "" {
		return ts.staticToken, nil
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Return cached token if still valid (with 5 minute buffer).
	if ts.accessToken != "" && time.Now().Before(ts.expiry.Add(-5*time.Minute)) {
		return ts.accessToken, nil
	}

	// Refresh via JWT exchange.
	token, expiry, err := ts.refreshToken()
	if err != nil {
		return "", err
	}

	ts.accessToken = token
	ts.expiry = expiry
	return ts.accessToken, nil
}

// refreshToken creates a JWT, signs it with the service account private key,
// and exchanges it for an access token.
func (ts *tokenSource) refreshToken() (string, time.Time, error) {
	now := time.Now()

	// Build JWT header.
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: marshal JWT header: %w", err)
	}

	// Build JWT claims.
	claims := map[string]any{
		"iss":   ts.saKey.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   ts.saKey.TokenURI,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: marshal JWT claims: %w", err)
	}

	// Encode header and claims.
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerB64 + "." + claimsB64

	// Parse the private key.
	block, _ := pem.Decode([]byte(ts.saKey.PrivateKey))
	if block == nil {
		return "", time.Time{}, fmt.Errorf("vertex: failed to decode PEM block from private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", time.Time{}, fmt.Errorf("vertex: private key is not RSA")
	}

	// Sign with RSA-SHA256.
	hashed := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: sign JWT: %w", err)
	}
	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)

	jwt := signingInput + "." + signatureB64

	// Exchange JWT for access token.
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}

	tokenReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.saKey.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: create token request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: token exchange request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort read for error message
		return "", time.Time{}, fmt.Errorf("vertex: token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("vertex: decode token response: %w", err)
	}

	expiry := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tokenResp.AccessToken, expiry, nil
}
