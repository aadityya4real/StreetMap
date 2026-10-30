package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testProjectURL = "https://streetmap-test.supabase.co"
	testIssuer     = testProjectURL + "/auth/v1"
	testAudience   = "streetmap-api-test"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTripper roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func testSupabaseTransport(serverURL string, base http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		target, err := url.Parse(serverURL)
		if err != nil {
			return nil, err
		}
		target.Path = request.URL.Path
		target.RawQuery = request.URL.RawQuery
		clone.URL = target
		clone.Host = target.Host
		return base.RoundTrip(clone)
	})
}

type testSigningKey struct {
	id      string
	private *rsa.PrivateKey
}

func newTestSigningKey(t *testing.T, id string) testSigningKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	return testSigningKey{id: id, private: private}
}

func (key testSigningKey) jwk() map[string]string {
	return map[string]string{
		"kty": "RSA", "kid": key.id, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.private.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.private.E)).Bytes()),
	}
}

func signedTestToken(t *testing.T, key testSigningKey, issuer string, mutate func(*supabaseClaims)) string {
	t.Helper()
	now := time.Now()
	anonymous := false
	claims := &supabaseClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: "user-subject-123", Audience: jwt.ClaimStrings{testAudience},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)),
		},
		Role: "authenticated", IsAnonymous: &anonymous,
		UserMetadata: map[string]any{"full_name": "A StreetMap User"},
	}
	if mutate != nil {
		mutate(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = key.id
	raw, err := token.SignedString(key.private)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return raw
}

func newTestVerifier(t *testing.T, keys ...testSigningKey) (*Verifier, *atomic.Int32, *atomic.Int32, func(...testSigningKey), func() (<-chan struct{}, func())) {
	t.Helper()
	var calls atomic.Int32
	var status atomic.Int32
	var keyLock sync.RWMutex
	var blockLock sync.Mutex
	var nextBlock *refreshBlock
	currentKeys := append([]testSigningKey(nil), keys...)
	status.Store(http.StatusOK)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, request)
			return
		}
		blockLock.Lock()
		block := nextBlock
		nextBlock = nil
		blockLock.Unlock()
		if block != nil {
			close(block.started)
			select {
			case <-block.release:
			case <-request.Context().Done():
				return
			}
		}
		w.WriteHeader(int(status.Load()))
		if status.Load() != http.StatusOK {
			return
		}
		jwks := struct {
			Keys []map[string]string `json:"keys"`
		}{Keys: make([]map[string]string, 0)}
		keyLock.RLock()
		defer keyLock.RUnlock()
		for _, key := range currentKeys {
			jwks.Keys = append(jwks.Keys, key.jwk())
		}
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(server.Close)
	config := Config{
		ProjectURL: testProjectURL, Issuer: testIssuer, Audience: testAudience,
		Algorithm: "RS256", JWKSCacheFor: time.Minute, ClockSkew: 0,
	}
	client := newJWKSHTTPClient(testSupabaseTransport(server.URL, server.Client().Transport))
	verifier, err := newVerifierWithClient(config, client)
	if err != nil {
		t.Fatalf("create test verifier: %v", err)
	}
	setKeys := func(next ...testSigningKey) {
		keyLock.Lock()
		defer keyLock.Unlock()
		currentKeys = append([]testSigningKey(nil), next...)
	}
	blockNextRefresh := func() (<-chan struct{}, func()) {
		block := &refreshBlock{started: make(chan struct{}), release: make(chan struct{})}
		blockLock.Lock()
		nextBlock = block
		blockLock.Unlock()
		var releaseOnce sync.Once
		return block.started, func() { releaseOnce.Do(func() { close(block.release) }) }
	}
	return verifier, &calls, &status, setKeys, blockNextRefresh
}

type refreshBlock struct {
	started chan struct{}
	release chan struct{}
}

func TestVerifierAcceptsValidAccessTokenAndUsesSafeDisplayName(t *testing.T) {
	key := newTestSigningKey(t, "active-key")
	verifier, _, _, _, _ := newTestVerifier(t, key)
	raw := signedTestToken(t, key, verifier.issuer, nil)
	identity, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if identity.Issuer != verifier.issuer || identity.Subject != "user-subject-123" || identity.DisplayName != "A StreetMap User" {
		t.Fatalf("unexpected identity: %+v", identity)
	}

	token := signedTestToken(t, key, verifier.issuer, func(claims *supabaseClaims) {
		claims.UserMetadata = map[string]any{"full_name": "unsafe\nname", "name": string(make([]byte, 81))}
	})
	identity, err = verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("verify token with unsafe profile metadata: %v", err)
	}
	if identity.DisplayName != fallbackDisplayName {
		t.Fatalf("unsafe name was not replaced: %q", identity.DisplayName)
	}
}

