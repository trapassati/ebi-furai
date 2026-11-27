"""The home keeps its place when a folder opens, the folders can be put in
another order, and the feed statistics page shows what was read."""
from lib import *


def add_folders(n):
    """Folders enough to scroll the home, with Tecnologia (which has a feed)
    last. Removed again by remove_folders."""
    uid = sql("SELECT id FROM users WHERE username = 'admin'")[0][0]
    for i in range(n):
        sql("INSERT INTO folders(user_id, name, position) VALUES(?, ?, ?)", uid, f"E2E {i}", 10 + i)
    sql("UPDATE folders SET position = 100 WHERE name = 'Tecnologia'")


def remove_folders():
    sql("DELETE FROM folders WHERE name LIKE 'E2E %'")
    sql("UPDATE folders SET position = 1 WHERE name = 'Tecnologia'")


def folder_keeps_place(browser):
    add_folders(12)
    try:
        pg = login(context(browser))
        sc = pg.locator("#pane-sources .pane-scroll, .pane-scroll").first
        fid = sql("SELECT id FROM folders WHERE name = 'Tecnologia'")[0][0]
        chevron = pg.locator(f"[data-folder='{fid}'] button[aria-expanded]")
        chevron.scroll_into_view_if_needed()
        pg.wait_for_timeout(400)
        before = sc.evaluate("e => e.scrollTop")
        check(before > 100, "the home is scrolled down")
        chevron.tap()
        pg.wait_for_timeout(600)
        check(pg.locator(f"[data-folder='{fid}'] .feed-row.nested").count() == 1, "the folder opened")
        check(abs(sc.evaluate("e => e.scrollTop") - before) < 2, "the home did not jump to the top")
        pg.locator(f"[data-folder='{fid}'] button[aria-expanded]").tap()
        pg.wait_for_timeout(600)
        check(sc.evaluate("e => e.scrollTop") > 100, "nor when it closed")
        check(not pg.errors, "no page errors")
    finally:
        remove_folders()


def reorder_folders(browser):
    add_folders(3)
    try:
        pg = login(context(browser, DESKTOP))
        first = sql("SELECT id FROM folders WHERE name = 'E2E 0'")[0][0]
        pg.click(f".feed-row[data-key='folder:{first}']", button="right")
        pg.click("text=Riordina cartelle")
        pg.wait_for_selector(".reorder-row")
        pg.wait_for_timeout(1000)  # the rows slide in and the page glides to them
        names = lambda: pg.locator(".reorder-row .name").all_text_contents()
        check(names() == ["E2E 0", "E2E 1", "E2E 2", "Tecnologia"], "the folders in their order")
        handle = pg.locator(".reorder-row .drag-handle").first.bounding_box()
        target = pg.locator(".reorder-row").nth(2).bounding_box()
        x, y = handle["x"] + handle["width"] / 2, handle["y"] + handle["height"] / 2
        pg.mouse.move(x, y)
        pg.mouse.down()
        for i in range(1, 13):
            pg.mouse.move(x, y + (target["y"] + target["height"] * 0.7 - handle["y"]) * i / 12)
            pg.wait_for_timeout(30)
        pg.mouse.up()
        pg.wait_for_timeout(800)
        check(names() == ["E2E 1", "E2E 2", "E2E 0", "Tecnologia"], "dragged below two folders")
        stored = [r[0] for r in sql("SELECT name FROM folders ORDER BY position")]
        check(stored == ["E2E 1", "E2E 2", "E2E 0", "Tecnologia"], "the order is saved")
        # The keyboard moves the folder too.
        pg.locator(".reorder-row .drag-handle").nth(2).focus()
        pg.keyboard.press("ArrowUp")
        pg.wait_for_timeout(600)
        check([r[0] for r in sql("SELECT name FROM folders ORDER BY position")][:3] == ["E2E 1", "E2E 0", "E2E 2"], "moved with the arrows")
        pg.click("button.reorder-done")
        pg.wait_for_timeout(600)
        check(pg.locator(".reorder-row").count() == 0, "Done goes back to the home")
        check(pg.locator(".feed-rows > [data-folder] .name").first.text_content() == "E2E 1", "the home in the new order")
        check(not pg.errors, "no page errors")
    finally:
        remove_folders()


def stats_page(browser):
    today = int(time.time()) // 86400
    news = sql("SELECT id FROM feeds WHERE title = 'Notizie del giorno'")[0][0]
    sql("DELETE FROM feed_opens")
    sql("INSERT INTO feed_opens(feed_id, day, opens) VALUES(?, ?, 3), (?, ?, 2)", news, today, news, today - 2)
    try:
        for device in (IPHONE, DESKTOP):
            pg = login(context(browser, device))
            pg.click("button[aria-label='Impostazioni']")
            pg.wait_for_timeout(600)
            settings_page(pg, "stats")
            pg.wait_for_selector(".stats-bar")
            check(pg.locator(".stats-bar").count() == 30, "one bar a day")
            check(pg.locator(".stats-row:has-text('Notizie del giorno')").count() == 1, "the feed read most")
            check(pg.locator(".stats-value").first.text_content().strip() == "5", "five articles opened")
            # Two days of data: no suggestion to unsubscribe yet.
            check(pg.locator("text=Suggerimenti tra").count() == 1, "the suggestions wait for more data")
            check(pg.locator(".stats-remove").count() == 0, "nothing to remove yet")
            pg.locator("button[data-v='90']").click()
            pg.wait_for_timeout(800)
            check(pg.locator(".stats-bar").count() == 90, "90 days")
            check(not pg.errors, "no page errors")
    finally:
        sql("DELETE FROM feed_opens")


run(folder_keeps_place, reorder_folders, stats_page)
