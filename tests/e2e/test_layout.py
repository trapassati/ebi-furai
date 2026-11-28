"""Layout: iPhone Duo rotations, desktop scale, mouse and keyboard."""
from lib import *

DUO = {"cover_portrait": (466, 678), "cover_landscape": (678, 466), "open_portrait": (626, 890), "open_landscape": (890, 626)}

ANCHOR = """()=>{const sc=document.querySelector('#pane-reader .pane-scroll');const t=sc.getBoundingClientRect().top+parseFloat(getComputedStyle(sc).paddingTop);
for(const e of sc.querySelectorAll('.reader-title, .article-body > *')){const r=e.getBoundingClientRect();if(r.bottom>t)return [e.textContent.slice(0,22), r.top<t?(t-r.top)/r.height:0]}}"""


def duo_rotations_keep_place(browser):
    pg = login(context(browser, {"viewport": {"width": 466, "height": 678}, "has_touch": True, "is_mobile": True}))
    open_collection(pg, 3)
    pg.click("#pane-list .arow[data-id='1']")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.wait_for_timeout(800)
    pg.evaluate("document.querySelector('#pane-reader .pane-scroll').scrollTop = 1400")
    pg.wait_for_timeout(400)
    start = pg.evaluate(ANCHOR)
    for name in ["cover_landscape", "cover_portrait", "open_portrait", "open_landscape", "cover_portrait", "open_landscape", "cover_portrait"]:
        w, h = DUO[name]
        pg.set_viewport_size({"width": w, "height": h})
        pg.wait_for_timeout(800)
    end = pg.evaluate(ANCHOR)
    check(end[0] == start[0] and abs(end[1] - start[1]) < 0.12, f"reading position kept after 7 rotations ({start} -> {end})")
    check(pg.evaluate("document.documentElement.scrollWidth <= innerWidth"), "no horizontal overflow")
    check(not pg.errors, "no page errors")


def desktop_scale_and_keys(browser):
    pg = login(context(browser, {"viewport": {"width": 2560, "height": 1440}}))
    ui = float(pg.evaluate("getComputedStyle(document.documentElement).getPropertyValue('--ui')"))
    check(abs(ui - 1.3) < 0.01, f"interface at 130% on a 2560 px screen ({ui})")
    pg.keyboard.press("4")
    pg.wait_for_selector("#pane-list .arow")
    pg.keyboard.press("j")
    pg.wait_for_selector("#pane-reader .reader-title")
    first = pg.inner_text("#pane-reader .reader-title")
    pg.wait_for_timeout(500)
    pg.keyboard.press("j")
    pg.wait_for_function("t => document.querySelector('#pane-reader .reader-title')?.textContent !== t", arg=first, timeout=5000)
    check(True, "J opens the next article")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.keyboard.press("s")
    for _ in range(20):
        if sql("SELECT COUNT(*) FROM articles WHERE starred = 1")[0][0] == 1:
            break
        pg.wait_for_timeout(200)
    check(sql("SELECT COUNT(*) FROM articles WHERE starred = 1")[0][0] == 1, "S stars the article")
    pg.keyboard.press("Shift+?")
    pg.wait_for_selector("text=Scorciatoie da tastiera")
    pg.keyboard.press("Escape")
    pg.wait_for_timeout(600)
    check(pg.locator(".sheet").count() == 0, "? shows the shortcuts, Esc closes them")
    card = pg.locator(".deck-card >> nth=1")
    card.hover()
    pg.wait_for_timeout(300)
    check(pg.evaluate("getComputedStyle(document.querySelectorAll('.deck-card')[1]).cursor") == "pointer", "pointer cursor on hover")


def touch_devices_unscaled(browser):
    pg = login(context(browser, IPAD))
    ui = float(pg.evaluate("getComputedStyle(document.documentElement).getPropertyValue('--ui')"))
    check(ui == 1, "touch devices stay at 100%")


def ipad_hide_first_column(browser):
    # The button in the reader hides the first column, landscape and portrait.
    pg = login(context(browser, IPAD))
    width = lambda sel: pg.evaluate(f"document.querySelector('{sel}').getBoundingClientRect().width")
    open_collection(pg, 0)
    pg.click("#pane-list .arow >> nth=0")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.wait_for_timeout(600)
    check(pg.evaluate("document.body.dataset.cols") == "3", "three columns in landscape")
    shown = width("#pane-sources")
    pg.click("#pane-reader .sidebar-btn")
    pg.wait_for_timeout(700)
    check(width("#pane-sources") == 0, f"landscape: the sources column hides ({shown} -> {width('#pane-sources')})")
    pg.click("#pane-reader .sidebar-btn")
    pg.wait_for_timeout(700)
    check(width("#pane-sources") == shown, "and comes back")
    pg.set_viewport_size({"width": 820, "height": 1180})
    pg.wait_for_timeout(800)
    check(pg.evaluate("document.body.dataset.cols") == "2", "two columns in portrait")
    shown = width("#pane-list")
    pg.click("#pane-reader .sidebar-btn")
    pg.wait_for_timeout(700)
    check(width("#pane-list") == 0, f"portrait: the list hides ({shown} -> {width('#pane-list')})")
    pg.click("#pane-reader .sidebar-btn")
    pg.wait_for_timeout(700)
    check(width("#pane-list") == shown, "and comes back")
    check(not pg.errors, "no page errors")


