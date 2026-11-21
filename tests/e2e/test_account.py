"""Account: two-step verification, connected devices, backups."""
import base64, gzip, hashlib, hmac, os, sqlite3, struct, tempfile, time
from lib import *


def totp(secret, step):
    s = secret.replace(" ", "")
    key = base64.b32decode(s + "=" * ((8 - len(s) % 8) % 8))
    d = hmac.new(key, struct.pack(">Q", step), hashlib.sha1).digest()
    o = d[-1] & 15
    return "%06d" % ((struct.unpack(">I", d[o:o + 4])[0] & 0x7FFFFFFF) % 1_000_000)


def two_step_verification(browser):
    pg = login(context(browser))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Verifica in due passaggi")
    pg.fill(".sheet form input[type=password]", ADMIN[1])
    pg.click(".sheet button:has-text('Continua')")
    pg.wait_for_selector(".qr path")
    secret = pg.inner_text(".secret-key").strip()
    step = int(time.time()) // 30
    pg.fill(".sheet input[inputmode=numeric]", totp(secret, step))
    pg.click(".sheet button:has-text('Attiva')")
    pg.wait_for_selector(".recovery-codes code")
    codes = [e.inner_text() for e in pg.query_selector_all(".recovery-codes code")]
    check(len(codes) == 10, "10 recovery codes")
    raw = sql("SELECT totp_secret FROM users WHERE username = 'admin'")[0][0]
    check(secret.replace(" ", "").encode() not in raw, "the secret is not stored in clear")

    b = login(context(browser, DESKTOP), wait_home=False)
    b.wait_for_selector("input[name=code]:visible")
    check(True, "the password alone asks for the code")
    b.fill("input[name=code]", "000000")
    b.click("#login button[type=submit]")
    b.wait_for_timeout(700)
    check(b.locator(".deck-card").count() == 0, "a wrong code is refused")
    b.fill("input[name=code]", totp(secret, step))
    b.click("#login button[type=submit]")
    b.wait_for_timeout(700)
    check(b.locator(".deck-card").count() == 0, "the code used to enable cannot be used again")
    b.fill("input[name=code]", totp(secret, step + 1))
    b.click("#login button[type=submit]")
    b.wait_for_selector(".deck-card")
    check(True, "the next code signs in")

    c = login(context(browser, IPAD), wait_home=False)
    c.wait_for_selector("input[name=code]:visible")
    c.fill("input[name=code]", codes[0].upper().replace("-", " "))
    c.click("#login button[type=submit]")
    c.wait_for_selector(".deck-card")
    check(sql("SELECT COUNT(*) FROM recovery_codes")[0][0] == 9, "a recovery code signs in once")

    pg.click("text=Li ho salvati")
    pg.wait_for_timeout(500)
    pg.click("text=Dispositivi collegati")
    pg.wait_for_selector("text=Questo dispositivo")
    check(pg.locator(".sheet .row-btn").count() >= 3, "three devices listed")
    pg.click(".sheet button:has-text('Scollega tutti gli altri')")
    pg.click(".confirm button:has-text('Scollega')")
    pg.wait_for_timeout(1000)
    check(b.evaluate("fetch('/api/me').then(r=>r.status)") == 401, "the other devices are signed out")
    check(pg.evaluate("fetch('/api/me').then(r=>r.status)") == 200, "this one stays")


def backups(browser):
    pg = login(context(browser, accept_downloads=True))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Crea un backup ora")
    link = pg.locator("a[href^='/api/admin/backups/']").first
    link.wait_for()
    with pg.expect_download() as dl:
        link.click()
    path = os.path.join(tempfile.mkdtemp(), "b.db")
    with gzip.open(dl.value.path()) as src, open(path, "wb") as out:
        out.write(src.read())
    con = sqlite3.connect(path)
    check(con.execute("PRAGMA integrity_check").fetchone()[0] == "ok", "the downloaded backup is a sound database")
    check(con.execute("SELECT COUNT(*) FROM articles").fetchone()[0] == sql("SELECT COUNT(*) FROM articles")[0][0], "with all the articles")
    check(pg.evaluate("fetch('/api/admin/backups/..%2Freader.db').then(r=>r.status)") == 404, "no path tricks")


def restore(browser):
    pg = login(context(browser))
    pg.click("button[aria-label=Impostazioni]")
    pg.wait_for_timeout(700)
    pg.click("text=Crea un backup ora")
    pg.wait_for_timeout(800)
    # A file that is not a backup is refused, and nothing changes.
    bad = os.path.join(tempfile.mkdtemp(), "not-a-backup.db")
    with open(bad, "wb") as f:
        f.write(b"hello" * 100)
    with pg.expect_file_chooser() as fc:
        pg.click("text=Ripristina da un file…")
    fc.value.set_files(bad)
    pg.fill(".sheet form input[type=password]", ADMIN[1])
    pg.click(".sheet form button[type=submit]")
    pg.wait_for_selector(".sheet .text-danger:has-text('non è un database di Ebi Furai')")
    check(True, "a file that is not a backup is refused")
    pg.click(".sheet form button:has-text('Annulla')")
    pg.wait_for_timeout(500)

    # Something made after the backup: the restore takes it away.
    uid = sql("SELECT id FROM users WHERE username = 'admin'")[0][0]
    sql("INSERT INTO folders(user_id, name) VALUES(?, 'Dopo il backup')", uid)
    pg.click("button[aria-label^='Ripristina il backup del']")
    pg.fill(".sheet form input[type=password]", "wrong-password")
    pg.click(".sheet form button[type=submit]")
    pg.wait_for_selector(".sheet .text-danger:not(.hidden)")
    check(sql("SELECT COUNT(*) FROM folders WHERE name = 'Dopo il backup'")[0][0] == 1, "a wrong password restores nothing")
    pg.fill(".sheet form input[type=password]", ADMIN[1])
    pg.click(".sheet form button[type=submit]")
    pg.wait_for_selector(".restore-overlay")
    check(True, "the app waits for the server to restart")
    pg.wait_for_selector("#login input[name=username]:visible", timeout=30000)
    check(True, "then it reloads and asks to sign in again")
    check(sql("SELECT COUNT(*) FROM folders WHERE name = 'Dopo il backup'")[0][0] == 0, "the database is the backup's")
    check(sql("PRAGMA integrity_check")[0][0] == "ok", "and it is sound")
    pg2 = login(context(browser))
    st = pg2.evaluate("fetch('/api/admin/backups').then(r=>r.json())")
    check(len(st["items"]) >= 2, "the state before the restore was saved as a backup")
    check(not pg.errors, "no page errors")


if __name__ == "__main__":
    run(two_step_verification, backups, restore)
