# Contributing

Thanks for wanting to help. Bug reports, translations and pull requests are welcome; for larger changes,
open an issue first so we can agree on the approach.

## Layout

```
cmd/ebi-furai/       main, healthcheck, backup, reset-2fa
internal/adblock/    ad filter (EasyList)
internal/api/        router, JSON handlers, image proxy, static files
internal/auth/       argon2id, tokens, rate limit, two-step verification
internal/backup/     automatic database backups
internal/config/     environment variables
internal/content/    sanitizing, full text, image rewriting
internal/db/         SQLite and migrations (internal/db/migrations)
internal/dedup/      duplicates and reading time
internal/feeds/      scheduler, workers, background full text, discovery, icons, OPML
internal/filter/     keyword filters
internal/i18n/       server messages in Italian
internal/netsafe/    HTTP client with SSRF protection
internal/push/       Web Push (RFC 8291/8292) and badge
internal/store/      queries
web/                 the web app, embedded in the binary
tests/e2e/           interface tests (Playwright)
packaging/           systemd unit
scripts/             build helpers and the installer
site/                the project website (GitHub Pages)
```

## Building

- Go: the latest stable release. No CGO (SQLite is `modernc.org/sqlite`).
- `scripts/fetch-gsap.sh` downloads GSAP into `web/static/vendor/` (it is not in the repository).
- `go build ./cmd/ebi-furai`, then `DATA_DIR=./data ADMIN_USER=admin ADMIN_PASSWORD=change-me-now ./ebi-furai`.
- The front end is plain JavaScript modules, no bundler. `web/static/app.css` is compiled from
  `web/css/input.css` with Tailwind CSS v4 and committed: after changing classes or styles run
  `npx @tailwindcss/cli -i web/css/input.css -o web/static/app.css --minify` from `web/`.

## Tests

```sh
go vet ./... && go test ./...
pip install playwright && python -m playwright install chromium
tests/e2e/run.sh                     # all interface tests
tests/e2e/run.sh test_reading.py     # one file
```

The interface tests start a server on a temporary database with fake articles and drive Chromium as an iPhone,
an iPad, an iPhone Duo and a desktop. They never fetch real feeds.

## Translations

The code is written in English. The Italian lives in `web/static/js/i18n-it.js` (interface) and
`internal/i18n/it.go` (server messages), keyed by the English text. In the interface, `t('Text')` translates
and `t('{0} unread', n)` fills placeholders; a key may carry a context after `|` when one English word has two
translations (`t('Read|one')`). To add a language, add a catalog next to the Italian one and teach
`web/static/js/i18n.js` and `internal/i18n` to pick it.

## Style

- Go: `gofmt`, `go vet`, comments in English that say why, not what.
- JavaScript: ES modules, no framework, small functions. Every user action gets visible feedback, animated
  with GSAP where it helps.
- Keep dependencies few and up to date; nothing deprecated.

## License

By contributing you agree that your work is released under the AGPL-3.0 with the GSAP exception in
[NOTICE](NOTICE).
