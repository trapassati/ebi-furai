module github.com/trapassati/ebi-furai

go 1.27.0

require (
	codeberg.org/readeck/go-readability/v2 v2.1.3
	github.com/go-chi/chi/v5 v5.5.0
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/mmcdole/gofeed v1.5.0
	golang.org/x/crypto v0.59.0
	golang.org/x/net v0.62.0
	golang.org/x/text v0.44.0
	modernc.org/sqlite v1.62.0
)

// Indirect dependencies raised to their latest releases (go mod tidy alone
// would keep the older minimums required by the libraries above).
require (
	github.com/andybalholm/cascadia v1.3.5 // indirect
	golang.org/x/sys v0.50.0 // indirect
)
