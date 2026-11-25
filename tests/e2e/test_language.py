"""Language: English or Italian from the device, and the choice in the settings."""
from lib import *


def english_device(browser):
    pg = login(context(browser, locale="en-US"), wait_home=False)
    pg.wait_for_selector(".deck-card")
    text = pg.inner_text("body")
    check("Unread" in text and "Today" in text and "Starred" in text, "an English device gets English")
    check("Non letti" not in text, "no Italian left on the home")
    pg.click("button[aria-label=Settings]")
    pg.wait_for_selector("text=Appearance")
    check("Impostazioni" not in pg.inner_text("body"), "the settings are in English")
    # Server messages follow the app's language.
    status, msg = pg.evaluate("""fetch('/api/articles/999999', {headers: {'X-Lang': 'en'}}).then(async r => [r.status, (await r.json()).error])""")
    check(status == 404 and msg == "not found", f"English error from the server ({msg})")
    status, msg = pg.evaluate("""fetch('/api/articles/999999', {headers: {'X-Lang': 'it'}}).then(async r => [r.status, (await r.json()).error])""")
    check(msg == "non trovato", f"Italian error from the server ({msg})")
    check(not pg.errors, "no page errors")


def switch_language(browser):
    pg = login(context(browser))  # Italian device
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    settings_page(pg, "appearance")
    pg.wait_for_selector(".sheet button[data-v=en]")
    pg.click(".sheet button[data-v=en]")
    pg.wait_for_selector(".deck-card")
    pg.wait_for_timeout(500)
    check("Unread" in pg.inner_text("body"), "choosing English switches the app")
    check(pg.evaluate("document.documentElement.lang") == "en", "and the page language")
    pg.evaluate("localStorage.removeItem('lang')")


if __name__ == "__main__":
    run(english_device, switch_language)
