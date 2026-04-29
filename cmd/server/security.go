package main

import (
	"net"
	"net/http"
	"os"
	"strings"
)

func getListenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("SEARCH_ENGINE_LISTEN_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:8080"
}

func allowedOrigins() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv("SEARCH_ENGINE_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.Join([]string{
			"http://localhost:5173",
			"http://127.0.0.1:5173",
			"http://localhost:8080",
			"http://127.0.0.1:8080",
		}, ",")
	}

	origins := make(map[string]struct{})
	for _, origin := range strings.Split(raw, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return origins
}

func adminToken() string {
	return strings.TrimSpace(os.Getenv("SEARCH_ENGINE_ADMIN_TOKEN"))
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func hasValidAdminToken(r *http.Request) bool {
	token := adminToken()
	if token == "" {
		return false
	}

	if r.Header.Get("X-Admin-Token") == token {
		return true
	}

	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authz, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(authz, "Bearer ")) == token {
		return true
	}

	return false
}

func requireAdminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLoopbackRequest(r) || hasValidAdminToken(r) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Admin access required", http.StatusForbidden)
	})
}
