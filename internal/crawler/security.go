package crawler

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

func NormalizeSeedURL(raw string, allowedHosts []string) (string, error) {
	u, err := validateCrawlURL(raw, allowedHosts)
	if err != nil {
		return "", err
	}

	u.Fragment = ""
	return u.String(), nil
}

func validateCrawlURL(raw string, allowedHosts []string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, fmt.Errorf("local hosts are not allowed")
	}
	if len(allowedHosts) > 0 && !isAllowedHost(host, allowedHosts) {
		return nil, fmt.Errorf("host is not in allowed list")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("host did not resolve")
	}

	for _, addr := range addrs {
		if !isPublicIP(addr.IP) {
			return nil, fmt.Errorf("non-public IP targets are not allowed")
		}
	}

	return parsed, nil
}

func isAllowedHost(host string, allowedHosts []string) bool {
	for _, allowed := range allowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}

	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 || v4[0] >= 224 {
			return false
		}
	}

	return true
}
