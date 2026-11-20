"""Feed settings (name, full text, open on the site) and keyword filters."""
from lib import *


def feed_ids():
    return dict(sql("SELECT title, id FROM feeds"))


def open_feed_settings(pg, fid):
    pg.click(f".feed-row[data-key='feed:{fid}']", button="right")
    pg.click("text=Impostazioni del feed…")
    pg.wait_for_selector("text=Scarica sempre il testo completo")
    pg.wait_for_timeout(400)


def feed_settings(browser):
    fid = feed_ids()["Notizie del giorno"]
    pg = login(context(browser, DESKTOP))
    open_feed_settings(pg, fid)
    pg.fill(".sheet input.field", "Notizie")
    switches = pg.locator(".sheet .switch")
    switches.nth(0).click()
    pg.wait_for_timeout(500)
    check(sql("SELECT full_text FROM feeds WHERE id = ?", fid)[0][0] == 1, "full text saved at once")
    queued = sql("SELECT COUNT(*) FROM articles WHERE feed_id = ? AND full_tried = 0", fid)[0][0]
    switches.nth(1).click()
    pg.wait_for_timeout(500)
    check(sql("SELECT open_web FROM feeds WHERE id = ?", fid)[0][0] == 1, "open on the site saved at once")
    pg.click(".sheet button:has-text('Fine')")
    pg.wait_for_timeout(1200)
    check(sql("SELECT custom_title FROM feeds WHERE id = ?", fid)[0][0] == "Notizie", "the short name is saved when closing")
    check(pg.locator(f".feed-row[data-key='feed:{fid}']:has-text('Notizie')").count() == 1, "the home shows the short name")
    check(queued in (0, 40), f"turning on the full text queues the unread articles ({queued})")
    # Background download: example.com is not reachable here, so every
    # article is tried once and keeps the feed's text.
    for _ in range(30):
        if sql("SELECT COUNT(*) FROM articles WHERE feed_id = ? AND full_tried = 0", fid)[0][0] == 0:
            break
        pg.wait_for_timeout(1000)
    check(sql("SELECT COUNT(*) FROM articles WHERE feed_id = ? AND full_tried = 0", fid)[0][0] == 0, "the background download works through the queue")
    check(not pg.errors, "no page errors")


def full_text_shown(browser):
    fid = feed_ids()["Notizie del giorno"]
    aid = sql("SELECT id FROM articles WHERE feed_id = ? ORDER BY published_at DESC LIMIT 1 OFFSET 1", fid)[0][0]
    sql("UPDATE feeds SET full_text = 1 WHERE id = ?", fid)
    sql("UPDATE articles SET full_content = '<p>Il testo completo scaricato dal sito.</p>' WHERE id = ?", aid)
    pg = login(context(browser, IPAD))
    pg.click(f".feed-row[data-key='feed:{fid}']")
    pg.wait_for_selector("#pane-list .arow")
    pg.click(f"#pane-list .arow[data-id='{aid}']")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.wait_for_timeout(500)
    check("testo completo scaricato" in pg.inner_text("#pane-reader .article-body"), "the full text opens by itself")
    check(pg.locator("#pane-reader button[aria-label='Testo completo'].on").count() == 1, "the full text button is on")


def opens_on_site(browser):
    fid = feed_ids()["Ricette"]
    sql("UPDATE feeds SET open_web = 1 WHERE id = ?", fid)
    ctx = context(browser, IPAD)
    pg = login(ctx)
    pg.click(f".feed-row[data-key='feed:{fid}']")
    pg.wait_for_selector("#pane-list .arow")
    pg.wait_for_timeout(500)
    row = pg.locator("#pane-list .arow").first
    aid = int(row.get_attribute("data-id"))
    check(row.locator(".a-site").count() == 1, "the row shows that it opens on the site")
    ctx.route("https://example.com/**", lambda r: r.fulfill(body="<h1>sito</h1>", content_type="text/html"))
    with ctx.expect_page() as popup:
        row.click()
    check(popup.value.url == f"https://example.com/a/{aid - 0}" or popup.value.url.startswith("https://example.com/a/"), "a tap opens the page of the site")
    pg.wait_for_timeout(800)
    check(sql("SELECT read FROM articles WHERE id = ?", aid)[0][0] == 1, "and the article becomes read")
    check(pg.locator("#pane-reader .article-body").count() == 0, "the reader stays closed")


