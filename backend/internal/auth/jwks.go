package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxJWKSBytes        = 1 << 20
	defaultHTTPTimeout  = 3 * time.Second
	defaultRefreshPause = time.Second
)

type jwksCache struct {
	url        string
	algorithm  string
	client     *http.Client
	cacheFor   time.Duration
	refreshGap time.Duration

	mu          sync.RWMutex
	keys        map[string]crypto.PublicKey
	expiresAt   time.Time
	lastAttempt time.Time
	refreshing  chan struct{}
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType  string   `json:"kty"`
	KeyID    string   `json:"kid"`
	Use      string   `json:"use"`
	Alg      string   `json:"alg"`
	Curve    string   `json:"crv"`
	X        string   `json:"x"`
	Y        string   `json:"y"`
	Modulus  string   `json:"n"`
	Exponent string   `json:"e"`
	KeyOps   []string `json:"key_ops"`
}

func newJWKSCache(projectURL, algorithm string, cacheFor time.Duration, client *http.Client) (*jwksCache, error) {
	if err := ValidateSupabaseProjectURL(projectURL); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(projectURL)
	if err != nil { // ValidateSupabaseProjectURL has already parsed this URL.
		return nil, fmt.Errorf("invalid Supabase project URL")
	}
	parsed.Path = "/auth/v1/.well-known/jwks.json"
	parsed.RawPath = ""
	if client == nil {
		client = newJWKSHTTPClient(nil)
	}
	return &jwksCache{
		url:        parsed.String(),
		algorithm:  algorithm,
		client:     client,
		cacheFor:   cacheFor,
		refreshGap: defaultRefreshPause,
		keys:       make(map[string]crypto.PublicKey),
	}, nil
}

func newJWKSHTTPClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = &http.Transport{
			Proxy:                 nil,
			DialContext:           safeJWKSConnection,
			TLSHandshakeTimeout:   defaultHTTPTimeout,
			ResponseHeaderTimeout: defaultHTTPTimeout,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConns:          2,
			MaxIdleConnsPerHost:   2,
			MaxConnsPerHost:       8,
		}
	}
	return &http.Client{
		Timeout:   defaultHTTPTimeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type ipAddressResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type connectionDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

func safeJWKSConnection(ctx context.Context, network, address string) (net.Conn, error) {
	return safeJWKSConnectionWith(ctx, network, address, net.DefaultResolver, &net.Dialer{Timeout: defaultHTTPTimeout})
}

func safeJWKSConnectionWith(ctx context.Context, network, address string, resolver ipAddressResolver, dialer connectionDialer) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !isHostedSupabaseProjectHost(host) || (port != "443") {
		return nil, fmt.Errorf("JWKS destination is not an approved Supabase project endpoint")
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("could not resolve Supabase project endpoint")
	}
	for _, address := range addresses {
		if blockedJWKSAddress(address.IP) {
			return nil, fmt.Errorf("Supabase project endpoint resolved to a prohibited network address")
		}
	}
	var lastErr error
	for _, address := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if lastErr == nil {
		return nil, fmt.Errorf("could not connect to Supabase project endpoint")
	}
	return nil, lastErr
}

