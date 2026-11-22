"""Desktop mode: commands at the top, menus where clicked, tooltips,
resizable columns, the settings window. Touch screens keep the phone and
tablet interface."""
from lib import *


def desktop_layout(browser):
    pg = login(context(browser, DESKTOP))
    check(pg.evaluate("document.documentElement.classList.contains('desk')"), "a computer gets the desktop mode")
    check(pg.locator("#pane-list .bar .dock").count() == 1 and pg.locator("#pane-reader .bar .dock").count() == 1,
          "the list and article commands are in the top bars")
    bar = pg.locator("#pane-list .bar").bounding_box()
    check(bar["height"] < 60, f"the bars are thin ({bar['height']:.0f} px)")
    open_collection(pg, 0)
    pg.wait_for_timeout(400)
    rows = pg.locator("#pane-list .arow")
    h = rows.nth(1).bounding_box()["height"]
    radius = float(pg.evaluate("parseFloat(getComputedStyle(document.querySelectorAll('#pane-list .arow')[1]).borderRadius)"))
    check(radius >= 12 and h < 190, f"the list is made of cards, sized for a computer ({h:.0f} px, radius {radius:.0f})")
    glass = pg.evaluate("getComputedStyle(document.getElementById('pane-sources'), '::before').backdropFilter")
    check("blur" in glass, "the sidebar is a floating glass panel")
    side = pg.locator(".deck-card").first.bounding_box()["height"]
    check(side < 44, f"collections are rows in the sidebar ({side:.0f} px)")

    # Right click: a menu at the pointer, no Cancel button, keyboard works.
    r = rows.nth(2).bounding_box()
    pg.mouse.click(r["x"] + 120, r["y"] + 30, button="right")
    pg.wait_for_selector(".menu .menu-item")
    m = pg.locator(".menu").bounding_box()
    check(abs(m["x"] - (r["x"] + 120)) < 4 and abs(m["y"] - (r["y"] + 32)) < 6, "the menu opens where the click was")
    check(pg.locator(".menu :text('Annulla')").count() == 0 and pg.locator(".sheet").count() == 0, "no phone sheet, no Cancel")
    pg.keyboard.press("ArrowDown")
    first = pg.evaluate("document.activeElement.textContent")
    check(first == "Segna come letto", f"arrows move through it ({first})")
    pg.keyboard.press("Escape")
    pg.wait_for_selector(".menu", state="detached")
    check(pg.locator("#pane-list .arow").count() > 10, "Esc closes only the menu")

    # Tooltip with the shortcut.
    rows.first.click()
    pg.wait_for_timeout(600)
    pg.hover("#pane-reader .seg-btn[data-key=S]")
    pg.wait_for_selector(".tooltip.on")
    check(pg.inner_text(".tooltip") .replace("\n", " ").strip().endswith("S"), "hovering a button tells what it does and its key")

    # Resize the sidebar; it is remembered.
    handle = pg.locator("#pane-sources .col-resizer").bounding_box()
    w0 = pg.evaluate("document.getElementById('pane-sources').offsetWidth")
    pg.mouse.move(handle["x"] + 3, handle["y"] + 300)
    pg.mouse.down()
    pg.mouse.move(handle["x"] + 63, handle["y"] + 300, steps=6)
    pg.mouse.up()
    w1 = pg.evaluate("document.getElementById('pane-sources').offsetWidth")
    check(abs(w1 - w0 - 60) <= 2, f"dragging the edge resizes the column ({w0} → {w1})")
    pg.reload()
    pg.wait_for_selector(".deck-card")
    pg.wait_for_timeout(300)
    check(abs(pg.evaluate("document.getElementById('pane-sources').offsetWidth") - w1) <= 2, "and the width is kept")
    pg.evaluate("localStorage.removeItem('colWidths')")

    # Settings: sections on the left.
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_selector(".settings-nav-item")
    check(pg.locator(".settings-page .settings-section").count() == 1, "the settings show one section at a time")
    pg.click(".settings-nav-item:has-text('Account')")
    check(pg.locator(".settings-page :text('Cambia password')").count() == 1, "picking a section shows it")
    # A window opened from the settings takes their place, header included.
    pg.click(".settings-nav-item:has-text('Informazioni')")
    pg.click(".settings-page button:has-text('Informazioni su')")
    pg.wait_for_timeout(600)
    ring = "(() => { const f = document.querySelector(':focus-visible'); return f && f.tagName === 'BUTTON' ? f.textContent : ''; })()"
    check(pg.evaluate(ring) == "" and pg.evaluate("document.activeElement.classList.contains('sheet')"),
          "a window that opens has no outlined button: the focus is on the window")
    pg.keyboard.press("Tab")
    check(pg.evaluate("document.activeElement.textContent").strip().startswith("Indietro"), "Tab starts from its top")
    back = pg.locator(".sheet >> nth=-1 >> button:has-text('Indietro')")
    box = back.bounding_box()
    top = pg.evaluate(f"document.elementFromPoint({box['x'] + box['width'] / 2}, {box['y'] + box['height'] / 2}).closest('button')?.textContent || ''")
    check("Indietro" in top, "nothing covers the header of the window on top")
    back.click()
    pg.wait_for_selector(".settings-sheet:not(.covered)")
    check(pg.locator(".settings-nav-item.on:has-text('Informazioni')").count() == 1, "Back returns to the settings, where they were")
    pg.keyboard.press("Escape")
    check(not pg.errors, "no page errors")


def empty_states_line_up(browser):
    sql("UPDATE articles SET read = 1")
    pg = login(context(browser, DESKTOP))
    pg.wait_for_selector("#pane-list .empty-title")
    pg.wait_for_timeout(600)
    a = pg.locator("#pane-list .empty-title").bounding_box()["y"]
    b = pg.locator("#pane-reader .empty-title").bounding_box()["y"]
    check(abs(a - b) <= 2, f"\"Hai letto tutto\" and \"Nessun articolo aperto\" are on the same line ({a:.0f}, {b:.0f})")
    check("in alto" in pg.inner_text("#pane-list .empty-title + p"), "the hint says where the list filter is on a computer")
    sizes = pg.evaluate("[...document.querySelectorAll('.empty-title + p')].map((p) => getComputedStyle(p).fontSize)")
    check(len(set(sizes)) == 1, f"the two hints have the same size ({sizes})")
    check(not pg.errors, "no page errors")


def touch_keeps_its_interface(browser):
    for device in (IPAD, IPHONE):
        pg = login(context(browser, device))
        check(not pg.evaluate("document.documentElement.classList.contains('desk')"), f"{device['viewport']['width']} px touch: no desktop mode")
        check(pg.locator("#pane-reader > .dock").count() == 1, "the article commands stay at the bottom")
        check(not pg.errors, "no page errors")


if __name__ == "__main__":
    run(desktop_layout, empty_states_line_up, touch_keeps_its_interface)