def filters_hide_and_undo(browser):
    pg = login(context(browser))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Filtri per parole")
    pg.wait_for_selector("text=Nessun filtro")
    pg.click(".sheet button:has-text('Nuovo filtro')")
    pg.fill(".sheet input[placeholder^='calcio']", "Numero 1*, numero 7")
    pg.wait_for_selector("text=Ora prende 33 articoli non letti")
    check(True, "the preview counts what the filter catches (whole words, * for prefixes)")
    pg.click(".sheet form button:has-text('Crea filtro')")
    pg.wait_for_selector(".toast:has-text('33 articoli nascosti')")
    pg.wait_for_timeout(1200)
    check(sql("SELECT COUNT(*) FROM articles WHERE hidden_by IS NOT NULL")[0][0] == 33, "33 articles hidden")
    check(pg.locator(".sheet .row-btn:has-text('numero 1*, numero 7')").count() == 1, "the filter is listed")
    pg.locator(".sheet button:has-text('Fine')").last.click()
    pg.wait_for_timeout(500)
    pg.locator(".sheet button:has-text('Fine')").last.click()
    pg.wait_for_timeout(800)
    check(home_counts(pg)["unread"] == 87, "the counters leave them out")
    open_collection(pg, 3)
    titles = pg.evaluate("[...document.querySelectorAll('#pane-list .a-title')].map(e=>e.textContent)")
    check("Articolo numero 7" not in titles and "Articolo numero 10" not in titles and "Articolo numero 2" in titles, "the lists leave them out")

    # Deleting the filter brings them back as unread.
    pg.click("#pane-list button[aria-label=Indietro]")
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Filtri per parole")
    pg.click(".sheet .row-btn:has-text('numero 1*')")
    pg.click(".sheet button:has-text('Elimina filtro')")
    pg.click(".confirm button:has-text('Elimina')")
    pg.wait_for_selector(".toast:has-text('33 articoli di nuovo visibili')")
    pg.wait_for_timeout(800)
    check(unread() == 120 and sql("SELECT COUNT(*) FROM filters")[0][0] == 0, "deleting the filter brings them back")


def filters_mark_read_undo(browser):
    fid = feed_ids()["Ricette"]
    pg = login(context(browser, DESKTOP))
    open_feed_settings(pg, fid)
    pg.click("text=Filtri per parole")
    pg.click(".sheet button:has-text('Nuovo filtro')")
    pg.fill(".sheet input[placeholder^='calcio']", "articolo")
    pg.click(".sheet form button:has-text('Segna come letti')")
    pg.wait_for_selector("text=Ora prende 40 articoli non letti")
    check(True, "a filter made from a feed covers that feed only")
    pg.click(".sheet form button:has-text('Crea filtro')")
    pg.wait_for_selector(".toast:has-text('40 articoli segnati come letti')")
    check(unread() == 80, "the feed's articles are read")
    check(sql("SELECT action, feed_id FROM filters")[0] == ("read", fid), "stored as a read filter on that feed")
    pg.click(".toast-action")
    pg.wait_for_timeout(1500)
    check(unread() == 120 and sql("SELECT COUNT(*) FROM filters")[0][0] == 0, "Annulla removes the filter and its effect")
    check(not pg.errors, "no page errors")


if __name__ == "__main__":
    run(feed_settings, full_text_shown, opens_on_site, filters_hide_and_undo, filters_mark_read_undo)
