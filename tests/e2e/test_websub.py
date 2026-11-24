"""Instant updates (WebSub): the sign in the feed settings and the callback
the hubs call (confirmations and signed notifications)."""
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
    fid, url = feed("Blog di tecnologia")
    subscribe(url)
    pg = login(context(browser, DESKTOP))
    pg.click(f".feed-row[data-key='feed:{fid}']", button="right")
    pg.click("text=Impostazioni del feed…")
    pg.wait_for_selector("text=Aggiornamento istantaneo")
    check(pg.locator(".sheet [data-instant]").count() == 1, "the feed settings say updates are instant")
    pg.click(".sheet button:has-text('Fine')")
    pg.wait_for_timeout(800)
    other, _ = feed("Ricette")
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


if __name__ == "__main__":
    run(instant_shown, hub_calls)
