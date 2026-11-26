"""YouTube channels: large thumbnails with the play sign, the player inside
the article (paused when the article leaves the screen), and hiding the
Shorts."""
import base64, html, time
from lib import *

# A 16x9 grey PNG in place of YouTube's thumbnails (no network here).
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAABAAAAAJCAIAAAC0SDtlAAAAEklEQVR4nGNoaGggDzOMahzVCAD6Dz/hkdbd0QAAAABJRU5ErkJggg==")

VIDEOS = [("aaaaaaaaaa0", False, "Video lungo"), ("aaaaaaaaaa1", True, "Video breve"), ("aaaaaaaaaa2", False, "Altro video")]


def add_channel():
    """A YouTube feed with two videos and a Short, as the server stores them."""
    uid = sql("SELECT id FROM users WHERE username = 'admin'")[0][0]
    now = int(time.time())
    with db() as c:
        c.execute("""INSERT INTO feeds(user_id, url, title, site_url, created_at, next_fetch_at, last_fetched_at, last_ok_at)
                     VALUES(?, 'https://www.youtube.com/feeds/videos.xml?channel_id=UCaaaaaaaaaaaaaaaaaaaaaa', 'Canale video',
                     'https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa', ?, 4102444800, ?, ?)""", (uid, now, now, now))
        fid = c.execute("SELECT last_insert_rowid()").fetchone()[0]
        for i, (vid, short, title) in enumerate(VIDEOS):
            url = f"https://www.youtube.com/shorts/{vid}" if short else f"https://www.youtube.com/watch?v={vid}"
            thumb = f"https://i.ytimg.com/vi/{vid}/hqdefault.jpg"
            body = (f'<p><a href="https://www.youtube.com/watch?v={vid}"><img src="{thumb}" alt=""/></a></p>'
                    f"<p>Descrizione di {html.escape(title)}.</p>")
            c.execute("""INSERT INTO articles(feed_id, user_id, guid_hash, url, title, author, content, preview, thumbnail, published_at, fetched_at)
                         VALUES(?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)""",
                      (fid, uid, f"extra-yt-{vid}", url, title, body, f"Descrizione di {title}.", thumb, now - 60 - i * 600, now))
    return fid


def remove_channel(fid):
    with db() as c:
        c.execute("DELETE FROM articles WHERE feed_id = ?", (fid,))
        c.execute("DELETE FROM feeds WHERE id = ?", (fid,))


def page_with_channel(browser, fid, device=IPHONE):
    ctx = context(browser, device)
    ctx.route("**/img?**", lambda r: r.fulfill(body=PNG, content_type="image/png"))
    ctx.route("https://i.ytimg.com/**", lambda r: r.fulfill(body=PNG, content_type="image/png"))
    pg = login(ctx)
    pg.click(f".feed-row[data-key='feed:{fid}']")
    pg.wait_for_selector("#pane-list .arow")
    pg.wait_for_timeout(700)
    return pg


def player(browser):
    fid = add_channel()
    try:
        pg = page_with_channel(browser, fid)
        check(pg.locator("#pane-list .vthumb").count() == 3, "every video has its large thumbnail")
        check(pg.locator("#pane-list .vthumb .vthumb-play").count() == 3, "with the play sign")
        check(pg.locator("#pane-list .vthumb .vthumb-tag").count() == 1, "the Short is labelled")
        check(pg.locator("#pane-list .thumb").count() == 0, "no small square thumbnail for videos")

        # A stand-in for YouTube's player that notes the commands it gets.
        pg.route("https://www.youtube-nocookie.com/**", lambda r: r.fulfill(
            body="<script>addEventListener('message', (e) => { document.title = String(e.data); });</script>",
            content_type="text/html"))
        pg.click("#pane-list .arow >> nth=0")
        pg.wait_for_selector(".yt-frame iframe")
        pg.wait_for_timeout(800)
        src = pg.locator(".yt-frame iframe").get_attribute("src")
        check(src.startswith("https://www.youtube-nocookie.com/embed/aaaaaaaaaa0?"), "YouTube's player is there at once")
        check("autoplay" not in src and "enablejsapi=1" in src, "it starts with its own play button and can be paused")
        check(pg.locator(".yt-frame iframe").get_attribute("referrerpolicy") == "strict-origin-when-cross-origin",
              "the player gets the origin as referrer")
        check(pg.locator(".article-body img").count() == 0, "the feed's thumbnail gives way to the player")
        check(pg.locator(".article-body:has-text('Descrizione di Video lungo')").count() == 1, "the description stays")
        link = pg.locator(".yt-open")
        check(link.get_attribute("href") == "https://www.youtube.com/watch?v=aaaaaaaaaa0", "Guarda su YouTube links the video")
        check(pg.locator("button[aria-label='Testo completo']").is_disabled(), "no full text for a video")

        player_frame = next(f for f in pg.frames if f.url.startswith("https://www.youtube-nocookie.com/"))
        pg.click("#pane-reader .back-reader")
        pg.wait_for_timeout(800)
        check("pauseVideo" in player_frame.evaluate("document.title"), "going back to the list pauses the video")
        check(not pg.errors, f"no page errors {pg.errors}")
    finally:
        remove_channel(fid)


def hide_shorts(browser):
    fid = add_channel()
    try:
        pg = page_with_channel(browser, fid, DESKTOP)
        pg.click(f".feed-row[data-key='feed:{fid}']", button="right")
        pg.click("text=Impostazioni del feed…")
        pg.wait_for_selector("text=Nascondi gli Shorts")
        check(pg.locator("text=Scarica sempre il testo completo").count() == 0, "no full text option for a channel")
        pg.locator(".sheet .switch").nth(0).click()
        pg.wait_for_timeout(500)
        check(sql("SELECT hide_shorts FROM feeds WHERE id = ?", fid)[0][0] == 1, "saved at once")
        hidden = sql("SELECT title FROM articles WHERE feed_id = ? AND hidden_by = -1 AND read = 1", fid)
        check([t for (t,) in hidden] == ["Video breve"], "only the Short is hidden")
        pg.click(".sheet button:has-text('Fine')")
        pg.wait_for_timeout(1500)
        check(pg.locator("#pane-list .arow").count() == 2, "the list loses the Short")
        check(pg.locator("#pane-list .arow:has-text('Video breve')").count() == 0, "the Short is gone")

        pg.click(f".feed-row[data-key='feed:{fid}']", button="right")
        pg.click("text=Impostazioni del feed…")
        pg.wait_for_selector("text=Nascondi gli Shorts")
        pg.locator(".sheet .switch").nth(0).click()
        pg.wait_for_timeout(500)
        check(sql("SELECT COUNT(*) FROM articles WHERE feed_id = ? AND hidden_by IS NOT NULL", fid)[0][0] == 0,
              "turned off, the Short comes back")
        check(not pg.errors, f"no page errors {pg.errors}")
    finally:
        remove_channel(fid)


if __name__ == "__main__":
    run(player, hide_shorts)