def settings_pages(browser):
    # iPhone: the list of sections, a page slides in, back or a swipe from
    # the left edge returns to the list.
    pg = login(context(browser, IPHONE))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_selector(".settings-list")
    check(pg.locator(".settings-nav").count() == 0, "iPhone: a list, no column on the left")
    settings_page(pg, "reading")
    check(pg.inner_text(".settings-head .sheet-title") == "Lettura", "the page shows its name at the top")
    check(pg.locator(".settings-pageview :text('Unisci i doppioni')").is_visible(), "and its settings")
    pg.click(".settings-back")
    pg.wait_for_timeout(600)
    check(pg.locator(".settings-pageview").is_hidden(), "back returns to the list")
    settings_page(pg, "feeds")
    swipe = """(() => {
      const el = document.querySelector('.settings-stack');
      const touch = (x) => new Touch({ identifier: 1, target: el, clientX: x, clientY: 400 });
      const fire = (type, x) => el.dispatchEvent(new TouchEvent(type, { bubbles: true, cancelable: true,
        touches: type === 'touchend' ? [] : [touch(x)], changedTouches: [touch(x)] }));
      fire('touchstart', 6); for (let x = 20; x <= 300; x += 20) fire('touchmove', x); fire('touchend', 300);
    })()"""
    pg.evaluate(swipe)
    pg.wait_for_timeout(700)
    check(pg.locator(".settings-pageview").is_hidden(), "a swipe from the left edge returns to the list")
    check(pg.inner_text(".settings-head .sheet-title") == "Impostazioni", "with its title")
    # A page inside another one: Statistics in Feeds, one level down.
    settings_page(pg, "stats")
    check(pg.inner_text(".settings-head .sheet-title") == "Statistiche", "Statistics opens inside Feeds")
    check(pg.inner_text(".settings-back") == "Feed", "the way back names Feeds")
    pg.click(".settings-back")
    pg.wait_for_timeout(600)
    check(pg.inner_text(".settings-head .sheet-title") == "Feed", "back returns to Feeds")
    pg.click(".settings-back")
    pg.wait_for_timeout(600)
    check(pg.locator(".settings-pageview").count() == 0, "and then to the list")
    # Windows opened from a page are pages too (here Connected devices).
    settings_page(pg, "account")
    pg.click(".settings-pageview .row-btn:has-text('Dispositivi collegati')")
    pg.wait_for_timeout(700)
    check(pg.locator(".sheet").count() == 1 and pg.inner_text(".settings-head .sheet-title") == "Dispositivi collegati",
          "Connected devices opens as a page, not a window")
    check(pg.inner_text(".settings-back") == "Account", "and goes back to Account")
    check(not pg.errors, "no page errors")
    # iPad: side by side when held sideways, the list when upright; the
    # settings take the whole screen and the list on the left fits in it.
    pg = login(context(browser, IPAD))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_selector(".settings-nav")
    check(pg.locator(".settings-page .settings-section").count() == 1, "iPad sideways: one page beside the list")
    pg.wait_for_timeout(800)
    box = pg.locator(".settings-sheet").bounding_box()
    check(box["y"] == 0 and box["height"] == 820 and box["width"] == 1180, f"the whole screen ({box})")
    nav = pg.locator(".settings-nav").evaluate("n => n.scrollHeight <= n.clientHeight")
    check(nav, "every section fits in the list on the left")
    settings_page(pg, "backup")
    check(pg.locator(".settings-nav-item.on").get_attribute("data-page") == "admin", "Backup is inside Administration")
    pg.click(".settings-up")
    pg.wait_for_timeout(500)
    check(pg.locator(".settings-page .settings-row[data-page=backup]").count() == 1, "and the way back lists it")
    settings_page(pg, "feeds")
    pg.click(".settings-page .row-btn:has-text('Salute dei feed')")
    pg.wait_for_timeout(700)
    check(pg.locator(".sheet").count() == 1 and pg.inner_text(".settings-up") == "Feed", "Feed health opens as a page of Feeds")
    pg.click(".settings-up")
    pg.wait_for_timeout(500)
    pg.set_viewport_size({"width": 820, "height": 1180})
    pg.wait_for_selector(".settings-list")
    check(pg.locator(".settings-nav").count() == 0, "upright: the list, like the iPhone")
    check(not pg.errors, "no page errors")


if __name__ == "__main__":
    run(duo_rotations_keep_place, desktop_scale_and_keys, touch_devices_unscaled, ipad_hide_first_column, settings_pages)
