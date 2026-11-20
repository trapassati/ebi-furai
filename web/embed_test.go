package web

import (
	"bytes"
	"os"
	"testing"
)

// The license texts shown in the app are the same as the files in the
// project root.
func TestLicenseCopy(t *testing.T) {
	for root, app := range map[string]string{
		"../LICENSE": "static/legal/AGPL-3.0.txt",
		"../NOTICE":  "static/legal/NOTICE.txt",
	} {
		a, err := os.ReadFile(root)
		if err != nil {
			t.Fatal(err)
		}
		b, err := FS.ReadFile(app)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("web/%s differs from %s", app, root)
		}
	}
}
