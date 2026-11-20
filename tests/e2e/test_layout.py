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


if __name__ == "__main__":
    run(duo_rotations_keep_place, desktop_scale_and_keys, touch_devices_unscaled)
