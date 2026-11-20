package push

import (
	"testing"
	"time"
)

func TestDueSlot(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Rome")
	at := func(h, m int) time.Time { return time.Date(2026, 11, 14, h, m, 0, 0, loc) }
	times := []string{"20:00", "08:00", "13:00"}
	cases := []struct {
		now  time.Time
		last string
		want string
	}{
		{at(7, 59), "", ""},
		{at(8, 0), "", "2026-11-14 08:00"},
		{at(8, 10), "2026-11-14 08:00", ""},
		{at(8, 29), "2026-11-13 20:00", "2026-11-14 08:00"},
		{at(8, 30), "", ""}, // missed for more than the catch-up window
		{at(13, 5), "2026-11-14 08:00", "2026-11-14 13:00"},
		{at(23, 0), "", ""},
	}
	for _, c := range cases {
		if got, _ := DueSlot(times, c.last, c.now); got != c.want {
			t.Errorf("DueSlot(%s, %q) = %q, want %q", c.now.Format("15:04"), c.last, got, c.want)
		}
	}
}

func TestInQuiet(t *testing.T) {
	cases := []struct {
		t, s, e string
		want    bool
	}{
		{"23:00", "22:00", "07:00", true},
		{"06:59", "22:00", "07:00", true},
		{"07:00", "22:00", "07:00", false},
		{"13:00", "22:00", "07:00", false},
		{"13:30", "13:00", "14:00", true},
		{"14:00", "13:00", "14:00", false},
		{"10:00", "10:00", "10:00", false},
	}
	for _, c := range cases {
		if got := InQuiet(c.t, c.s, c.e); got != c.want {
			t.Errorf("InQuiet(%s, %s-%s) = %v", c.t, c.s, c.e, got)
		}
	}
}
