"""Apps that sync through the Google Reader API, and the update notice."""
import json, urllib.error, urllib.parse, urllib.request
from lib import *


def api(path, auth=None, data=None, raw=False):
    body = urllib.parse.urlencode(data, doseq=True).encode() if data is not None else None
    headers = {"Authorization": "GoogleLogin auth=" + auth} if auth else {}
    req = urllib.request.Request(URL + path, data=body, headers=headers)
    with urllib.request.urlopen(req, timeout=20) as r:
        text = r.read().decode()
    return text if raw else json.loads(text)


def status(path, auth=None, data=None):
    try:
        api(path, auth, data, raw=True)
        return 200
    except urllib.error.HTTPError as e:
        return e.code


def client_login(user, secret, prefix=""):
    text = api(prefix + "/accounts/ClientLogin", data={"Email": user, "Passwd": secret}, raw=True)
    return dict(line.split("=", 1) for line in text.strip().split("\n"))["Auth"]


def sync_with_an_app(browser):
    sql("DELETE FROM app_passwords")
    pg = login(context(browser))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Reeder e altre app")
    pg.wait_for_selector("text=Ancora nessuna password per le app.")
    check(pg.locator(f"text={URL}/api/greader.php").count() == 1, "the address to type in the app is shown")
    pg.click(".sheet button:has-text('Nuova password per le app')")
    pg.fill(".sheet form input[name=name]", "Reeder test")
    pg.fill(".sheet form input[type=password]", "wrong-password")
    pg.click(".sheet form button[type=submit]")
    pg.wait_for_selector(".sheet form .text-danger:not(.hidden)")
    check(sql("SELECT COUNT(*) FROM app_passwords")[0][0] == 0, "a wrong password makes no app password")
    pg.fill(".sheet form input[type=password]", ADMIN[1])
    pg.click(".sheet form button[type=submit]")
    secret = pg.inner_text(".app-secret").strip()
    check(len(secret.replace("-", "")) == 20, "the new app password is shown once")
    check(sql("SELECT COUNT(*) FROM app_passwords WHERE secret_hash LIKE '%" + secret[:5] + "%'")[0][0] == 0, "only its hash is stored")

    check(status("/accounts/ClientLogin", data={"Email": "admin", "Passwd": ADMIN[1]}) == 401,
          "the account password does not work in apps")
    auth = client_login("admin", secret)
    check(len(auth) > 30, "an app signs in with the app password")
    check(status("/reader/api/0/user-info") == 401 and status("/reader/api/0/user-info", "nope") == 401,
          "the API needs the token")
    info = api("/reader/api/0/user-info", auth)
    check(info["userName"] == "admin", "user info")
    auth2 = client_login("admin", secret.upper().replace("-", " "), prefix="/api/greader.php")
    check(api("/api/greader.php/reader/api/0/user-info", auth2)["userName"] == "admin", "FreshRSS address too, password typed loosely")

    subs = api("/reader/api/0/subscription/list?output=json", auth)["subscriptions"]
    feeds = sql("SELECT COUNT(*) FROM feeds f JOIN users u ON u.id = f.user_id WHERE u.username = 'admin'")[0][0]
    check(len(subs) == feeds, f"subscriptions: {len(subs)} feeds")

    total_unread = unread()
    counts = api("/reader/api/0/unread-count?output=json", auth)["unreadcounts"]
    rl = [c for c in counts if c["id"] == "user/-/state/com.google/reading-list"][0]
    check(rl["count"] == total_unread, f"unread count {rl['count']} like the app ({total_unread})")

    ids, cont = [], ""
    while True:
        page = api("/reader/api/0/stream/items/ids?output=json&n=7&xt=user/-/state/com.google/read&s=user/-/state/com.google/reading-list"
                   + ("&c=" + cont if cont else ""), auth)
        ids += [r["id"] for r in page["itemRefs"]]
        cont = page.get("continuation", "")
        if not cont:
            break
    check(len(ids) == total_unread == len(set(ids)), "the ids of the unread articles, page by page")

    items = api("/reader/api/0/stream/items/contents?output=json", auth, {"i": ids[:3]})["items"]
    check(len(items) == 3 and all(i["id"].startswith("tag:google.com,2005:reader/item/") for i in items), "the articles' contents")
    title = items[0]["title"]
    check(api("/reader/api/0/edit-tag", auth, {"i": items[0]["id"], "a": "user/-/state/com.google/read", "T": "t"}, raw=True) == "OK"
          and sql("SELECT read FROM articles WHERE title = ?", title)[0][0] == 1, "reading in the app reads here")
    api("/reader/api/0/edit-tag", auth, {"i": ids[1], "a": "user/-/state/com.google/starred"}, raw=True)
    check(sql("SELECT starred FROM articles WHERE id = ?", int(ids[1]))[0][0] == 1, "starring in the app stars here")
    pg.reload()
    pg.wait_for_selector(".deck-card")
    check(unread() == total_unread - 1, "the app's change is in the web app's counters")

    # Revoking the app password signs the app out.
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Reeder e altre app")
    row = pg.locator(".sheet .app-pass:has-text('Reeder test')")
    row.wait_for()
    check("Ultimo uso" in row.inner_text(), "the list shows when it was last used")
    row.click()
    pg.click(".sheet button:has-text('Revoca')")
    pg.wait_for_selector("text=Ancora nessuna password per le app.")
    check(status("/reader/api/0/user-info", auth) == 401, "a revoked app password signs its apps out")
    check(not pg.errors, "no page errors")


