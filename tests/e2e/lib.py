"""Shared helpers for the end-to-end tests.

The runner (run.sh) starts a server on a temporary data directory and sets:
  E2E_URL  base URL of the server
  E2E_DB   path of its SQLite database
  CHROME   optional: a Chrome/Chromium binary (default: Playwright's own)
"""
import os, sqlite3, sys, time, traceback

URL = os.environ.get("E2E_URL", "http://127.0.0.1:9199")
DB = os.environ["E2E_DB"]
ADMIN = ("admin", "e2e-password-123")

IPHONE = {"viewport": {"width": 393, "height": 852}, "has_touch": True, "is_mobile": True}
IPAD = {"viewport": {"width": 1180, "height": 820}, "has_touch": True, "is_mobile": True}
DESKTOP = {"viewport": {"width": 1440, "height": 900}}


def db():
    return sqlite3.connect(DB, timeout=10)


def sql(query, *args):
    with db() as c:
        return c.execute(query, args).fetchall()


def reset():
    """Every test starts from the same data: all unread, nothing starred,
    no two-step verification, no feed errors, no extra articles, no
    filters and no feed options."""
    with db() as c:
        c.execute("DELETE FROM articles WHERE guid_hash LIKE 'extra-%'")
        c.execute("UPDATE articles SET read = 0, starred = 0, starred_at = 0")
        c.execute("UPDATE users SET totp_enabled = 0, totp_secret = NULL, totp_last_step = 0, prefs = '{}'")
        c.execute("DELETE FROM recovery_codes")
        c.execute("UPDATE feeds SET error_count = 0, last_error = '', next_fetch_at = 4102444800")
        c.execute("DELETE FROM sessions")
        c.execute("DELETE FROM filters")
        c.execute("UPDATE articles SET hidden_by = NULL, full_content = '', full_tried = 1, dup_of = NULL, words = 0")
        c.execute("UPDATE users SET dedup = 1")
        c.execute("UPDATE feeds SET full_text = 0, open_web = 0, custom_title = ''")
        c.execute("DELETE FROM websub")


def unread():
    return sql("SELECT COUNT(*) FROM articles WHERE read = 0")[0][0]


def launch(p):
    return p.chromium.launch(executable_path=os.environ.get("CHROME") or None, args=["--no-sandbox"])


def context(browser, device=IPHONE, **kw):
    # Italian by default: the tests read the interface's words.
    opts = dict(device_scale_factor=1, service_workers="block", locale="it-IT")
    opts.update(device)
    opts.update(kw)
    return browser.new_context(**opts)


def login(ctx, user=ADMIN[0], password=ADMIN[1], wait_home=True):
    pg = ctx.new_page()
    pg.errors = []
    pg.on("pageerror", lambda e: pg.errors.append(str(e)))
    pg.add_init_script("try{localStorage.setItem('installHint',JSON.stringify({never:true}))}catch(e){}")
    pg.goto(URL + "/")
    pg.fill("input[name=username]", user)
    pg.fill("input[name=password]", password)
    pg.click("#login button[type=submit]")
    if wait_home:
        pg.wait_for_selector(".deck-card")
        pg.wait_for_timeout(600)
    return pg


def home_counts(pg):
    return pg.evaluate("Object.fromEntries([...document.querySelectorAll('#pane-sources .deck-num')].map(e=>[e.dataset.src,Number(e.textContent)]))")


def open_collection(pg, n):
    """0 Non letti, 1 Oggi, 2 Preferiti, 3 Tutti."""
    pg.click(f".deck-card >> nth={n}")
    pg.wait_for_selector("#pane-list .arow")
    pg.wait_for_timeout(500)


def check(cond, what):
    if not cond:
        raise AssertionError(what)
    print("  ok:", what)


def run(*tests):
    """Runs the test functions, prints a summary, exits 1 on failure."""
    from playwright.sync_api import sync_playwright
    failed = 0
    with sync_playwright() as p:
        browser = launch(p)
        for t in tests:
            print(f"- {t.__name__}")
            reset()
            start = time.time()
            try:
                t(browser)
                print(f"  passed in {time.time() - start:.1f}s")
            except Exception:
                failed += 1
                traceback.print_exc()
                print("  FAILED")
        browser.close()
    sys.exit(1 if failed else 0)


def settings_page(pg, page):
    """Opens a page of the open Settings window: from the list on the left
    (wide windows) or from the list that slides away (narrow ones)."""
    pg.click(f".settings-nav-item[data-page={page}], .settings-list [data-page={page}]")
    pg.wait_for_timeout(600)
