package store

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/trapassati/ebi-furai/internal/db"
)

func TestTOTPStore(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	uid, _ := res.LastInsertId()
	secret := []byte("12345678901234567890")
	if err := st.SetTOTPPending(uid, secret); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	conn.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, uid).Scan(&raw)
	if bytes.Contains(raw, secret) {
		t.Fatal("secret stored in clear")
	}
	if err := st.EnableTOTP(uid, 100, []string{"aaaa-bbbb", "cccc-dddd"}); err != nil {
		t.Fatal(err)
	}
	s, err := st.TOTP(uid)
	if err != nil || !s.Enabled || !bytes.Equal(s.Secret, secret) || s.RecoveryCodes != 2 {
		t.Fatalf("state %+v %v", s, err)
	}
	if err := st.SetTOTPPending(uid, secret); err == nil {
		t.Fatal("a new secret replaced an active one")
	}
	if st.UseTOTPStep(uid, 100) || !st.UseTOTPStep(uid, 101) || st.UseTOTPStep(uid, 101) {
		t.Fatal("a code was accepted twice")
	}
	if !st.UseRecoveryCode(uid, "aaaa-bbbb") || st.UseRecoveryCode(uid, "aaaa-bbbb") {
		t.Fatal("recovery code not single use")
	}
	if err := st.DisableTOTP(uid); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.TOTP(uid); s.Enabled || s.Secret != nil || s.RecoveryCodes != 0 {
		t.Fatalf("not cleared: %+v", s)
	}
}
