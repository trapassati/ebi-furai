package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trapassati/ebi-furai/internal/db"
)

func TestKeep(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 11, 20, 3, 0, 0, 0, loc) // a Friday
	var list []Info
	// Two backups a day for 60 days, newest first.
	for d := 0; d < 60; d++ {
		for _, h := range []int{15, 3} {
			tm := start.AddDate(0, 0, -d).Add(time.Duration(h-3) * time.Hour)
			list = append(list, Info{Name: fmt.Sprintf("b-%02d-%02d", d, h), Time: tm})
		}
	}
	keep := Keep(list, 7, 4, loc)
	if len(keep) != 11 {
		t.Fatalf("kept %d backups, want 11: %v", len(keep), keep)
	}
	for d := 0; d < 7; d++ {
		if !keep[fmt.Sprintf("b-%02d-15", d)] {
			t.Errorf("day %d: newest backup not kept", d)
		}
		if keep[fmt.Sprintf("b-%02d-03", d)] {
			t.Errorf("day %d: older backup of the same day kept", d)
		}
	}
}

func TestCreateListOpen(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`CREATE TABLE t(v TEXT); INSERT INTO t VALUES('ciao')`); err != nil {
		t.Fatal(err)
	}
	m := NewManager(conn, filepath.Join(dir, "backups"), 7, 4, time.UTC)
	info, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size == 0 || !nameRE.MatchString(info.Name) {
		t.Fatalf("bad backup %+v", info)
	}
	list, _ := m.List()
	if len(list) != 1 || list[0].Name != info.Name {
		t.Fatalf("list = %+v", list)
	}
	f, _, err := m.Open(info.Name)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, _, err := m.Open("../x.db"); err != ErrNotFound {
		t.Fatalf("path traversal not refused: %v", err)
	}
	if m.due() {
		t.Fatal("a fresh backup should not be due")
	}
	left, _ := os.ReadDir(m.Dir())
	if len(left) != 1 {
		t.Fatalf("temporary files left: %v", left)
	}
}
