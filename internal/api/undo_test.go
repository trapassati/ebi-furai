package api

import "testing"

func TestUndoStore(t *testing.T) {
	var u undoStore
	tok := u.put(1, []int64{5, 6})
	if _, ok := u.take(2, tok); ok {
		t.Fatal("another user took the undo")
	}
	if ids, ok := u.take(1, tok); !ok || len(ids) != 2 {
		t.Fatalf("got %v %v", ids, ok)
	}
	if _, ok := u.take(1, tok); ok {
		t.Fatal("an undo was used twice")
	}
}
