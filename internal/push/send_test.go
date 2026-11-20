package push

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSend checks the request a push service receives: headers, a valid
// VAPID token signed by our key, and a payload the browser can decrypt.
func TestSend(t *testing.T) {
	v, _, _ := GenerateVAPID()
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)

	var got []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") != "3600" || r.Header.Get("Topic") != "digest" || r.Header.Get("Urgency") != "normal" {
			t.Errorf("bad headers: %v", r.Header)
		}
		a := r.Header.Get("Authorization")
		tok, k, ok := strings.Cut(strings.TrimPrefix(a, "vapid t="), ", k=")
		if !ok || k != v.PublicKey {
			t.Fatalf("bad authorization %q", a)
		}
		parts := strings.Split(tok, ".")
		var claims map[string]any
		cb, _ := b64.DecodeString(parts[1])
		json.Unmarshal(cb, &claims)
		if claims["aud"] != "https://"+r.Host || claims["sub"] != "https://reader.example.org" {
			t.Errorf("claims %v", claims)
		}
		sig, _ := b64.DecodeString(parts[2])
		pubBytes, _ := b64.DecodeString(k)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pubBytes)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			t.Error("VAPID signature does not verify")
		}
		body, _ := io.ReadAll(r.Body)
		got = decrypt(t, uaPriv, auth, body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	sub := Subscription{Endpoint: srv.URL + "/push/abc", P256dh: b64.EncodeToString(uaPriv.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}
	msg := Message{Payload: []byte(`{"title":"t"}`), TTL: time.Hour, Topic: "digest", Urgency: "normal"}
	if err := v.Send(context.Background(), srv.Client(), sub, "https://reader.example.org", msg); err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"title":"t"}` {
		t.Fatalf("payload %q", got)
	}

	gone := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGone) }))
	defer gone.Close()
	sub.Endpoint = gone.URL
	err := v.Send(context.Background(), gone.Client(), sub, "https://reader.example.org", msg)
	if se, ok := err.(*SendError); !ok || !se.Gone() {
		t.Fatalf("want gone error, got %v", err)
	}
}
