package server

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTrustedProxiesFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        []string
		invalid     bool
	}{
		{name: "disabled by default"},
		{name: "blank", value: "  "},
		{name: "loopback", value: "127.0.0.1", want: []string{"127.0.0.1"}},
		{name: "explicit addresses and networks", value: " 127.0.0.1, ::1, 10.20.3.0/24, 2001:db8::/48 ", want: []string{"127.0.0.1", "::1", "10.20.3.0/24", "2001:db8::/48"}},
		{name: "hostname", value: "localhost", invalid: true},
		{name: "wildcard", value: "*", invalid: true},
		{name: "port", value: "127.0.0.1:80", invalid: true},
		{name: "invalid mask", value: "127.0.0.1/33", invalid: true},
		{name: "empty item", value: "127.0.0.1,", invalid: true},
		{name: "invalid IPv4", value: "127.0.0.999", invalid: true},
		{name: "IPv6 zone", value: "fe80::1%eth0", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", tc.value)
			got, err := TrustedProxiesFromEnv()
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v, want invalid = %v", err, tc.invalid)
			}
			if !tc.invalid && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("proxies = %v, want %v", got, tc.want)
			}
		})
	}
}

// Exercise the real router's ClientIP resolution, which the login limiter uses,
// without needing a database or creating accounts.
func TestRouterClientIPBehindProxy(t *testing.T) {
	for _, tc := range []struct {
		name, trusted, peer, forwarded, realIP, want string
	}{
		{name: "default ignores all headers", peer: "127.0.0.1:49100", forwarded: "203.0.113.10", realIP: "203.0.113.11", want: "127.0.0.1"},
		{name: "trusted loopback reports visitor", trusted: "127.0.0.1", peer: "127.0.0.1:49100", forwarded: "203.0.113.10", want: "203.0.113.10"},
		{name: "untrusted peer cannot spoof visitor", trusted: "127.0.0.1", peer: "198.51.100.5:49100", forwarded: "203.0.113.10", realIP: "203.0.113.11", want: "198.51.100.5"},
		{name: "alternate header ignored", trusted: "127.0.0.1", peer: "127.0.0.1:49100", realIP: "203.0.113.11", want: "127.0.0.1"},
		{name: "invalid forwarded address ignored", trusted: "127.0.0.1", peer: "127.0.0.1:49100", forwarded: "not-an-ip", realIP: "203.0.113.11", want: "127.0.0.1"},
		{name: "untrusted preceding hop ends chain", trusted: "127.0.0.1", peer: "127.0.0.1:49100", forwarded: "203.0.113.10, 198.51.100.5", want: "198.51.100.5"},
		{name: "IPv6 trusted network", trusted: "2001:db8:1::/48", peer: "[2001:db8:1::1]:49100", forwarded: "2001:db8:2::5", want: "2001:db8:2::5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", tc.trusted)
			t.Setenv("APP_ORIGINS", "http://127.0.0.1:5173")
			t.Setenv("AUTH_COOKIE_SECURE", "false")
			router := NewRouter(nil, nil)
			router.GET("/test/client-ip", func(c *gin.Context) { c.String(200, c.ClientIP()) })
			req := httptest.NewRequest("GET", "/test/client-ip", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			req.Header.Set("X-Real-IP", tc.realIP)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != 200 || response.Body.String() != tc.want {
				t.Fatalf("status = %d, client IP = %q, want %q", response.Code, response.Body.String(), tc.want)
			}
		})
	}
}
