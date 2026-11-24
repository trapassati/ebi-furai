#!/usr/bin/env python3
"""Renders the app icons (PNG) from the SVG drawings in web/assets.

    python3 scripts/render-icons.py

Needs Python Playwright with Chromium (CHROME=/path/to/chrome to use an
installed one). The PNGs are committed: the build does not need this.
"""
import base64, os, pathlib
from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parent.parent
ASSETS = ROOT / "web/assets"
OUT = ROOT / "web/static/icons"
BG = "linear-gradient(135deg, #7b5cff 0%, #3a1f8f 100%)"  # "viola notte"


def data_uri(name):
    return "data:image/svg+xml;base64," + base64.b64encode((ASSETS / name).read_bytes()).decode()


def page(size, body, transparent=False):
    return f"""<!doctype html><html><head><style>
    html,body{{margin:0;width:{size}px;height:{size}px;overflow:hidden;background:{'transparent' if transparent else BG}}}
    img{{position:absolute;display:block}}
    </style></head><body>{body}</body></html>"""


def shot(pg, size, html, out, transparent=False, height=None):
    pg.set_viewport_size({"width": size, "height": height or size})
    pg.set_content(html)
    pg.wait_for_timeout(100)
    pg.screenshot(path=str(OUT / out), omit_background=transparent)


def main():
    shrimp = data_uri("shrimp.svg")
    # The drawing's own center (its bounding box in the 512 view box) goes
    # to the center of the icon.
    cx, cy = 245 / 512, 273 / 512

    def icon(size, scale):
        s = size * scale
        return page(size, f'<img src="{shrimp}" style="width:{s}px;height:{s}px;left:{size / 2 - cx * s}px;top:{size / 2 - cy * s}px">')

    with sync_playwright() as p:
        b = p.chromium.launch(executable_path=os.environ.get("CHROME") or None)
        pg = b.new_page()
        for name, size in [("apple-touch-icon.png", 180), ("icon-192.png", 192), ("icon-512.png", 512), ("favicon-32.png", 32)]:
            shot(pg, size, icon(size, 0.8 if size > 32 else 0.96), name)
        shot(pg, 512, icon(512, 0.62), "icon-maskable-512.png")  # inside the 80% safe zone
        # Splash: the icon as a rounded tile, on transparent.
        shot(pg, 512, page(512, f'''<div style="position:absolute;inset:24px;border-radius:108px;background:{BG};
            box-shadow:0 18px 40px -18px rgba(40,20,110,.55), inset 0 0 0 1px rgba(255,255,255,.12)"></div>
            <img src="{shrimp}" style="width:350px;height:350px;left:{256 - cx * 350}px;top:{256 - cy * 350}px">''', True), "splash-icon.png", True)
        # Transparent drawing for the app (logo in the header).
        shot(pg, 192, page(192, f'<img src="{shrimp}" style="width:192px;height:192px;left:0;top:0">', True), "shrimp.png", True)
        b.close()


if __name__ == "__main__":
    main()
