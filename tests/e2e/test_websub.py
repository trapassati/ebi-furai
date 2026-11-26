"""Instant updates (WebSub): the sign in the feed settings, the callback
the hubs call (confirmations and signed notifications) and the page the
administrators check them on."""
import hashlib, hmac, secrets, urllib.error, urllib.parse, urllib.request

from lib import *

TOKEN = "0123456789abcdef0123456789abcdef"
SECRET = "e2e-websub-secret"
FAR = 4102444800


def feed(title):
    return sql("SELECT id, url FROM feeds WHERE title = ?", title)[0]


def subscribe(url, **cols):
    """A subscription as the hub left it: confirmed, with a long lease."""
    now = int(time.time())
    row = dict(feed_url=url, hub="https://hub.invalid/", topic=url, token=TOKEN, secret=SECRET,
               callback=URL + "/websub/" + TOKEN, lease_seconds=10 * 86400, verified_until=now + 10 * 86400,
               requested_at=now, created_at=now)
    row.update(cols)
    sql(f"INSERT INTO websub({', '.join(row)}) VALUES({', '.join('?' * len(row))})", *row.values())


def call(method, path, body=None, headers=None):
    req = urllib.request.Request(URL + path, data=body, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, r.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()


def instant_shown(browser):
    fid, url = feed("Ricette")
    subscribe(url)
    pg = login(context(browser, DESKTOP))
    pg.click(f".feed-row[data-key='feed:{fid}']", button="right")
    pg.click("text=Impostazioni del feed…")
    pg.wait_for_selector("text=Aggiornamento istantaneo")
    check(pg.locator(".sheet [data-instant]").count() == 1, "the feed settings say updates are instant")
    pg.click(".sheet button:has-text('Fine')")
    pg.wait_for_timeout(800)
    other, _ = feed("Notizie del giorno")
    pg.click(f".feed-row[data-key='feed:{other}']", button="right")
    pg.click("text=Impostazioni del feed…")
    pg.wait_for_selector("text=Scarica sempre il testo completo")
    check(pg.locator(".sheet [data-instant]").count() == 0, "not for a feed without a hub")
    check(not pg.errors, "no page errors")


def hub_calls(browser):
    fid, url = feed("Notizie del giorno")
    subscribe(url, pending="subscribe", verified_until=0)
    q = lambda **p: "/websub/" + TOKEN + "?" + urllib.parse.urlencode(p)

    status, _ = call("GET", q(**{"hub.mode": "subscribe", "hub.topic": "https://other.example/", "hub.challenge": "c1"}))
    check(status == 404, "a check for another topic is refused")
    challenge = secrets.token_hex(8)
    status, body = call("GET", q(**{"hub.mode": "subscribe", "hub.topic": url, "hub.challenge": challenge, "hub.lease_seconds": "7200"}))
    check(status == 200 and body == challenge, "the pending subscription is confirmed with the challenge")
    pending, until = sql("SELECT pending, verified_until FROM websub")[0]
    check(pending == "" and until >= time.time() + 7000, "and stored with its lease")
    status, _ = call("GET", q(**{"hub.mode": "subscribe", "hub.topic": url, "hub.challenge": "again"}))
    check(status == 404, "a check nobody asked for is refused")
    status, _ = call("GET", "/websub/" + "f" * 32 + "?hub.mode=subscribe")
    check(status == 404, "an unknown address is not found")

    payload = b"<feed><entry><title>Nuovo</title></entry></feed>"
    status, _ = call("POST", "/websub/" + TOKEN, payload, {"Content-Type": "application/atom+xml"})
    check(status == 202, "an unsigned notification gets the 2xx the specification asks for")
    bad = "sha256=" + hmac.new(b"wrong", payload, hashlib.sha256).hexdigest()
    call("POST", "/websub/" + TOKEN, payload, {"X-Hub-Signature": bad})
    check(sql("SELECT next_fetch_at FROM feeds WHERE id = ?", fid)[0][0] == FAR
          and sql("SELECT notified_at FROM websub")[0][0] == 0, "unsigned or wrongly signed notifications are ignored")

    good = "sha1=" + hmac.new(SECRET.encode(), payload, hashlib.sha1).hexdigest()
    status, _ = call("POST", "/websub/" + TOKEN, payload, {"X-Hub-Signature": good})
    check(status == 202, "a signed notification is accepted")
    check(sql("SELECT notified_at FROM websub")[0][0] > 0, "and recorded")
    # The fetcher may already have tried the feed (example.com is not a real
    # feed): either way it is no longer waiting for its normal turn.
    check(sql("SELECT next_fetch_at FROM feeds WHERE id = ?", fid)[0][0] != FAR, "the feed is fetched at once")
    others = sql("SELECT COUNT(*) FROM feeds WHERE id <> ? AND kind = 'feed' AND next_fetch_at = ?", fid, FAR)[0][0]
    check(others == 2, "the other feeds keep their turn")


def admin_page(browser):
    _, url = feed("Ricette")
    subscribe(url, hub="https://hub.example/", notified_at=int(time.time()) - 120)
    _, url = feed("Notizie del giorno")
    subscribe(url, hub="https://hub.example/", token="f" * 32, verified_until=0,
              last_error="denied by the hub: not today", requested_at=int(time.time()) - 3600)
    pg = login(context(browser, IPHONE))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_selector(".settings-list")
    settings_page(pg, "websub")
    pg.wait_for_selector(".websub-status")
    check(pg.inner_text(".settings-head .sheet-title") == "In tempo reale", "the page has its name")
    check("127.0.0.1" in pg.inner_text(".websub-status") and "PUBLIC_URL" in pg.inner_text(".websub-status"),
          "it shows the address and where it comes from")
    stats = [x.strip() for x in pg.locator(".websub-stat-value").all_inner_texts()]
    check(stats == ["1", "0", "1"], f"one instant, none waiting, one with errors: {stats}")
    check(pg.locator("text=hub.example").count() >= 1, "the hub is listed")
    check(pg.locator(".health-row :text('Notizie del giorno')").count() == 1, "the failing feed is named")
    check(pg.locator("text=L'hub ha rifiutato l'iscrizione").count() == 1, "with what the error means")
    pg.click("text=Richiedi di nuovo agli hub")
    pg.wait_for_timeout(600)
    # The server may already have asked again (and failed: hub.example is
    # not a real hub): either way the hour-long pause is gone.
    asked = sql("SELECT requested_at FROM websub WHERE token = ?", "f" * 32)[0][0]
    check(asked == 0 or asked > time.time() - 60, "asking again skips the pause")
    check(not pg.errors, "no page errors")

    # Not for the others: the page is missing and the API refuses.
    status, _ = call("GET", "/api/admin/websub")
    check(status in (401, 403), "the API needs an administrator")


if __name__ == "__main__":
    run(instant_shown, hub_calls, admin_page)