UPDATE = {"enabled": True, "available": True, "update": {
    "current": "1.1.0", "latest": "9.0.0", "available": True, "install": "docker",
    "notes": "- **Big** news with `code`\n- Second <b>item</b> [link](https://example.org/x)\n\n### Install\nhidden",
    "url": "https://github.com/trapassati/ebi-furai/releases/tag/v9.0.0", "published_at": "2026-11-21T10:00:00Z"}}


def update_notice(browser):
    ctx = context(browser)
    ctx.route("**/api/admin/update", lambda r: r.fulfill(status=200, content_type="application/json", body=json.dumps(UPDATE)))
    pg = login(ctx)
    pg.wait_for_selector(".update-card")
    check("9.0.0" in pg.inner_text(".update-card"), "administrators see the new version when they open the app")
    pg.click(".update-card-btn")
    pg.wait_for_selector(".release-notes li")
    notes = pg.inner_text(".release-notes")
    check(pg.locator(".release-notes strong").count() == 1 and "<b>" in notes and pg.locator(".release-notes b").count() == 0,
          "the notes are shown as text, with their formatting but no HTML")
    check(pg.locator(".command-box code").inner_text() == "docker compose pull && docker compose up -d", "the command for Docker")
    pg.click(".sheet button:has-text('Fine')")
    pg.wait_for_timeout(500)
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    check(pg.locator("text=Aggiornamento disponibile: 9.0.0").count() == 1, "and in Settings")
    pg.keyboard.press("Escape")
    pg.reload()
    pg.wait_for_selector(".update-card")
    pg.click(".update-card-close")
    pg.wait_for_selector(".update-card", state="detached")
    pg.reload()
    pg.wait_for_selector(".deck-card")
    pg.wait_for_timeout(1500)
    check(pg.locator(".update-card").count() == 0, "\"Later\" hides it on this device")
    check(not pg.errors, "no page errors")

    # The notification of a new version opens the app on the update window.
    pg.evaluate("localStorage.removeItem('updateSnoozed')")
    pg.evaluate("history.replaceState(null, '', '/#/update')")
    pg.reload()
    pg.wait_for_selector(".release-notes li")
    check(pg.locator(".command-box").count() == 1 and pg.evaluate("location.hash") != "#/update",
          "the link of the notification opens the update window")
    check(not pg.errors, "no page errors")

    # Without an administrator's session the status is not given.
    check(status("/api/admin/update") == 401, "the update status needs an administrator's session")


if __name__ == "__main__":
    run(sync_with_an_app, update_notice)