func TestVerifierRejectsMalformedOrInappropriateTokens(t *testing.T) {
	key := newTestSigningKey(t, "active-key")
	otherKey := newTestSigningKey(t, "active-key")
	verifier, _, _, _, _ := newTestVerifier(t, key)
	anonymous := true
	tests := []struct {
		name  string
		token func() string
	}{
		{"malformed", func() string { return "not.a.jwt" }},
		{"invalid signature", func() string { return signedTestToken(t, otherKey, verifier.issuer, nil) }},
		{"expired", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute)) })
		}},
		{"future not-before", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Minute)) })
		}},
		{"future issued-at", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Minute)) })
		}},
		{"wrong issuer", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.Issuer = "https://wrong.example/auth/v1" })
		}},
		{"wrong audience", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.Audience = jwt.ClaimStrings{"other-api"} })
		}},
		{"additional audience", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.Audience = jwt.ClaimStrings{testAudience, "other-api"} })
		}},
		{"missing subject", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.Subject = "" })
		}},
		{"anonymous token", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.IsAnonymous = &anonymous })
		}},
		{"wrong role", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.Role = "anon" })
		}},
		{"missing anonymous claim", func() string {
			return signedTestToken(t, key, verifier.issuer, func(c *supabaseClaims) { c.IsAnonymous = nil })
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), test.token()); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify error = %v, want ErrInvalidToken", err)
			}
		})
	}

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"iss": verifier.issuer, "sub": "subject", "aud": testAudience, "exp": time.Now().Add(time.Hour).Unix()})
	unsigned.Header["kid"] = key.id
	raw, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("create unsigned token: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unsigned token error = %v, want ErrInvalidToken", err)
	}

	hmac := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": verifier.issuer, "sub": "subject", "aud": testAudience, "exp": time.Now().Add(time.Hour).Unix()})
	hmac.Header["kid"] = key.id
	raw, err = hmac.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("create HMAC token: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unsupported algorithm error = %v, want ErrInvalidToken", err)
	}

	malformedClaims := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": verifier.issuer, "sub": "subject", "aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": "not-a-number",
		"role": 7, "is_anonymous": "false", "user_metadata": []string{"unexpected"},
	})
	malformedClaims.Header["kid"] = key.id
	raw, err = malformedClaims.SignedString(key.private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("malformed claim types error = %v, want ErrInvalidToken", err)
	}

	if _, err := verifier.Verify(context.Background(), strings.Repeat("a", 16*1024+1)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("oversized token error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifierRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{"missing audience", Config{ProjectURL: testProjectURL, Issuer: testIssuer, Algorithm: "RS256", JWKSCacheFor: time.Minute}},
		{"unsupported algorithm", Config{ProjectURL: testProjectURL, Issuer: testIssuer, Audience: testAudience, Algorithm: "HS256", JWKSCacheFor: time.Minute}},
		{"non-HTTPS project", Config{ProjectURL: "http://project.test", Issuer: testIssuer, Audience: testAudience, Algorithm: "RS256", JWKSCacheFor: time.Minute}},
		{"issuer from another project", Config{ProjectURL: testProjectURL, Issuer: "https://other-project.test/auth/v1", Audience: testAudience, Algorithm: "RS256", JWKSCacheFor: time.Minute}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewVerifier(test.config); err == nil {
				t.Fatal("NewVerifier accepted invalid configuration")
			}
		})
	}
}

