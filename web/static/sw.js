// Service worker. Phase 1: caches the app shell so the PWA starts fast and
// shows its UI even without network. The route table below is meant to be
// extended in later phases (offline articles, push notifications).

const VERSION = '__VERSION__';
const SHELL_CACHE = 'shell-' + VERSION;
// Images survive app updates: proxied URLs are signed and never change.
const IMG_CACHE = 'img-v1';
const IMG_MAX = 1500;

const SHELL = [
  '/',
  '/manifest.webmanifest',
  '/static/app.css',
  '/static/js/boot.js',
  '/static/js/app.js',
  '/static/js/api.js',
  '/static/js/ui.js',
  '/static/js/state.js',
  '/static/js/prefs.js',
  '/static/js/gestures.js',
  '/static/js/views/sources.js',
  '/static/js/views/list.js',
  '/static/js/views/reader.js',
  '/static/js/views/settings.js',
  '/static/js/views/login.js',
  '/static/js/offline/db.js',
  '/static/js/offline/index.js',
  '/static/js/push.js',
  '/static/js/motion.js',
  '/static/js/install.js',
  '/static/js/shortcuts.js',
  '/static/fonts/ebi-furai-mark.woff2',
  '/static/icons/splash-icon.png',
  '/static/icons/shrimp.png',
  '/static/icons/koi.png',
  '/static/js/views/about.js',
  '/static/js/views/health.js',
  '/static/js/views/feedsettings.js',
  '/static/js/views/filters.js',
  '/static/js/views/security.js',
  '/static/vendor/uqr-0.1.3.js',
  '/static/legal/AGPL-3.0.txt',
  '/static/legal/Go-LICENSE.txt',
  '/static/legal/NOTICE.txt',
  '/static/vendor/gsap-3.15.1.min.js',
  '/static/vendor/lenis-1.3.27.min.js',
  '/static/icons/sprite.svg?v=' + VERSION, // versioned: never served from a stale HTTP cache
  '/static/fonts/outfit-latin.woff2',
  '/static/fonts/outfit-latin-ext.woff2',
  '/static/icons/icon-192.png',
  '/static/icons/apple-touch-icon.png',
  '/static/icons/favicon-32.png',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(SHELL_CACHE)
      .then((cache) => cache.addAll(SHELL.map((u) => new Request(u, { cache: 'reload' }))))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k.startsWith('shell-') && k !== SHELL_CACHE).map((k) => caches.delete(k)));
    await self.clients.claim();
  })());
});

// ---- strategies ----

async function networkFirstPage(request) {
  try {
    const res = await fetch(request);
    if (res.ok) {
      const cache = await caches.open(SHELL_CACHE);
      cache.put('/', res.clone());
    }
    return res;
  } catch (e) {
    const cached = await caches.match('/', { cacheName: SHELL_CACHE });
    return cached || Response.error();
  }
}

let imgPuts = 0;

async function trimImages() {
  const cache = await caches.open(IMG_CACHE);
  const keys = await cache.keys();
  // Oldest entries first (insertion order).
  for (let i = 0; i < keys.length - IMG_MAX; i++) await cache.delete(keys[i]);
}

async function cacheFirstImage(request) {
  const cache = await caches.open(IMG_CACHE);
  const cached = await cache.match(request);
  if (cached) return cached;
  const res = await fetch(request);
  if (res.ok) {
    await cache.put(request, res.clone());
    if (++imgPuts % 25 === 0) trimImages();
  }
  return res;
}

// Feed icons: answer from the cache, refresh in the background.
async function staleWhileRevalidate(request, event) {
  const cache = await caches.open(IMG_CACHE);
  const cached = await cache.match(request);
  const update = fetch(request).then((res) => {
    if (res.ok) return cache.put(request, res.clone()).then(() => res);
    return res;
  }).catch(() => cached || Response.error());
  if (cached) {
    event.waitUntil(update);
    return cached;
  }
  return update;
}

async function cacheFirstShell(request) {
  const cached = await caches.match(request, { cacheName: SHELL_CACHE, ignoreSearch: true });
  if (cached) return cached;
  return fetch(request);
}

// ---- routes: first match wins; returning null leaves the request to the network ----

const routes = [
  { match: (url) => url.pathname === '/img', handle: cacheFirstImage },
  { match: (url) => /^\/api\/feeds\/\d+\/icon$/.test(url.pathname), handle: staleWhileRevalidate },
  // Other API calls: network only. Offline answers come from IndexedDB in the page.
  { match: (url) => url.pathname.startsWith('/api/'), handle: null },
  { match: (url, req) => req.mode === 'navigate', handle: networkFirstPage },
  { match: (url) => SHELL.some((u) => u.split('?')[0] === url.pathname), handle: cacheFirstShell },
];

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  for (const r of routes) {
    if (r.match(url, req)) {
      if (r.handle) event.respondWith(r.handle(req, event));
      return;
    }
  }
});

// ---- push notifications (phase 3) ----
// iOS allows no silent pushes: every push must show a notification.

self.addEventListener('push', (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch (e) { /* show a generic notification */ }
  const title = data.title || 'Ebi Furai';
  const options = {
    body: data.body || 'There are articles to read',
    tag: data.tag || 'digest',
    renotify: true,
    icon: '/static/icons/icon-192.png',
    badge: '/static/icons/icon-192.png',
    data: { url: data.url || '/' },
  };
  const badge = typeof data.badge === 'number' && 'setAppBadge' in self.navigator
    ? (data.badge > 0 ? self.navigator.setAppBadge(data.badge) : self.navigator.clearAppBadge()).catch(() => {})
    : Promise.resolve();
  event.waitUntil(Promise.all([self.registration.showNotification(title, options), badge]));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL(event.notification.data && event.notification.data.url || '/', self.location.origin).href;
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const c of all) {
      if (new URL(c.url).origin === self.location.origin) {
        // The app is open: let it switch to the right list itself.
        c.postMessage({ type: 'navigate', url: target });
        await c.focus();
        return;
      }
    }
    await self.clients.openWindow(target);
  })());
});
