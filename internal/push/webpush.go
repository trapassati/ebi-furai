// Package push implements Web Push (RFC 8030) with VAPID authentication
// (RFC 8292) and aes128gcm message encryption (RFC 8291), using only the
// Go standard library.
package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var b64 = base64.RawURLEncoding

// decodeB64 accepts both padded/unpadded and URL/standard base64.
func decodeB64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return b64.DecodeString(s)
}

// VAPID holds the application server key pair.
type VAPID struct {
	priv      *ecdsa.PrivateKey
	PublicKey string // base64url, uncompressed P-256 point (65 bytes)
}

// GenerateVAPID creates a new key pair. It returns the private key encoded
// as base64url of the raw 32-byte scalar, for storage.
func GenerateVAPID() (*VAPID, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	raw, err := priv.Bytes()
	if err != nil {
		return nil, "", err
	}
	v, err := LoadVAPID(b64.EncodeToString(raw))
	return v, b64.EncodeToString(raw), err
}

// LoadVAPID restores a key pair from the stored private key.
func LoadVAPID(private string) (*VAPID, error) {
	raw, err := decodeB64(private)
	if err != nil {
		return nil, err
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, err
	}
	pub, err := priv.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return &VAPID{priv: priv, PublicKey: b64.EncodeToString(pub)}, nil
}

// authorization builds the "vapid t=..., k=..." header for an endpoint.
func (v *VAPID) authorization(endpoint, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": subject,
	})
	signingInput := b64.EncodeToString(header) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	der, err := ecdsa.SignASN1(rand.Reader, v.priv, digest[:])
	if err != nil {
		return "", err
	}
	// JWS wants the raw R||S form, 32 bytes each.
	var sig struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &sig); err != nil {
		return "", err
	}
	raw := make([]byte, 64)
	sig.R.FillBytes(raw[:32])
	sig.S.FillBytes(raw[32:])
	return "vapid t=" + signingInput + "." + b64.EncodeToString(raw) + ", k=" + v.PublicKey, nil
}

// Subscription is a browser PushSubscription.
type Subscription struct {
	Endpoint string
	P256dh   string // base64url, receiver public key
	Auth     string // base64url, 16-byte auth secret
}

const recordSize = 4096

// Encrypt encrypts a payload for a subscription (RFC 8291, aes128gcm).
func Encrypt(sub Subscription, payload []byte) ([]byte, error) {
	return encrypt(sub, payload, rand.Reader)
}

func encrypt(sub Subscription, payload []byte, rnd io.Reader) ([]byte, error) {
	uaPubBytes, err := decodeB64(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	authSecret, err := decodeB64(sub.Auth)
	if err != nil || len(authSecret) != 16 {
		return nil, errors.New("invalid auth secret")
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	asPriv, err := ecdh.P256().GenerateKey(rnd)
	if err != nil {
		return nil, err
	}
	asPub := asPriv.PublicKey().Bytes()
	shared, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}

	// IKM = HKDF(auth_secret, ecdh_secret, "WebPush: info" || 0 || ua_public || as_public, 32)
	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPubBytes) + string(asPub)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rnd, salt); err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	// Single record: payload followed by the 0x02 "last record" delimiter.
	if len(payload)+1+16 > recordSize {
		return nil, errors.New("payload too large")
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain := append(append([]byte{}, payload...), 0x02)
	ciphertext := gcm.Seal(nil, nonce, plain, nil)

	// Header: salt(16) || rs(4) || idlen(1) || keyid(as_public)
	var buf bytes.Buffer
	buf.Write(salt)
	binary.Write(&buf, binary.BigEndian, uint32(recordSize))
	buf.WriteByte(byte(len(asPub)))
	buf.Write(asPub)
	buf.Write(ciphertext)
	return buf.Bytes(), nil
}

// Message describes one push request.
type Message struct {
	Payload []byte
	TTL     time.Duration
	Topic   string // collapses pending messages with the same topic
	Urgency string // very-low, low, normal, high
}

// SendError reports the push service's answer.
type SendError struct {
	Status int
	Body   string
}

func (e *SendError) Error() string {
	return fmt.Sprintf("push service HTTP %d: %s", e.Status, e.Body)
}

// Gone reports whether the subscription no longer exists and must be deleted.
func (e *SendError) Gone() bool {
	return e.Status == http.StatusNotFound || e.Status == http.StatusGone
}

// Send encrypts and delivers a message.
func (v *VAPID) Send(ctx context.Context, client *http.Client, sub Subscription, subject string, msg Message) error {
	body, err := Encrypt(sub, msg.Payload)
	if err != nil {
		return err
	}
	auth, err := v.authorization(sub.Endpoint, subject, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	ttl := max(int(msg.TTL.Seconds()), 0)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(ttl))
	if msg.Urgency != "" {
		req.Header.Set("Urgency", msg.Urgency)
	}
	if msg.Topic != "" {
		req.Header.Set("Topic", msg.Topic)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &SendError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
}

// Known push services. Endpoints come from the browser, so only these
// hosts are contacted.
var allowedHosts = []string{
	"web.push.apple.com",        // Safari, iOS/iPadOS
	"fcm.googleapis.com",        // Chrome, Edge on Android
	"push.services.mozilla.com", // Firefox
	"notify.windows.com",        // Edge on Windows
	"android.googleapis.com",    // legacy Chrome endpoints
	"updates.push.services.mozilla.com",
}

// ValidEndpoint checks that an endpoint is https and belongs to a known
// push service.
func ValidEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || len(endpoint) > 1024 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range allowedHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}
