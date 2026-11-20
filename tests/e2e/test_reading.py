"""Reading: mark all as read and undo, counters, empty states, preferences."""
import time
from lib import *


def mark_all_and_undo(browser):
    sql("UPDATE articles SET read = 1 WHERE id IN (SELECT id FROM articles ORDER BY id LIMIT 5)")
    before = unread()
    pg = login(context(browser))
    open_collection(pg, 0)
    pg.click("#pane-list button[aria-label='Segna tutto come letto']")
    pg.click(".confirm button:has-text('Segna come letti')")
    pg.wait_for_timeout(300)
    check(home_counts(pg)["unread"] == 0, "home counters at zero at once")
    pg.wait_for_selector("#pane-list .empty-koi", state="visible", timeout=8000)
    check(unread() == 0, "server: everything read")
    pg.click(".toast-action")
    pg.wait_for_timeout(2500)
    check(unread() == before, f"undo brings back exactly {before} articles (the 5 read before stay read)")
    check(pg.locator("#pane-list .arow:not(.read)").count() > 0, "the list shows them again")
    check(not pg.errors, "no page errors")


def counters_follow_server(browser):
    pg = login(context(browser))
    open_collection(pg, 3)
    pg.wait_for_timeout(16000)  # the home counters are re-read when older than 15 s
    fid, uid = sql("SELECT id, user_id FROM feeds LIMIT 1")[0]
    now = int(time.time())
    with db() as c:
        for i in range(3):
            c.execute("""INSERT INTO articles(feed_id, user_id, guid_hash, url, title, author, content, preview, thumbnail, published_at, fetched_at)
                         VALUES(?, ?, ?, '', ?, '', '', '', '', ?, ?)""", (fid, uid, f"extra-{now}-{i}", f"Nuovo {i}", now, now))
    before = unread()
    pg.click("#pane-list button[aria-label=Indietro]")
    pg.wait_for_timeout(1800)
    check(home_counts(pg)["unread"] == before, "articles fetched meanwhile show up back on the home")


def empty_states_aligned(browser):
    pg = login(context(browser, IPAD))
    open_collection(pg, 0)
    pg.click("#pane-list .arow >> nth=0")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.click("#pane-list button[aria-label='Segna tutto come letto']")
    pg.click(".confirm button:has-text('Segna come letti')")
    pg.wait_for_selector("#pane-list .empty-koi", state="visible", timeout=8000)
    pg.wait_for_timeout(1000)
    tops = pg.evaluate("[...document.querySelectorAll('.empty-title')].filter(e=>e.offsetParent).map(e=>Math.round(e.getBoundingClientRect().top))")
    check(len(tops) == 2, "list and reader both empty when everything is read")
    check(abs(tops[0] - tops[1]) <= 1, f"'Hai letto tutto' and 'Nessun articolo aperto' on the same line {tops}")


def preferences_per_device(browser):
    style = "(()=>{const s=getComputedStyle(document.documentElement);return [s.getPropertyValue('--reader-line').trim(), s.getPropertyValue('--list-scale').trim()]})()"
    a = login(context(browser))
    b = login(context(browser))
    a.click("button[aria-label=Impostazioni]")
    a.wait_for_timeout(700)
    a.click("button[data-v='1.85']")
    a.click("button[data-v='1.25']")
    a.wait_for_timeout(300)
    check(a.evaluate(style) == ["1.85", "1.25"], "device A: line spacing and list text changed")
    b.reload()
    b.wait_for_selector(".deck-card")
    check(b.evaluate(style) == ["1.65", "1"], "device B keeps its own settings")


if __name__ == "__main__":
    run(mark_all_and_undo, counters_follow_server, empty_states_aligned, preferences_per_device)
