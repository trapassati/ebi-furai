package netsafe

import (
	"net/netip"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1",
		"0.0.0.0", "::1", "fe80::1", "fd00::1", "::ffff:127.0.0.1", "::ffff:192.168.1.1", "224.0.0.1"}
	for _, s := range blocked {
		if IsPublicIP(netip.MustParseAddr(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	allowed := []string{"1.1.1.1", "151.101.1.69", "2606:4700:4700::1111"}
	for _, s := range allowed {
		if !IsPublicIP(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestValidateURL(t *testing.T) {
	bad := []string{"ftp://x.org/feed", "file:///etc/passwd", "http://127.0.0.1/", "https://user:pw@x.org/", "http:///nohost"}
	for _, s := range bad {
		if _, err := ValidateURL(s); err == nil {
			t.Errorf("%s should be rejected", s)
		}
	}
	if _, err := ValidateURL("https://example.org/feed.xml"); err != nil {
		t.Error(err)
	}
}
