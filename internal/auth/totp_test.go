package auth

import (
	"testing"
	"time"
)

// RFC 6238, appendix B (SHA-1, 8 digits there; the last 6 digits here).
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		unix int64
		want string
	}{
		{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"},
		{1234567890, "005924"}, {2000000000, "279037"},
	} {
		if got := TOTPCode(secret, c.unix/30); got != c.want {
			t.Errorf("t=%d: got %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestVerifyTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	step := TOTPStep(now)
	if s, ok := VerifyTOTP(secret, TOTPCode(secret, step-1), now); !ok || s != step-1 {
		t.Fatal("previous step refused")
	}
	if _, ok := VerifyTOTP(secret, "123 456", now); ok && TOTPCode(secret, step) != "123456" {
		t.Fatal("wrong code accepted")
	}
	if _, ok := VerifyTOTP(secret, TOTPCode(secret, step-3), now); ok {
		t.Fatal("old code accepted")
	}
	if NormalizeRecoveryCode("ABCD EFGH") != "abcd-efgh" || NormalizeRecoveryCode("abc") != "" {
		t.Fatal("recovery code normalization")
	}
}
