"""List tools: mark above / below, duplicates, reading time."""
import time
from lib import *


def row_menu(pg, nth):
    pg.locator("#pane-list .arow").nth(nth).click(button="right")
    pg.wait_for_selector(".menu button:has-text('Segna come letti quelli sopra')")


def mark_above_and_below(browser):
    pg = login(context(browser, DESKTOP))
    open_collection(pg, 0)  # Non letti, newest first
    pg.wait_for_timeout(500)
    row_menu(pg, 4)
    pg.click(".menu button:has-text('Segna come letti quelli sopra')")
    pg.wait_for_selector(".toast:has-text('4 segnati come letti')")
    check(unread() == 116, "the 4 articles above are read")
    check(pg.locator("#pane-list .arow.read").count() == 4, "and greyed in the list")
    check(sql("SELECT read FROM articles WHERE title = 'Articolo numero 5'")[0][0] == 0, "the article itself stays unread")
    pg.click(".toast-action")
    pg.wait_for_timeout(1200)
    check(unread() == 120, "Annulla brings them back")
    check(pg.locator("#pane-list .arow.read").count() == 0, "on screen too")
    row_menu(pg, 9)
    pg.click(".menu button:has-text('Segna come letti quelli sotto')")
    pg.wait_for_selector(".toast:has-text('110 segnati come letti')")
    check(unread() == 10, "below marks also the articles not loaded yet")
    pg.wait_for_timeout(800)
    check(home_counts(pg)["unread"] == 10, "the counters follow")
    check(not pg.errors, "no page errors")


def oldest_first(browser):
    pg = login(context(browser, DESKTOP))
    pg.evaluate("localStorage.setItem('prefs', JSON.stringify({...JSON.parse(localStorage.getItem('prefs')||'{}'), order:'old'}))")
    pg.reload()
    pg.wait_for_selector(".deck-card")
    open_collection(pg, 0)
    pg.wait_for_timeout(500)
    first = pg.inner_text("#pane-list .arow >> nth=0 >> .a-title")
    check(first == "Articolo numero 120", f"oldest first ({first})")
    row_menu(pg, 3)
    pg.click(".menu button:has-text('Segna come letti quelli sopra')")
    pg.wait_for_selector(".toast:has-text('3 segnati come letti')")
    read = [r[0] for r in sql("SELECT title FROM articles WHERE read = 1 ORDER BY id")]
    check(sorted(read) == sorted(["Articolo numero 120", "Articolo numero 119", "Articolo numero 118"]), f"above follows the order of the list ({read})")


def duplicates(browser):
    ids = dict(sql("SELECT title, id FROM feeds"))
    other = ids["Ricette"]
    orig = sql("SELECT id, feed_id, title FROM articles WHERE feed_id <> ? ORDER BY published_at DESC LIMIT 1 OFFSET 3", other)[0]
    now = int(time.time())
    with db() as c:
        c.execute("""INSERT INTO articles(feed_id, user_id, guid_hash, url, title, author, content, preview, thumbnail, published_at, fetched_at, read, dup_of)
                     SELECT ?, user_id, 'extra-dup', 'https://news.example/x', title || ' - Giornale', '', '', '', '', published_at + 1, ?, 1, id
                     FROM articles WHERE id = ?""", (other, now, orig[0]))
    dup = sql("SELECT id FROM articles WHERE guid_hash = 'extra-dup'")[0][0]
    pg = login(context(browser, IPAD))
    pg.evaluate("localStorage.setItem('prefs', JSON.stringify({...JSON.parse(localStorage.getItem('prefs')||'{}'), filter:'all'}))")
    pg.reload()
    pg.wait_for_selector(".deck-card")
    pg.click(f".feed-row[data-key='feed:{other}']")
    pg.wait_for_selector(f"#pane-list .arow[data-id='{dup}']")
    check(pg.locator(f"#pane-list .arow.read[data-id='{dup}']").count() == 1, "the copy is in its own feed, already read")
    before = unread()
    pg.click(f"#pane-list .arow[data-id='{dup}']")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.wait_for_timeout(800)
    check(sql("SELECT read FROM articles WHERE id = ?", orig[0])[0][0] == 0, "opening a read copy changes nothing")
    # Mark the copy unread then read: the first copy follows.
    sql("UPDATE articles SET read = 0 WHERE id = ?", dup)
    pg.reload()
    pg.wait_for_selector("#pane-reader .article-body")
    pg.wait_for_timeout(1000)
    check(sql("SELECT read FROM articles WHERE id = ?", orig[0])[0][0] == 1, "reading a copy reads the other one too")
    check(unread() == before - 1, "one unread less")
    b = login(context(browser, IPAD))
    b.evaluate("localStorage.setItem('prefs', JSON.stringify({...JSON.parse(localStorage.getItem('prefs')||'{}'), filter:'all'}))")
    b.reload()
    b.wait_for_selector(".deck-card")
    open_collection(b, 3)  # Tutti
    titles = b.evaluate("[...document.querySelectorAll('#pane-list .a-title')].map(e=>e.textContent)")
    check(orig[2] + " - Giornale" not in titles and orig[2] in titles, "Tutti shows the story once")


def reading_time(browser):
    sql("UPDATE articles SET words = 920 WHERE title = 'Articolo numero 1'")
    sql("UPDATE articles SET words = 100 WHERE title = 'Articolo numero 2'")
    pg = login(context(browser))
    open_collection(pg, 0)
    row1 = pg.locator("#pane-list .arow").nth(0)
    check(row1.locator(".a-time").inner_text().strip() == "4 min", "the row shows 4 min")
    check(pg.locator("#pane-list .arow").nth(1).locator(".a-time").count() == 0, "short articles show nothing")
    row1.click()
    pg.wait_for_selector("#pane-reader .article-body")
    check("4 min di lettura" in pg.inner_text("#pane-reader .reading-time"), "the reader shows it under the date")


if __name__ == "__main__":
    run(mark_above_and_below, oldest_first, duplicates, reading_time)