func isHostedSupabaseProjectHost(host string) bool {
	host = strings.ToLower(host)
	const suffix = ".supabase.co"
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	projectRef := strings.TrimSuffix(host, suffix)
	if projectRef == "" || len(projectRef) > 63 || strings.Contains(projectRef, ".") || projectRef[0] == '-' || projectRef[len(projectRef)-1] == '-' {
		return false
	}
	for _, char := range projectRef {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func blockedJWKSAddress(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		if !ipv4.IsGlobalUnicast() || ipv4.IsPrivate() || ipv4.IsLoopback() || ipv4.IsLinkLocalUnicast() || ipv4.IsLinkLocalMulticast() || ipv4.IsUnspecified() || ipv4.IsMulticast() {
			return true
		}
		if ipv4[0] == 100 && ipv4[1]&0xc0 == 0x40 { // RFC 6598 shared address space
			return true
		}
		return ipv4.Equal(net.IPv4(100, 100, 100, 200)) || ipv4.Equal(net.IPv4(168, 63, 129, 16)) || ipv4.Equal(net.IPv4(169, 254, 170, 2)) // Alibaba/Azure metadata and AWS task metadata
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	return false
}

// ValidateSupabaseProjectURL accepts only canonical hosted Supabase project
// origins. Custom domains and self-hosted instances need a separately reviewed
// explicit allowlist before they can be trusted as a JWKS source.
func ValidateSupabaseProjectURL(projectURL string) error {
	parsed, err := url.Parse(projectURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("Supabase project URL must be an HTTPS origin")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("Supabase project URL must use the standard HTTPS port")
	}
	if !isHostedSupabaseProjectHost(parsed.Hostname()) {
		return fmt.Errorf("Supabase project URL must use a canonical hosted Supabase project domain")
	}
	return nil
}

func (c *jwksCache) key(ctx context.Context, keyID string) (crypto.PublicKey, error) {
	if keyID == "" {
		return nil, ErrInvalidToken
	}
	now := time.Now()
	c.mu.RLock()
	if now.Before(c.expiresAt) {
		if key := c.keys[keyID]; key != nil {
			c.mu.RUnlock()
			return key, nil
		}
	}
	c.mu.RUnlock()

	c.mu.Lock()
	now = time.Now()
	if now.Before(c.expiresAt) {
		if key := c.keys[keyID]; key != nil {
			c.mu.Unlock()
			return key, nil
		}
	}
	if c.refreshing != nil {
		inFlight := c.refreshing
		c.mu.Unlock()
		wait, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
		defer cancel()
		select {
		case <-inFlight:
			return c.cachedKeyAfterRefresh(keyID)
		case <-wait.Done():
			return nil, ErrUnavailable
		}
	}
	if !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < c.refreshGap {
		fresh := now.Before(c.expiresAt)
		c.mu.Unlock()
		if fresh {
			return nil, ErrInvalidToken
		}
		return nil, ErrUnavailable
	}
	inFlight := make(chan struct{})
	c.refreshing = inFlight
	c.lastAttempt = now
	c.mu.Unlock()

	err := c.fetch(ctx)
	c.mu.Lock()
	c.refreshing = nil
	close(inFlight)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.cachedKeyAfterRefresh(keyID)
}

func (c *jwksCache) cachedKeyAfterRefresh(keyID string) (crypto.PublicKey, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !time.Now().Before(c.expiresAt) {
		return nil, ErrUnavailable
	}
	if key := c.keys[keyID]; key != nil {
		return key, nil
	}
	return nil, ErrInvalidToken
}

func (c *jwksCache) fetch(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, defaultHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return ErrUnavailable
	}
	response, err := c.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBytes+1))
	if err != nil || len(body) > maxJWKSBytes {
		return ErrUnavailable
	}
	var document jwksDocument
	if err := json.Unmarshal(body, &document); err != nil || len(document.Keys) == 0 {
		return ErrUnavailable
	}
	keys := make(map[string]crypto.PublicKey, len(document.Keys))
	seen := make(map[string]struct{}, len(document.Keys))
	for _, item := range document.Keys {
		if item.KeyID == "" {
			return ErrUnavailable
		}
		if _, exists := seen[item.KeyID]; exists {
			return ErrUnavailable
		}
		seen[item.KeyID] = struct{}{}
		if (item.Use != "" && item.Use != "sig") || (item.Alg != "" && item.Alg != c.algorithm) || !item.permitsVerification() {
			continue
		}
		key, err := item.publicKey(c.algorithm)
		if err != nil {
			return ErrUnavailable
		}
		keys[item.KeyID] = key
	}
	if len(keys) == 0 {
		return ErrUnavailable
	}
	c.mu.Lock()
	c.keys = keys
	c.expiresAt = time.Now().Add(c.cacheFor)
	c.mu.Unlock()
	return nil
}

func (k jwk) permitsVerification() bool {
	if k.KeyOps == nil {
		return true
	}
	seen := make(map[string]struct{}, len(k.KeyOps))
	permitsVerification := false
	for _, operation := range k.KeyOps {
		if _, exists := seen[operation]; exists {
			return false
		}
		seen[operation] = struct{}{}
		if operation == "verify" {
			permitsVerification = true
		}
	}
	return permitsVerification
}

func (k jwk) publicKey(algorithm string) (crypto.PublicKey, error) {
	switch algorithm {
	case "RS256":
		if k.KeyType != "RSA" {
			return nil, errors.New("wrong key type")
		}
		modulusBytes, err := base64.RawURLEncoding.DecodeString(k.Modulus)
		if err != nil {
			return nil, err
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(k.Exponent)
		if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			return nil, errors.New("invalid RSA exponent")
		}
		exponentValue := new(big.Int).SetBytes(exponentBytes)
		if !exponentValue.IsInt64() || exponentValue.Int64() < 3 || exponentValue.Int64()%2 == 0 {
			return nil, errors.New("invalid RSA exponent")
		}
		modulus := new(big.Int).SetBytes(modulusBytes)
		if modulus.BitLen() < 2048 {
			return nil, errors.New("RSA modulus is too small")
		}
		return &rsa.PublicKey{N: modulus, E: int(exponentValue.Int64())}, nil
	case "ES256":
		if k.KeyType != "EC" || k.Curve != "P-256" {
			return nil, errors.New("wrong EC curve")
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, err
		}
		x, y := new(big.Int).SetBytes(xBytes), new(big.Int).SetBytes(yBytes)
		if !elliptic.P256().IsOnCurve(x, y) {
			return nil, errors.New("EC point is not on P-256")
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
	case "EdDSA":
		if k.KeyType != "OKP" || k.Curve != "Ed25519" {
			return nil, errors.New("wrong EdDSA curve")
		}
		publicKey, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 key")
		}
		return ed25519.PublicKey(publicKey), nil
	default:
		return nil, ErrInvalidToken
	}
}
