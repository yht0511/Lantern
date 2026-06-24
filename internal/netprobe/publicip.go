package netprobe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func PublicIP(ctx context.Context, family string) (string, error) {
	url := "https://api.ipify.org"
	network := "tcp4"
	if family == "ipv6" {
		url = "https://api64.ipify.org"
		network = "tcp6"
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, address)
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("public IP lookup failed: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(data))
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("public IP lookup returned invalid IP %q", ip)
	}
	if family == "ipv4" && parsed.To4() == nil {
		return "", fmt.Errorf("public IP lookup returned non-IPv4 %q", ip)
	}
	if family == "ipv6" && parsed.To4() != nil {
		return "", fmt.Errorf("public IP lookup returned non-IPv6 %q", ip)
	}
	return ip, nil
}
