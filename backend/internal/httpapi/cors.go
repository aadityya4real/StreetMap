package httpapi

import (
	"net/http"
	"strings"
)

const developmentExtensionOrigin = "chrome-extension://ifepahgidkeebpgeackolcjlcobgoaid"

func enablesDevelopmentExtensionCORS(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "", "development", "test":
		return true
	default:
		return false
	}
}

func developmentExtensionCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/notes" && r.URL.Path != "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}

		addVary(w.Header(), "Origin")
		origins := r.Header.Values("Origin")
		originAllowed := len(origins) == 1 && origins[0] == developmentExtensionOrigin
		if !originAllowed {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			addVary(w.Header(), "Access-Control-Request-Method")
			addVary(w.Header(), "Access-Control-Request-Headers")
			if !allowedCORSPreflight(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", developmentExtensionOrigin)
			w.Header().Set("Access-Control-Allow-Methods", allowedMethodsForPath(r.URL.Path))
			if r.URL.Path == "/api/v1/notes" {
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", developmentExtensionOrigin)
		next.ServeHTTP(w, r)
	})
}

func allowedCORSPreflight(r *http.Request) bool {
	methods := r.Header.Values("Access-Control-Request-Method")
	if len(methods) != 1 {
		return false
	}
	method := strings.TrimSpace(methods[0])
	if !isAllowedCORSMethod(r.URL.Path, method) {
		return false
	}
	if r.URL.Path != "/api/v1/notes" {
		return strings.TrimSpace(strings.Join(r.Header.Values("Access-Control-Request-Headers"), ",")) == ""
	}
	for _, value := range r.Header.Values("Access-Control-Request-Headers") {
		for _, header := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(header)) {
			case "authorization", "content-type", "idempotency-key":
			default:
				if strings.TrimSpace(header) != "" {
					return false
				}
			}
		}
	}
	return true
}

func allowedMethodsForPath(path string) string {
	if path == "/api/v1/notes" {
		return "GET, POST, OPTIONS"
	}
	return "GET, OPTIONS"
}

func isAllowedCORSMethod(path, method string) bool {
	if method == http.MethodOptions || method == http.MethodGet {
		return true
	}
	return path == "/api/v1/notes" && method == http.MethodPost
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, token := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(token), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
