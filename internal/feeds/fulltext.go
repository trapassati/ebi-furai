package feeds

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/dedup"
)

const (
	fullTextWorkers = 2
	fullTextBatch   = 20
	fullTextMaxAge  = 3 * 24 * time.Hour // older articles are not worth it
	fullTextIdle    = 5 * time.Minute
)

// runFullText downloads, in background, the full text of the new articles
// of feeds with "Scarica sempre il testo completo": they are ready when
// opened, also offline. The queue lives in the database (full_tried), so
// nothing is lost on restart. A failed page is not retried: the reader
// keeps the feed's text and the button can still ask again.
func (f *Fetcher) runFullText(ctx context.Context) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-f.fullWake:
		}
		// Keep going while batches come back full (a bounded number of
		// rounds, so a stuck queue cannot spin).
		for range 50 {
			if ctx.Err() != nil || f.fullTextBatch(ctx) < fullTextBatch {
				break
			}
		}
		timer.Reset(fullTextIdle)
	}
}

// fullTextBatch handles up to fullTextBatch articles and returns how many.
func (f *Fetcher) fullTextBatch(ctx context.Context) int {
	since := time.Now().Add(-fullTextMaxAge).Unix()
	if err := f.store.ExpireFullTexts(since); err != nil {
		slog.Error("full text expire", "err", err)
	}
	list, err := f.store.PendingFullTexts(since, fullTextBatch)
	if err != nil {
		slog.Error("full text queue", "err", err)
		return 0
	}
	sem := make(chan struct{}, fullTextWorkers)
	var wg sync.WaitGroup
	for _, p := range list {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			html, err := content.FetchFullText(ctx, f.client, p.URL)
			if ctx.Err() != nil {
				return // shutting down: it stays in the queue
			}
			if err != nil {
				slog.Debug("full text failed", "article", p.ID, "url", p.URL, "err", err)
			}
			if err := f.store.FullTextDone(p.ID, html, dedup.Words(content.PlainText(html))); err != nil {
				slog.Error("full text save", "article", p.ID, "err", err)
			}
		})
	}
	wg.Wait()
	return len(list)
}

// countOld gives words (for the reading time) and duplicate keys to the
// articles stored before they existed, a batch at a time, once.
func (f *Fetcher) countOld(ctx context.Context) {
	total := 0
	for ctx.Err() == nil {
		list, err := f.store.UncountedArticles(200)
		if err != nil {
			slog.Error("count words", "err", err)
			return
		}
		if len(list) == 0 {
			break
		}
		for _, a := range list {
			words := max(dedup.Words(content.PlainText(a.Content)), dedup.Words(content.PlainText(a.FullText)))
			tk, tk2 := dedup.TitleKeys(a.Title)
			if err := f.store.SetArticleCounts(a.ID, words, dedup.URLKey(a.URL), tk, tk2); err != nil {
				slog.Error("count words", "err", err)
				return
			}
		}
		total += len(list)
		time.Sleep(50 * time.Millisecond) // leave room to the app
	}
	if total > 0 {
		slog.Info("reading time counted", "articles", total)
	}
}
