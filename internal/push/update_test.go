package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/store"
)

// A new version is told to the administrators' devices, in their
// language, once; other users never get it.
func TestNotifyUpdate(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "reader.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	svc, err := NewService(st, &config.Config{AppName: "Ebi Furai"})
	if err != nil {
		t.Fatal(err)
	}
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	var mu sync.Mutex
	got := map[string][]byte{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = decrypt(t, uaPriv, auth, body)
		mu.Unlock()
		if r.Header.Get("Topic") != "update" {
			t.Errorf("topic %q", r.Header.Get("Topic"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	svc.client = srv.Client()

	admin, _ := st.CreateUser("admin", "x", true)
	user, _ := st.CreateUser("reader", "x", false)
	st.SetUserLang(admin.ID, "it")
	key := b64.EncodeToString(uaPriv.PublicKey().Bytes())
	st.SavePushSubscription(admin.ID, srv.URL+"/admin", key, b64.EncodeToString(auth), "iPhone")
	st.SavePushSubscription(user.ID, srv.URL+"/reader", key, b64.EncodeToString(auth), "iPhone")

	n, err := svc.NotifyUpdate(context.Background(), "9.0.0")
	if err != nil || n != 1 {
		t.Fatalf("sent %d, %v", n, err)
	}
	if _, ok := got["/reader"]; ok {
		t.Fatal("a user who is not an administrator was notified")
	}
	var p map[string]any
	json.Unmarshal(got["/admin"], &p)
	if !strings.Contains(p["body"].(string), "9.0.0") || !strings.Contains(p["body"].(string), "disponibile") || p["url"] != "/#/update" {
		t.Fatalf("payload %v", p)
	}
	if _, ok := p["badge"]; ok {
		t.Fatal("the update notification changes the badge of the app icon")
	}
	if n, _ := svc.NotifyUpdate(context.Background(), "9.0.0"); n != 0 {
		t.Fatal("the same version was notified twice")
	}
}
