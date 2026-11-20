package db

import (
	"net/url"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO), FTS5 included
)

const driverName = "sqlite"

// dsn builds the modernc.org/sqlite connection string. Pragmas are applied
// to every new connection of the pool.
func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "temp_store(MEMORY)")
	q.Add("_pragma", "cache_size(-8000)") // 8 MB page cache per connection
	q.Set("_txlock", "immediate")
	return "file:" + path + "?" + q.Encode()
}
