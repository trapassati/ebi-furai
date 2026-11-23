"""Read later: save a page from the app, from a Shortcut (app password) and
from the bookmarklet; its own collection; remove a page."""
from lib import *

# The test server cannot reach the web: the pages are kept as links, with
# the reason they could not be read (the address checks refuse 127.0.0.1).
PAGE = "http://127.0.0.1:9/una-pagina"


def saved_count(pg):
    return pg.evaluate("(() => { const e = document.querySelector('#pane-sources .deck-num[data-src=saved]'); return e ? Number(e.textContent) : null })()")


def save_for_later(browser):
    pg = login(context(browser))
    check(pg.locator(".deck-card[data-sel=saved]").count() == 0, "no Read later card before saving anything")
    unread = home_counts(pg)["unread"]

    # From the app: + → Save a page for later.
    pg.click("button[aria-label=Aggiungi]")
    pg.click(".sheet button:has-text('Salva una pagina per dopo')")
    pg.fill(".sheet input[name=url]", PAGE)
    pg.click(".sheet button:has-text('Salva')")
    pg.wait_for_selector(".toast:has-text('Salvata, ma non è stato possibile leggere la pagina: indirizzo non consentito')")
    pg.wait_for_selector("#pane-list .arow")
    check(pg.inner_text("#pane-list .arow >> nth=0").count("127.0.0.1:9/una-pagina") == 1, "the page is in Read later, named after its address")
    check("Da leggere" in pg.inner_text("#pane-list"), "the list is Read later")
    pg.click("#pane-list button[aria-label=Indietro]")
    pg.wait_for_timeout(800)
    check(saved_count(pg) == 1, f"the card shows 1 page to read ({saved_count(pg)})")
    check(home_counts(pg)["unread"] == unread, "Unread does not count it")

    # From a Shortcut: an app password as a bearer token.
    secret = pg.evaluate("""import('/static/js/api.js').then(m => m.post('/api/me/app-passwords', {name: 'Shortcut', password: 'e2e-password-123'})).then(r => r.secret)""")
    status = pg.evaluate("""s => fetch('/api/save', {method: 'POST', credentials: 'omit', headers: {'Authorization': 'Bearer ' + s, 'Content-Type': 'application/json'},
        body: JSON.stringify({url: 'Un titolo http://127.0.0.1:9/altra'})}).then(async r => [r.status, await r.json()])""", secret)
    check(status[0] == 200 and status[1]["saved"] and status[1]["title"] == "127.0.0.1:9/altra", f"saved with the app password ({status})")
    wrong = pg.evaluate("""fetch('/api/save', {method: 'POST', credentials: 'omit', headers: {'Authorization': 'Bearer nope', 'Content-Type': 'text/plain'},
        body: 'http://127.0.0.1:9/x'}).then(r => r.status)""")
    check(wrong == 401, f"a wrong app password is refused ({wrong})")
    bad = pg.evaluate("""s => fetch('/api/save', {method: 'POST', credentials: 'omit', headers: {'Authorization': 'Bearer ' + s, 'Content-Type': 'text/plain'},
        body: 'not a link'}).then(r => r.status)""", secret)
    check(bad == 400, f"text that is not an address is refused ({bad})")

    # From the bookmarklet: the app opens at #/save?url=…
    pg.goto(URL + "/#/save?url=" + "http%3A%2F%2F127.0.0.1%3A9%2Fterza")
    pg.wait_for_selector(".toast:has-text('Salvata')")
    for _ in range(40):  # the list reloads after the save
        if pg.locator("#pane-list .arow").count() == 3:
            break
        pg.wait_for_timeout(200)
    rows = pg.locator("#pane-list .arow").count()
    check(rows == 3, f"three pages in Read later ({rows})")
    check(not pg.url.count("save?url"), f"the address is clean again ({pg.url})")

    # Reading one: the count goes down, the article stays in its feed only.
    pg.click("#pane-list .arow >> nth=0")
    pg.wait_for_selector(".reader-title")
    pg.wait_for_timeout(600)
    pg.click("#pane-reader button[aria-label=Indietro]")
    pg.wait_for_timeout(600)
    pg.click("#pane-list button[aria-label=Indietro]")
    pg.wait_for_timeout(800)
    check(saved_count(pg) == 2, f"two left to read ({saved_count(pg)})")
    open_collection(pg, 3)
    check("127.0.0.1:9" not in pg.inner_text("#pane-list"), "All articles does not show them")
    check(not pg.errors, "no page errors")


def remove_saved(browser):
    pg = login(context(browser, DESKTOP))
    pg.click("#pane-sources .deck-card[data-sel=saved]")
    pg.wait_for_selector("#pane-list .arow")
    before = pg.locator("#pane-list .arow").count()
    pg.click("#pane-list .arow >> nth=0", button="right")
    pg.click("text=Togli da Da leggere")
    pg.wait_for_selector(".toast:has-text('Tolta da Da leggere')")
    pg.wait_for_timeout(700)
    check(pg.locator("#pane-list .arow").count() == before - 1, "the page is gone from the list")
    # Read later stays in the help, and in the settings.
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(600)
    pg.click(".settings-nav :text('Account')")
    pg.wait_for_timeout(400)
    pg.click(".sheet .row-btn:has-text('Salva per dopo')")
    pg.wait_for_selector(".bookmarklet")
    href = pg.get_attribute(".bookmarklet", "href")
    check(href.startswith("javascript:") and "/#/save?url=" in href, "the bookmarklet points to this server")
    check(not pg.errors, "no page errors")


if __name__ == "__main__":
    run(save_for_later, remove_saved)
