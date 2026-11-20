"""Offline: actions made without connection reach the server later."""
from lib import *


def offline_actions_are_sent(browser):
    ctx = context(browser)
    pg = login(ctx)
    open_collection(pg, 3)
    pg.wait_for_timeout(2500)  # first sync of the offline copy
    saved = pg.evaluate("import('/static/js/offline/index.js').then(m => m.status.count)")
    check(saved > 0, f"the offline copy has articles ({saved})")
    pg.click("#pane-list .arow >> nth=0")
    pg.wait_for_selector("#pane-reader .article-body")
    aid = int(pg.evaluate("location.hash").split("/")[-1])
    ctx.set_offline(True)
    pg.wait_for_timeout(300)
    pg.click("#pane-reader button[aria-label*='preferiti']")
    pg.wait_for_timeout(800)
    check(sql("SELECT starred FROM articles WHERE id = ?", aid)[0][0] == 0, "offline: the server has not seen it yet")
    ctx.set_offline(False)
    pg.evaluate("window.dispatchEvent(new Event('online'))")
    for _ in range(20):
        if sql("SELECT starred FROM articles WHERE id = ?", aid)[0][0] == 1:
            break
        pg.wait_for_timeout(500)
    check(sql("SELECT starred FROM articles WHERE id = ?", aid)[0][0] == 1, "back online: the star reaches the server")


if __name__ == "__main__":
    run(offline_actions_are_sent)
