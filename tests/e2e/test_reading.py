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


def image_viewer(browser):
    pg = login(context(browser))
    check(pg.evaluate("getComputedStyle(document.documentElement).touchAction") == "manipulation", "no double-tap zoom on the page")
    prevented = "(() => { const e = new Event('gesturestart', { cancelable: true, bubbles: true }); document.body.dispatchEvent(e); return e.defaultPrevented; })()"
    check(pg.evaluate(prevented), "the page's pinch zoom is stopped")
    open_collection(pg, 0)
    pg.locator("#pane-list .arow").first.click()
    pg.wait_for_selector("#pane-reader .article-body")
    # Article images come through the image proxy, which cannot reach this
    # test server: a same-origin picture is put in the article instead.
    pg.evaluate("""(() => { const b = document.querySelector('#pane-reader .article-body');
      const p = document.createElement('p'); const i = document.createElement('img');
      i.src = '/static/icons/icon-512.png'; i.alt = 'Un gambero'; p.append(i); b.prepend(p); })()""")
    img = pg.locator("#pane-reader .article-body img[alt='Un gambero']").first
    for _ in range(50):
        if img.evaluate("(i) => i.naturalWidth") > 0:
            break
        pg.wait_for_timeout(100)
    img.click()
    pg.wait_for_selector(".iv[role=dialog] .iv-img")
    check(pg.inner_text(".iv-caption") == "Un gambero", "the image opens full screen, with its caption")
    pg.wait_for_timeout(600)
    check(pg.evaluate("(() => { const e = new Event('gesturestart', { cancelable: true, bubbles: true }); document.querySelector('.iv-img').dispatchEvent(e); return !e.defaultPrevented; })()"),
          "zooming is allowed in the viewer")
    box = pg.locator(".iv-img").bounding_box()
    pg.mouse.move(box["x"] + box["width"] / 2, box["y"] + box["height"] / 2)
    pg.keyboard.down("Control")
    pg.mouse.wheel(0, -300)
    pg.keyboard.up("Control")
    pg.wait_for_timeout(300)
    scale = pg.evaluate("gsap.getProperty(document.querySelector('.iv-img'), 'scale')")
    check(scale > 2, f"it zooms in ({scale:.1f}x)")
    pg.keyboard.press("Escape")
    pg.wait_for_selector(".iv", state="detached")
    check(pg.locator("#pane-reader .article-body").count() >= 1, "Esc closes it, the article stays open")
    img.click()
    pg.wait_for_selector(".iv .iv-close")
    pg.wait_for_timeout(500)
    pg.click(".iv .iv-close")
    pg.wait_for_selector(".iv", state="detached")
    check(True, "the close button closes it")
    check(not pg.errors, "no page errors")

# Counts the sounds started (each tone or noise is one source).
COUNT_SOUNDS = """(() => {
  window.__snd = 0;
  for (const C of [window.OscillatorNode, window.AudioBufferSourceNode]) {
    const start = C.prototype.start;
    C.prototype.start = function (...a) { window.__snd++; return start.apply(this, a); };
  }
})()"""


def sound_effects(browser):
    ctx = context(browser, DESKTOP)
    ctx.add_init_script(COUNT_SOUNDS)
    pg = login(ctx)
    sounds = lambda: pg.evaluate("window.__snd")
    open_collection(pg, 0)
    pg.click("#pane-list .arow >> nth=0")
    pg.wait_for_selector("#pane-reader .article-body")
    pg.keyboard.press("s")
    pg.wait_for_timeout(300)
    check(sounds() == 0, "sounds are off by default")

    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click(".switch-row:has-text('Effetti sonori') [role=switch]")
    pg.wait_for_timeout(300)
    check(sounds() == 3, "turning them on plays a preview")
    check(pg.locator(".sound-volume button[data-v='60'].on").count() == 1, "medium volume by default")
    pg.keyboard.press("Escape")
    pg.wait_for_timeout(800)
    pg.click("#pane-reader .article-body")

    pg.evaluate("window.__snd = 0")
    pg.keyboard.press("s")  # unstar: silent
    pg.wait_for_timeout(300)
    check(sounds() == 0, "removing a star is silent")
    pg.keyboard.press("s")
    pg.wait_for_timeout(300)
    check(sounds() == 3, "starring plays the crystal")
    pg.evaluate("window.__snd = 0")
    pg.keyboard.press("m")  # the open article is read: back to unread, silent
    pg.wait_for_timeout(300)
    check(sounds() == 0, "marking as unread is silent")
    pg.keyboard.press("m")
    pg.wait_for_timeout(300)
    check(sounds() == 1, "marking as read plays the drop")
    pg.evaluate("window.__snd = 0")
    pg.click("#pane-list button[aria-label='Segna tutto come letto']")
    pg.click(".confirm button:has-text('Segna come letti')")
    pg.wait_for_timeout(500)
    check(sounds() == 1, "mark all as read plays the swoosh")
    check(not pg.errors, "no page errors")


def reading_appearance(browser):
    # The "Aa" window holds only what changes the article and always fits,
    # with Done in reach, even on a small iPhone.
    pg = login(context(browser, {"viewport": {"width": 375, "height": 667}, "has_touch": True, "is_mobile": True}))
    open_collection(pg, 0)
    pg.click(".arow >> nth=0")
    pg.wait_for_selector(".article-body")
    pg.wait_for_timeout(600)
    pg.click("button[aria-label=Aspetto]")
    pg.wait_for_selector(".reading-appearance")
    pg.wait_for_timeout(600)
    text = pg.inner_text(".reading-appearance")
    check("Effetti sonori" not in text and "Lingua" not in text and "Testo della lista" not in text,
          "no sounds, language or list size in the Aa window")
    check("Interlinea" in text, "the line spacing is there")
    box = pg.evaluate("(() => { const r = document.querySelector('.sheet').getBoundingClientRect(); return [r.top, r.bottom]; })()")
    check(box[0] > 40 and box[1] <= 667 + 1, f"the window fits with room above it ({box})")
    pg.click(".reading-appearance button:has-text('Fine')")
    pg.wait_for_timeout(700)
    check(pg.locator(".reading-appearance").count() == 0, "Done closes it")
    check(not pg.errors, "no page errors")


if __name__ == "__main__":
    run(mark_all_and_undo, counters_follow_server, empty_states_aligned, preferences_per_device, image_viewer, sound_effects, reading_appearance)
