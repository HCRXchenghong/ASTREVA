package httpx

import (
	"net/http/httptest"
	"testing"
)

func testTrustedProxies() trustedProxySet {
	return parseTrustedProxyCIDRs("127.0.0.1/32,::1/128,172.16.0.0/12")
}

func TestResolveClientIP(t *testing.T) {
	proxies := testTrustedProxies()
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		cfIP       string
		realIP     string
		wantIP     string
		wantSource string
	}{
		{
			name:       "direct untrusted connection ignores spoofed headers",
			remoteAddr: "203.0.113.9:41000",
			xff:        "9.9.9.9",
			cfIP:       "8.8.8.8",
			wantIP:     "203.0.113.9",
			wantSource: "direct",
		},
		{
			name:       "plain proxy chain takes first untrusted from right",
			remoteAddr: "127.0.0.1:41001",
			xff:        "203.0.113.7, 127.0.0.1",
			wantIP:     "203.0.113.7",
			wantSource: "xff",
		},
		{
			name:       "spoofed leftmost entry is never used",
			remoteAddr: "127.0.0.1:41002",
			xff:        "9.9.9.9, 203.0.113.7, 127.0.0.1",
			wantIP:     "203.0.113.7",
			wantSource: "xff",
		},
		{
			name:       "cloudflare chain prefers CF-Connecting-IP",
			remoteAddr: "127.0.0.1:41003",
			xff:        "9.9.9.9, 198.51.100.23, 172.64.32.10, 127.0.0.1",
			cfIP:       "198.51.100.23",
			wantIP:     "198.51.100.23",
			wantSource: "cloudflare",
		},
		{
			name:       "spoofed CF header without cloudflare hop is ignored",
			remoteAddr: "127.0.0.1:41004",
			xff:        "203.0.113.7, 127.0.0.1",
			cfIP:       "6.6.6.6",
			wantIP:     "203.0.113.7",
			wantSource: "xff",
		},
		{
			name:       "ipv6 visitor through cloudflare",
			remoteAddr: "127.0.0.1:41005",
			xff:        "2405:b500:1::1234, 2400:cb00::77, 127.0.0.1",
			cfIP:       "2405:b500:1::1234",
			wantIP:     "2405:b500:1::1234",
			wantSource: "cloudflare",
		},
		{
			name:       "ipv6 remote addr normalized",
			remoteAddr: "[::1]:41006",
			realIP:     "2001:0db8:0000:0000:0000:0000:0000:0001",
			wantIP:     "2001:db8::1",
			wantSource: "x-real-ip",
		},
		{
			name:       "x-real-ip is cloudflare edge uses CF header",
			remoteAddr: "127.0.0.1:41007",
			realIP:     "104.16.7.7",
			cfIP:       "198.51.100.50",
			wantIP:     "198.51.100.50",
			wantSource: "cloudflare",
		},
		{
			name:       "no headers falls back to remote",
			remoteAddr: "127.0.0.1:41008",
			wantIP:     "127.0.0.1",
			wantSource: "direct",
		},
		{
			name:       "ipv6 compressed canonical form",
			remoteAddr: "127.0.0.1:41009",
			xff:        "2001:0db8::0001, 127.0.0.1",
			wantIP:     "2001:db8::1",
			wantSource: "xff",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/visitor/conversations", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.cfIP != "" {
				req.Header.Set("CF-Connecting-IP", tc.cfIP)
			}
			if tc.realIP != "" {
				req.Header.Set("X-Real-IP", tc.realIP)
			}
			got := resolveClientIP(req, proxies)
			if got.IP != tc.wantIP {
				t.Fatalf("IP = %q, want %q", got.IP, tc.wantIP)
			}
			if got.Source != tc.wantSource {
				t.Fatalf("Source = %q, want %q", got.Source, tc.wantSource)
			}
		})
	}
}
