package test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"cpa-usage-ranking/internal/httpserver"
	"cpa-usage-ranking/internal/network"
	"github.com/gin-gonic/gin"
)

func TestRegistrationSources(t *testing.T) {
	for _, tc := range []struct {
		name, remote, forwarded, real, want string
		proxies                             []string
		group                               bool
	}{
		{name: "default ignores forged headers", remote: "192.0.2.4:1234", forwarded: "198.51.100.2", real: "203.0.113.1", want: "192.0.2.4"},
		{name: "mapped IPv4", remote: "[::ffff:192.0.2.4]:1234", group: true, want: "192.0.2.4"},
		{name: "IPv6 normalized", remote: "[2001:0db8:0001:0002::1234]:1234", want: "2001:db8:1:2::1234"},
		{name: "IPv6 grouped", remote: "[2001:db8:1:2:abcd::1234]:1234", group: true, want: "2001:db8:1:2::/64"},
		{name: "trusted peer", remote: "10.0.0.1:1234", proxies: []string{"10.0.0.0/8"}, forwarded: "198.51.100.2", want: "198.51.100.2"},
		{name: "untrusted peer", remote: "192.0.2.4:1234", proxies: []string{"10.0.0.0/8"}, forwarded: "198.51.100.2", want: "192.0.2.4"},
		{name: "rightmost untrusted hop", remote: "10.0.0.1:1234", proxies: []string{"10.0.0.0/8"}, forwarded: "203.0.113.9, 198.51.100.2, 10.0.0.2", want: "198.51.100.2"},
		{name: "malformed forwarded falls back", remote: "10.0.0.1:1234", proxies: []string{"10.0.0.0/8"}, forwarded: "invalid", want: "10.0.0.1"},
		{name: "trusted real IP", remote: "10.0.0.1:1234", proxies: []string{"10.0.0.0/8"}, real: "::ffff:192.0.2.4", want: "192.0.2.4"},
		{name: "invalid remote", remote: "invalid", forwarded: "198.51.100.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := httpserver.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err := router.SetTrustedProxies(tc.proxies); err != nil {
				t.Fatal(err)
			}
			router.GET("/test/source", func(c *gin.Context) {
				source, err := network.RegistrationSource(c, tc.group)
				if tc.want == "" {
					if err == nil {
						t.Error("invalid source accepted")
					}
					c.Status(400)
					return
				}
				if err != nil || source != tc.want {
					t.Errorf("source = %q, error = %v; want %q", source, err, tc.want)
				}
				c.Status(200)
			})
			r := httptest.NewRequest("GET", "/test/source?source=203.0.113.20", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("X-Real-IP", tc.real)
			r.Header.Set("Forwarded", "for=203.0.113.20")
			r.Header.Set("CF-Connecting-IP", "203.0.113.20")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
		})
	}
}
