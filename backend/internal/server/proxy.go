package server

import (
	"errors"
	"net/netip"
	"os"
	"strings"
)

// Only explicitly configured proxy peers may supply the client IP used by login
// rate limiting. The default remains suitable for direct local development.
func TrustedProxiesFromEnv() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		return nil, nil
	}
	var proxies []string
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if address, err := netip.ParseAddr(value); err == nil && address.Zone() == "" {
			proxies = append(proxies, address.String())
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			proxies = append(proxies, prefix.Masked().String())
			continue
		}
		return nil, errors.New("TRUSTED_PROXIES 必须为逗号分隔的 IP 或 CIDR，不接受域名、端口或空条目")
	}
	return proxies, nil
}
