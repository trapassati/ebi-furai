package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords (RFC 6238, as used by authenticator apps
// and the iOS Passwords app): HMAC-SHA1, 30-second steps, 6 digits.

const totpPeriod = 30

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret.
func NewTOTPSecret() []byte {
	b := make([]byte, 20)
	rand.Read(b)
	return b
}

// TOTPSecretText is the secret as people type it: base32, in groups of 4.
func TOTPSecretText(secret []byte) string {
	s := b32.EncodeToString(secret)
	var parts []string
	for len(s) > 4 {
		parts = append(parts, s[:4])
		s = s[4:]
	}
	return strings.Join(append(parts, s), " ")
}

// TOTPURI is the otpauth:// link (and QR code content) for the apps.
func TOTPURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", b32.EncodeToString(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// TOTPCode is the code of a time step.
func TOTPCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// TOTPStep is the time step of t.
func TOTPStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// VerifyTOTP checks a code against the current step and the ones next to
// it (clocks drift, people type slowly). It returns the matching step, so
// that the caller can refuse a code already used.
func VerifyTOTP(secret []byte, code string, t time.Time) (int64, bool) {
	code = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
	if len(code) != 6 {
		return 0, false
	}
	now := TOTPStep(t)
	for _, step := range []int64{now, now - 1, now + 1} {
		if subtle.ConstantTimeCompare([]byte(TOTPCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// Recovery codes: easy to read and type (no 0/o, 1/l/i), xxxx-xxxx.
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewRecoveryCodes returns n one-time codes.
func NewRecoveryCodes(n int) []string {
	codes := make([]string, n)
	for i := range codes {
		b := make([]byte, 8)
		rand.Read(b)
		var sb strings.Builder
		for j, c := range b {
			if j == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
		}
		codes[i] = sb.String()
	}
	return codes
}

// NormalizeRecoveryCode makes "ABCD EFGH" and "abcd-efgh" the same code.
func NormalizeRecoveryCode(s string) string {
	s = strings.ToLower(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, s))
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

// SetupCode is the one-time code that lets the owner create the first
// administrator from the app: 8 characters, no look-alikes (O/0, I/1).
func SetupCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	rand.Read(b)
	for i, c := range b {
		b[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(b)
}

// FormatSetupCode writes a setup code as "ABCD-EFGH", easier to copy.
func FormatSetupCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}
