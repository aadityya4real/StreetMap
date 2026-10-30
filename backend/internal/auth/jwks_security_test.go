package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateSupabaseProjectURL(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://project-ref.supabase.co", true},
		{"https://project-ref.supabase.co/", true},
		{"http://project-ref.supabase.co", false},
		{"https://unrelated.example", false},
		{"https://supabase.co.attacker.example", false},
		{"https://project-ref.supabase.co.attacker.example", false},
		{"https://nested.project-ref.supabase.co", false},
		{"https://user:pass@project-ref.supabase.co", false},
		{"https://project-ref.supabase.co:8443", false},
		{"https://project-ref.supabase.co:443", true},
		{"https://project-ref.supabase.co?next=x", false},
		{"https://project-ref.supabase.co#fragment", false},
		{"https://127.0.0.1", false},
		{"https://-project.supabase.co", false},
		{"https://[", false},
		{"https://project-ref.supabase.co:abc", false},
	}
	for _, test := range tests {
		t.Run(test.url, func(t *testing.T) {
			err := ValidateSupabaseProjectURL(test.url)
			if (err == nil) != test.ok {
				t.Fatalf("ValidateSupabaseProjectURL(%q) error = %v, want ok=%v", test.url, err, test.ok)
			}
		})
	}
}

type fixedResolver []net.IPAddr

func (resolver fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver, nil
}

type captureDialer struct {
	address string
	err     error
}

func (dialer *captureDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	dialer.address = address
	return nil, dialer.err
}

func TestSafeJWKSConnectionRejectsProhibitedResolvedAddresses(t *testing.T) {
	prohibited := []string{
		"127.0.0.1", "10.1.2.3", "192.168.1.4", "169.254.169.254", "0.0.0.0",
		"224.0.0.1", "100.64.0.1", "100.100.100.200", "::1", "fc00::1", "fe80::1",
		"::ffff:10.0.0.1", "168.63.129.16", "169.254.170.2",
	}
	for _, raw := range prohibited {
		t.Run(raw, func(t *testing.T) {
			dialer := &captureDialer{}
			_, err := safeJWKSConnectionWith(context.Background(), "tcp", "project-ref.supabase.co:443", fixedResolver{{IP: net.ParseIP(raw)}}, dialer)
			if err == nil {
				t.Fatal("prohibited destination was accepted")
			}
			if dialer.address != "" {
				t.Fatalf("dial attempted before address rejection: %q", dialer.address)
			}
		})
	}
	resolver := fixedResolver{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.1")}}
	dialer := &captureDialer{}
	if _, err := safeJWKSConnectionWith(context.Background(), "tcp", "project-ref.supabase.co:443", resolver, dialer); err == nil {
		t.Fatal("mixed public and prohibited DNS answers were accepted")
	}
}

func TestSafeJWKSConnectionPinsAllowedResolvedAddress(t *testing.T) {
	wantErr := errors.New("dial captured")
	dialer := &captureDialer{err: wantErr}
	_, err := safeJWKSConnectionWith(context.Background(), "tcp", "project-ref.supabase.co:443", fixedResolver{{IP: net.ParseIP("8.8.8.8")}}, dialer)
	if !errors.Is(err, wantErr) || dialer.address != "8.8.8.8:443" {
		t.Fatalf("dial result address=%q err=%v", dialer.address, err)
	}
}

func TestSafeJWKSConnectionRejectsUntrustedHostAndPortBeforeResolution(t *testing.T) {
	dialer := &captureDialer{}
	for _, destination := range []string{"supabase.co.attacker.example:443", "project-ref.supabase.co:8443"} {
		if _, err := safeJWKSConnectionWith(context.Background(), "tcp", destination, fixedResolver{{IP: net.ParseIP("8.8.8.8")}}, dialer); err == nil {
			t.Fatalf("untrusted destination %q was accepted", destination)
		}
	}
	if dialer.address != "" {
		t.Fatalf("untrusted destination reached dialer at %q", dialer.address)
	}
}

func TestJWKSRedirectIsNotFollowed(t *testing.T) {
	var redirected atomic.Bool
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer redirectTarget.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer server.Close()
	client := newJWKSHTTPClient(testSupabaseTransport(server.URL, server.Client().Transport))
	response, err := client.Get(testProjectURL + "/auth/v1/.well-known/jwks.json")
	if err != nil {
		t.Fatalf("request JWKS: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("response status = %d, want redirect response", response.StatusCode)
	}
	if redirected.Load() {
		t.Fatal("redirect to unrelated destination was followed")
	}
}

func TestJWKSRejectsMalformedEmptyDuplicateAndInvalidKeyOps(t *testing.T) {
	key := newTestSigningKey(t, "security-key")
	valid := key.jwk()
	tests := []struct {
		name string
		body string
	}{
		{"empty", `{"keys":[]}`},
		{"missing key id", `{"keys":[{}]}`},
		{"malformed JSON", `{"keys":`},
		{"duplicate kid", `{"keys":[` + jwkJSON(t, valid) + `,` + jwkJSON(t, valid) + `]}`},
		{"sign-only key_ops", `{"keys":[` + jwkJSONWithOps(t, valid, []string{"sign"}) + `]}`},
		{"unrelated key_ops", `{"keys":[` + jwkJSONWithOps(t, valid, []string{"encrypt"}) + `]}`},
		{"malformed key_ops type", `{"keys":[{"kty":"RSA","kid":"security-key","key_ops":"verify"}]}`},
		{"invalid key structure", `{"keys":[{"kty":"RSA","kid":"broken","alg":"RS256","use":"sig","n":"AQ","e":"Aw"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := cacheForJWKSBody(t, test.body)
			if _, err := cache.key(context.Background(), key.id); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("cache key error = %v, want ErrUnavailable", err)
			}
		})
	}

	if !(jwk{KeyOps: nil}).permitsVerification() || !(jwk{KeyOps: []string{"verify"}}).permitsVerification() || !(jwk{KeyOps: []string{"verify", "sign"}}).permitsVerification() {
		t.Fatal("absent or verify key_ops should be accepted")
	}
}

func TestJWKSRejectsOversizedResponse(t *testing.T) {
	cache := cacheForJWKSBody(t, `{"keys":[`+strings.Repeat(" ", maxJWKSBytes)+`]}`)
	if _, err := cache.key(context.Background(), "anything"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("oversized response error = %v, want ErrUnavailable", err)
	}
}

func jwkJSON(t *testing.T, value map[string]string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func jwkJSONWithOps(t *testing.T, value map[string]string, ops []string) string {
	t.Helper()
	copy := make(map[string]any, len(value)+1)
	for key, item := range value {
		copy[key] = item
	}
	copy["key_ops"] = ops
	encoded, err := json.Marshal(copy)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func cacheForJWKSBody(t *testing.T, body string) *jwksCache {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(server.Close)
	cache, err := newJWKSCache(testProjectURL, "RS256", time.Minute, newJWKSHTTPClient(testSupabaseTransport(server.URL, server.Client().Transport)))
	if err != nil {
		t.Fatal(err)
	}
	return cache
}
