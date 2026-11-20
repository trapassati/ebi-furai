package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// decrypt is an independent receiver-side implementation of RFC 8291.
func decrypt(t *testing.T, uaPriv *ecdh.PrivateKey, auth, body []byte) []byte {
	t.Helper()
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idlen := int(body[20])
	asPubBytes := body[21 : 21+idlen]
	ct := body[21+idlen:]
	if rs != recordSize {
		t.Fatalf("record size %d", rs)
	}
	asPub, err := ecdh.P256().NewPublicKey(asPubBytes)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := uaPriv.ECDH(asPub)
	prkKey, _ := hkdf.Extract(sha256.New, shared, auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(uaPriv.PublicKey().Bytes())+string(asPubBytes), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatal("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func TestEncryptRoundTrip(t *testing.T) {
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	sub := Subscription{Endpoint: "https://web.push.apple.com/x", P256dh: b64.EncodeToString(uaPriv.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}
	payload := []byte(`{"title":"Reader","body":"42 articoli da leggere"}`)
	body, err := Encrypt(sub, payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := decrypt(t, uaPriv, auth, body); string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	// Export a vector for the cross-check with an independent library.
	if out := os.Getenv("PUSH_VECTOR"); out != "" {
		v, _, _ := GenerateVAPID()
		authz, _ := v.authorization(sub.Endpoint, "https://reader.example.org", time.Now())
		data, _ := json.Marshal(map[string]string{
			"ua_private": b64.EncodeToString(uaPriv.Bytes()),
			"auth":       sub.Auth,
			"body":       b64.EncodeToString(body),
			"payload":    string(payload),
			"jwt":        strings.TrimPrefix(strings.SplitN(authz, ",", 2)[0], "vapid t="),
			"vapid_pub":  v.PublicKey,
		})
		os.WriteFile(out, data, 0o600)
	}
}

func TestVAPIDRoundTrip(t *testing.T) {
	v, private, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := LoadVAPID(private)
	if err != nil || v2.PublicKey != v.PublicKey {
		t.Fatalf("reload mismatch: %v", err)
	}
	pub, _ := decodeB64(v.PublicKey)
	if len(pub) != 65 || pub[0] != 4 {
		t.Fatal("public key must be an uncompressed P-256 point")
	}
}

func TestValidEndpoint(t *testing.T) {
	ok := []string{"https://web.push.apple.com/QAbc", "https://fcm.googleapis.com/fcm/send/x", "https://updates.push.services.mozilla.com/wpush/v2/x", "https://wns2-db5p.notify.windows.com/w/?token=x"}
	bad := []string{"http://web.push.apple.com/x", "https://evil.com/x", "https://web.push.apple.com.evil.com/x", "https://127.0.0.1/x", "https://user:p@fcm.googleapis.com/x"}
	for _, s := range ok {
		if !ValidEndpoint(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	for _, s := range bad {
		if ValidEndpoint(s) {
			t.Errorf("%s should be rejected", s)
		}
	}
}
