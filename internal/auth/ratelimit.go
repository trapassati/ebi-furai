package auth

import (
	"strings"
	"sync"
	"time"
)

// LoginLimiter counts failed login attempts per key (IP and username) in a
// sliding window. It lives in memory: the app runs as a single container.
type LoginLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	maxIP   int
	maxUser int
	hits    map[string][]time.Time
}

func NewLoginLimiter() *LoginLimiter {
	l := &LoginLimiter{
		window:  15 * time.Minute,
		maxIP:   10,
		maxUser: 20,
		hits:    map[string][]time.Time{},
	}
	go l.janitor()
	return l
}

func ipKey(ip string) string     { return "ip:" + ip }
func userKey(user string) string { return "user:" + strings.ToLower(user) }

// Allowed reports whether a new attempt from ip for user may proceed.
func (l *LoginLimiter) Allowed(ip, user string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	return l.count(ipKey(ip), now) < l.maxIP && l.count(userKey(user), now) < l.maxUser
}

// Fail records a failed attempt.
func (l *LoginLimiter) Fail(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.hits[ipKey(ip)] = append(l.hits[ipKey(ip)], now)
	l.hits[userKey(user)] = append(l.hits[userKey(user)], now)
}

// Success clears the counters of the IP after a valid login.
func (l *LoginLimiter) Success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, ipKey(ip))
}

func (l *LoginLimiter) count(key string, now time.Time) int {
	list := l.hits[key]
	cut := 0
	for cut < len(list) && now.Sub(list[cut]) > l.window {
		cut++
	}
	if cut > 0 {
		list = list[cut:]
		if len(list) == 0 {
			delete(l.hits, key)
		} else {
			l.hits[key] = list
		}
	}
	return len(list)
}

func (l *LoginLimiter) janitor() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		now := time.Now()
		for k := range l.hits {
			l.count(k, now)
		}
		l.mu.Unlock()
	}
}