func TestJWKSRefreshFailureUnknownKeyAndRotation(t *testing.T) {
	first := newTestSigningKey(t, "first-key")
	rotated := newTestSigningKey(t, "rotated-key")
	verifier, calls, _, setKeys, _ := newTestVerifier(t, first)
	verifier.keys.refreshGap = 0

	if _, err := verifier.Verify(context.Background(), signedTestToken(t, first, verifier.issuer, nil)); err != nil {
		t.Fatalf("verify using first key: %v", err)
	}
	setKeys(rotated)
	if _, err := verifier.Verify(context.Background(), signedTestToken(t, rotated, verifier.issuer, nil)); err != nil {
		t.Fatalf("verify after JWKS key rotation: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("JWKS requests after rotation = %d, want 2", got)
	}

	failing, _, failingStatus, _, _ := newTestVerifier(t, first)
	failingStatus.Store(http.StatusServiceUnavailable)
	if _, err := failing.Verify(context.Background(), signedTestToken(t, first, failing.issuer, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("JWKS failure error = %v, want ErrUnavailable", err)
	}

	unknown, _, _, _, _ := newTestVerifier(t, first)
	unknown.keys.refreshGap = 0
	if _, err := unknown.Verify(context.Background(), signedTestToken(t, rotated, unknown.issuer, nil)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown key error = %v, want ErrInvalidToken", err)
	}
}

func TestJWKSRefreshDoesNotBlockCachedKeysAndCoalescesUnknownKeyTraffic(t *testing.T) {
	first := newTestSigningKey(t, "first-key")
	rotated := newTestSigningKey(t, "rotated-key")
	verifier, calls, _, setKeys, blockNextRefresh := newTestVerifier(t, first)
	if _, err := verifier.Verify(context.Background(), signedTestToken(t, first, verifier.issuer, nil)); err != nil {
		t.Fatalf("warm initial key cache: %v", err)
	}

	setKeys(rotated)
	verifier.keys.refreshGap = 0
	refreshStarted, releaseRefresh := blockNextRefresh()
	defer releaseRefresh()
	rotatedToken := signedTestToken(t, rotated, verifier.issuer, nil)
	rotationResult := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), rotatedToken)
		rotationResult <- err
	}()
	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("JWKS refresh did not start")
	}

	verifier.keys.mu.Lock()
	verifier.keys.refreshGap = time.Hour
	verifier.keys.mu.Unlock()
	const unknownRequests = 24
	var wait sync.WaitGroup
	started := make(chan struct{}, unknownRequests)
	results := make(chan error, unknownRequests)
	for index := range unknownRequests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			started <- struct{}{}
			_, err := verifier.keys.key(context.Background(), fmt.Sprintf("untrusted-kid-%d", index))
			results <- err
		}(index)
	}
	for range unknownRequests {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("unknown-key callers did not start")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("requests during blocked rotation = %d, want one initial and one coalesced refresh", got)
	}

	knownResult := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), signedTestToken(t, first, verifier.issuer, nil))
		knownResult <- err
	}()
	select {
	case err := <-knownResult:
		if err != nil {
			t.Fatalf("cached key failed during blocked refresh: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("known cached-key lookup waited for the JWKS network response")
	}

	releaseRefresh()
	for range unknownRequests {
		select {
		case err := <-results:
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("unknown-key error = %v, want ErrInvalidToken", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("coalesced unknown-key request did not complete after refresh")
		}
	}
	wait.Wait()
	select {
	case err := <-rotationResult:
		if err != nil {
			t.Fatalf("rotated key was not available after successful refresh: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rotated-key verification did not finish after refresh release")
	}
	for index := range unknownRequests {
		if _, err := verifier.keys.key(context.Background(), fmt.Sprintf("arbitrary-kid-%d", index)); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("post-refresh arbitrary kid %d error = %v", index, err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("repeated unknown keys triggered %d JWKS requests, want 2 total", got)
	}
}

func TestJWKSExpiredKeysFailClosedWhenRefreshFails(t *testing.T) {
	key := newTestSigningKey(t, "expiring-key")
	verifier, _, status, _, _ := newTestVerifier(t, key)
	if _, err := verifier.Verify(context.Background(), signedTestToken(t, key, verifier.issuer, nil)); err != nil {
		t.Fatalf("warm key cache: %v", err)
	}
	verifier.keys.mu.Lock()
	verifier.keys.expiresAt = time.Now().Add(-time.Second)
	verifier.keys.mu.Unlock()
	status.Store(http.StatusServiceUnavailable)
	if _, err := verifier.Verify(context.Background(), signedTestToken(t, key, verifier.issuer, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expired cached key with failed refresh error = %v, want ErrUnavailable", err)
	}
}

func TestJWKSCacheAvoidsFetchPerRequest(t *testing.T) {
	key := newTestSigningKey(t, "cached-key")
	verifier, calls, _, _, _ := newTestVerifier(t, key)
	for i := 0; i < 3; i++ {
		if _, err := verifier.Verify(context.Background(), signedTestToken(t, key, verifier.issuer, nil)); err != nil {
			t.Fatalf("verify request %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("JWKS requests = %d, want one cached request", got)
	}
}
