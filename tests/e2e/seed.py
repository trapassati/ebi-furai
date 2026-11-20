"""Fills a fresh database with feeds and articles for the tests (no network:
the feeds are never fetched)."""
import os, sqlite3, sys, time

db = sqlite3.connect(sys.argv[1], timeout=10)
c = db.cursor()
uid = c.execute("SELECT id FROM users WHERE username = 'admin'").fetchone()[0]
now = int(time.time())
c.execute("INSERT INTO folders(user_id, name, position) VALUES(?, 'Tecnologia', 1)", (uid,))
folder = c.lastrowid
feeds = []
for i, (title, fold) in enumerate([("Notizie del giorno", None), ("Blog di tecnologia", folder), ("Ricette", None)]):
    c.execute("""INSERT INTO feeds(user_id, folder_id, url, title, site_url, created_at, next_fetch_at, last_fetched_at, last_ok_at)
                 VALUES(?, ?, ?, ?, ?, ?, 4102444800, ?, ?)""",
              (uid, fold, f"https://example.com/feed{i}.xml", title, "https://example.com/", now, now, now))
    feeds.append(c.lastrowid)

long_body = "".join(
    f"<h2>Capitolo {k}</h2>" + "".join(f"<p>Paragrafo {k}.{j}: " + "testo di prova per la lettura lunga. " * 18 + "</p>" for j in range(4))
    for k in range(1, 9))
n = 0
for f in feeds:
    for i in range(40):
        n += 1
        body = long_body if n == 1 else f"<p>Contenuto dell'articolo {n}.</p>"
        c.execute("""INSERT INTO articles(feed_id, user_id, guid_hash, url, title, author, content, preview, thumbnail, published_at, fetched_at)
                     VALUES(?, ?, ?, ?, ?, '', ?, ?, '', ?, ?)""",
                  (f, uid, f"g-{n}", f"https://example.com/a/{n}", f"Articolo numero {n}", body,
                   f"Anteprima dell'articolo {n}", now - n * 600, now))
        c.execute("INSERT INTO articles_fts(rowid, title, body) VALUES(?, ?, ?)", (c.lastrowid, f"Articolo numero {n}", "testo"))
db.commit()
print(f"seeded {len(feeds)} feeds, {n} articles")
