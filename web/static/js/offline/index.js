// Offline layer: keeps recent articles in IndexedDB, answers API calls from
// it when the server cannot be reached, and queues read/star actions until
// the network comes back.

import { t } from '../i18n.js';
import { ApiError, api, isNetworkError, setOfflineHooks } from '../api.js';
import { getPrefs } from '../prefs.js';
import * as db from './db.js';

const ME_KEY = 'me-cache';
const IMG_CACHE = 'img-v1';
const MAX_IMAGES_PER_SYNC = 300;
const ACTIONS_PER_REQUEST = 200;

export const status = {
  online: true,
  pending: 0, // queued actions
  count: 0, // articles saved offline
  lastSync: 0,
  syncing: false,
};

const listeners = new Set();
export function onChange(fn) { listeners.add(fn); }
function emit(event = 'change') { listeners.forEach((fn) => fn(event, status)); }

let userId = null;
let flushing = null;
let syncing = null;
let probeTimer = null;

// ---- setup ----

// Installs the API hooks. Called before the first request, so that even
// /api/me can be answered from the cache when the app starts offline.
export function install() {
  setOfflineHooks({ intercept, fallback, success, status: (ok) => (ok ? setOnline(true) : reportFailure()) });
  window.addEventListener('online', () => probe());
  window.addEventListener('offline', () => reportFailure());
}

export async function init(me) {
  cacheMe(me);
  if (userId === me.user.id && db.isOpen()) return;
  userId = me.user.id;
  try {
    await db.open(userId);
    status.count = await db.count('articles');
    status.pending = await db.count('queue');
    status.lastSync = (await db.get('meta', 'lastSync')) || 0;
  } catch (e) {
    userId = null; // IndexedDB unavailable (private mode): the app still works online
  }
  if (navigator.storage && navigator.storage.persist) navigator.storage.persist().catch(() => {});
  emit();
}

const enabled = () => userId !== null && db.isOpen();
const offlineWanted = () => getPrefs().offline !== false;

// ---- me cache (lets the app open without network) ----

function cacheMe(me) {
  try { localStorage.setItem(ME_KEY, JSON.stringify(me)); } catch (e) { /* ignore */ }
}

function cachedMe() {
  try { return JSON.parse(localStorage.getItem(ME_KEY) || 'null'); } catch (e) { return null; }
}

// ---- reachability ----

function setOnline(online) {
  if (status.online === online) return;
  status.online = online;
  emit();
  if (online) {
    clearInterval(probeTimer);
    probeTimer = null;
    flushQueue().then(() => syncNow()).finally(() => emit('reconnected'));
  } else if (!probeTimer) {
    probeTimer = setInterval(probe, 20000);
  }
}

// reachable asks the server directly, with a short timeout.
async function reachable() {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 6000);
  try {
    const res = await fetch('/healthz', { cache: 'no-store', signal: ctrl.signal });
    return res.ok;
  } catch (e) {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

// A single failed request is not enough to call the app offline: iOS
// drops requests when the app goes to the background, and the tunnel can
// return a transient error. Only a failed check of the server counts.
let confirming = null;
function reportFailure() {
  if (!status.online || confirming) return;
  confirming = reachable().then((ok) => {
    confirming = null;
    if (!ok) setOnline(false);
  });
}

// probe checks whether the server is reachable again.
async function probe() {
  if (await reachable()) setOnline(true);
}

// ---- helpers ----

function todayStart() {
  const d = new Date();
  return Math.floor(new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() / 1000);
}

function offlineError(msg) { return new ApiError(0, msg); }

async function snapshot() {
  return enabled() ? db.get('meta', 'sources') : null;
}

function folderMap(src) {
  const m = new Map();
  if (src) src.folders.forEach((f) => f.feeds.forEach((feed) => m.set(feed.id, f.id)));
  return m;
}

// Adjusts the cached counters after a local change.
async function adjustCounts(rec, readDelta, starDelta) {
  const src = await snapshot();
  if (!src || !rec) return;
  adjustSnapshot(src, rec, readDelta, starDelta);
  await db.put('meta', src, 'sources');
}

function adjustSnapshot(src, rec, readDelta, starDelta) {
  if (readDelta && rec.saved) {
    src.saved = Math.max(0, (src.saved || 0) + readDelta);
  } else if (readDelta) {
    src.unread = Math.max(0, src.unread + readDelta);
    if (rec.published_at >= todayStart()) src.today = Math.max(0, src.today + readDelta);
    const all = src.feeds.concat(...src.folders.map((f) => f.feeds));
    const feed = all.find((f) => f.id === rec.feed_id);
    if (feed) feed.unread = Math.max(0, feed.unread + readDelta);
    const folder = src.folders.find((f) => f.feeds.some((x) => x.id === rec.feed_id));
    if (folder) folder.unread = Math.max(0, folder.unread + readDelta);
  }
  if (starDelta) src.starred = Math.max(0, src.starred + starDelta);
}

// Applies read/starred changes to the local copy.
async function applyLocal(id, changes) {
  if (!enabled()) return;
  const rec = await db.get('articles', id);
  if (!rec) return;
  let readDelta = 0;
  let starDelta = 0;
  if ('read' in changes && changes.read !== rec.read) readDelta = changes.read ? -1 : 1;
  if ('starred' in changes && changes.starred !== rec.starred) starDelta = changes.starred ? 1 : -1;
  if (!readDelta && !starDelta) return;
  await db.put('articles', { ...rec, ...changes });
  await adjustCounts(rec, readDelta, starDelta);
}

function matches(rec, source, fmap) {
  const [kind, raw] = source.split(':');
  const id = Number(raw);
  switch (kind) {
    case 'today': return !rec.saved && rec.published_at >= todayStart();
    case 'unread': return !rec.saved && !rec.read;
    case 'starred': return rec.starred;
    case 'all': return !rec.saved;
    case 'saved': return !!rec.saved;
    case 'feed': return rec.feed_id === id;
    case 'folder': return fmap.get(rec.feed_id) === id;
  }
  return false;
}

function matchesQuery(rec, q) {
  if (!q) return true;
  const hay = `${rec.title} ${rec.preview} ${rec.feed_title}`.toLowerCase();
  return q.toLowerCase().split(/\s+/).filter(Boolean).every((w) => hay.includes(w));
}

// ---- queue ----

async function enqueue(action) {
  await db.put('queue', { ...action, at: Date.now() });
  status.pending = await db.count('queue');
  emit();
}

// Sends the queued actions in order. Stops at the first network failure.
export function flushQueue() {
  if (!enabled()) return Promise.resolve();
  if (flushing) return flushing;
  flushing = (async () => {
    for (;;) {
      const all = await db.getAll('queue');
      if (!all.length) break;
      const chunk = all.slice(0, ACTIONS_PER_REQUEST);
      try {
        await api('POST', '/api/actions', { actions: chunk.map(({ seq, at, ...a }) => a) }, { raw: true });
      } catch (e) {
        // Network, session, rate limit or a server error (busy, restarting):
        // keep the actions and try again later.
        if (isNetworkError(e) || e.status === 401 || e.status === 403 || e.status === 429 || e.status >= 500) break;
        // A request the server refuses (4xx) would block the queue forever: drop it.
      }
      await db.delMany('queue', chunk.map((a) => a.seq));
    }
    status.pending = await db.count('queue');
    emit();
  })().finally(() => { flushing = null; });
  return flushing;
}

// ---- API hooks ----

function intercept(method, path, body) {
  if (!enabled()) return null;
  let m;
  if (method === 'PATCH' && (m = path.match(/^\/api\/articles\/(\d+)$/))) {
    return articleAction(Number(m[1]), body);
  }
  if (method === 'POST' && path === '/api/articles/mark-read') {
    return markRead(body);
  }
  return null;
}

async function articleAction(id, changes) {
  await applyLocal(id, changes);
  const action = { kind: 'article', id, ...pick(changes, ['read', 'starred']) };
  if (status.pending === 0 && status.online) {
    try {
      return await api('PATCH', `/api/articles/${id}`, changes, { raw: true });
    } catch (e) {
      if (!isNetworkError(e)) throw e;
    }
  }
  // Offline, or older actions still waiting: keep the order.
  await enqueue(action);
  if (status.online) await flushQueue(); else flushQueue();
  return { ok: true, queued: true };
}

async function markRead(body) {
  // The local copy and its counters change in two writes, not several per
  // article. Reading every stored article (with its full text) is the slow
  // part on the iPhone, so online it is done after the server has answered,
  // without waiting: the counters then come from the server, not from here.
  const markLocal = async (counters) => {
    const src = await snapshot();
    const fmap = folderMap(src);
    const all = await db.getAll('articles');
    const hit = all.filter((r) => !r.read && r.id <= (body.max_id || Infinity) && matches(r, body.source, fmap) && matchesQuery(r, body.q));
    if (!hit.length) return 0;
    await db.putMany('articles', hit.map((r) => ({ ...r, read: true })));
    if (src && counters) {
      hit.forEach((r) => adjustSnapshot(src, r, -1, 0));
      await db.put('meta', src, 'sources');
    }
    return hit.length;
  };
  if (status.pending === 0 && status.online) {
    try {
      const r = await api('POST', '/api/articles/mark-read', body, { raw: true });
      markLocal(false).catch(() => {});
      return r;
    } catch (e) {
      if (!isNetworkError(e)) throw e;
    }
  }
  const marked = await markLocal(true);
  await enqueue({ kind: 'mark_read', source: body.source, max_id: body.max_id || 0, q: body.q || '' });
  // Online with older actions waiting: they go first, and the caller reloads
  // the counters only once the server has them all.
  if (status.online) await flushQueue(); else flushQueue();
  return { marked, queued: true };
}

function pick(o, keys) {
  const r = {};
  keys.forEach((k) => { if (o && k in o) r[k] = o[k]; });
  return r;
}

function fallback(method, path) {
  const url = new URL(path, location.origin);
  const p = url.pathname;
  if (method === 'GET' && p === '/api/me') {
    const me = cachedMe();
    return me ? Promise.resolve(me) : null;
  }
  if (!enabled()) return null;
  if (method === 'GET' && p === '/api/sources') {
    return snapshot().then((s) => s || Promise.reject(offlineError(t('You\'re offline'))));
  }
  if (method === 'GET' && p === '/api/articles') return localList(url.searchParams);
  let m;
  if (method === 'GET' && (m = p.match(/^\/api\/articles\/(\d+)$/))) {
    return localArticle(Number(m[1]), url.searchParams.get('mark_read') === '1');
  }
  if (method === 'POST' && (m = p.match(/^\/api\/articles\/(\d+)\/fulltext$/))) {
    return db.get('articles', Number(m[1])).then((r) => {
      if (r && r.has_full) return { full_content: r.full_content };
      throw offlineError(t('You\'re offline: the full text was not downloaded'));
    });
  }
  return null;
}

function success(method, path, body, data) {
  if (method === 'GET' && path === '/api/me') { cacheMe(data); return; }
  if (!enabled()) return;
  const p = path.split('?')[0];
  if (method === 'GET' && p === '/api/sources') {
    db.put('meta', data, 'sources').catch(() => {});
    return;
  }
  let m;
  if (method === 'GET' && (m = p.match(/^\/api\/articles\/(\d+)$/)) && offlineWanted()) {
    // An opened article stays available offline until the next sync.
    db.put('articles', toRecord(data)).then(() => db.count('articles')).then((n) => { status.count = n; }).catch(() => {});
    return;
  }
  if (method === 'POST' && (m = p.match(/^\/api\/articles\/(\d+)\/fulltext$/))) {
    db.update('articles', Number(m[1]), (r) => ({ ...r, full_content: data.full_content, has_full: true })).catch(() => {});
  }
}

// ---- local answers ----

async function localList(params) {
  const source = params.get('source') || 'unread';
  const unreadOnly = params.get('filter') === 'unread' && source !== 'starred';
  const starredOnly = params.get('filter') === 'starred';
  const oldest = params.get('order') === 'old';
  const q = (params.get('q') || '').trim();
  const limit = Math.min(Number(params.get('limit')) || 50, 100);
  const cursor = params.get('cursor') || '';

  const src = await snapshot();
  const fmap = folderMap(src);
  const all = await db.getAll('articles');
  let list = all.filter((r) => matches(r, source, fmap) && (!unreadOnly || !r.read) && (!starredOnly || r.starred) && matchesQuery(r, q));
  const dir = oldest ? 1 : -1;
  list.sort((a, b) => (a.published_at - b.published_at || a.id - b.id) * dir);
  if (cursor) {
    const [cp, ci] = cursor.split(':').map(Number);
    list = list.filter((r) => (oldest
      ? r.published_at > cp || (r.published_at === cp && r.id > ci)
      : r.published_at < cp || (r.published_at === cp && r.id < ci)));
  }
  const page = list.slice(0, limit);
  const last = page[page.length - 1];
  return {
    items: page.map(toSummary),
    next_cursor: list.length > limit && last ? `${last.published_at}:${last.id}` : '',
    max_id: all.reduce((m, r) => Math.max(m, r.id), 0),
    offline: true,
  };
}

async function localArticle(id, markRead) {
  const rec = await db.get('articles', id);
  if (!rec) throw offlineError(t('You\'re offline: this article was not saved'));
  if (markRead && !rec.read) {
    await articleAction(id, { read: true });
    rec.read = true;
  }
  return rec;
}

const SUMMARY_KEYS = ['id', 'feed_id', 'feed_title', 'feed_has_icon', 'title', 'url', 'preview', 'thumbnail', 'published_at', 'read', 'starred'];

function toSummary(r) { return pick(r, SUMMARY_KEYS); }

function toRecord(a) {
  return pick(a, SUMMARY_KEYS.concat(['author', 'content', 'full_content', 'has_full', 'site_url']));
}

// ---- sync ----

// Downloads the offline set: newest unread articles (limit from the
// preferences) plus the starred ones. Queued actions are sent first.
export function syncNow() {
  if (!enabled() || !offlineWanted()) return Promise.resolve();
  if (syncing) return syncing;
  syncing = (async () => {
    status.syncing = true;
    emit();
    try {
      await flushQueue();
      if (status.pending > 0) return; // still offline
      const limit = Number(getPrefs().offline_limit) || 200;
      const have = new Set(await db.getAllKeys('articles'));
      const added = [];
      for (let round = 0; round < 40; round++) {
        const r = await api('POST', '/api/offline/sync', { have: [...have], limit }, { raw: true, timeout: 90000 });
        if (round === 0) {
          const keep = new Map(r.states.map(([id, read, starred]) => [id, { read: !!read, starred: !!starred }]));
          const drop = [...have].filter((id) => !keep.has(id));
          await db.delMany('articles', drop);
          drop.forEach((id) => have.delete(id));
          const existing = (await db.getAll('articles')).filter((rec) => {
            const st = keep.get(rec.id);
            return st && (st.read !== rec.read || st.starred !== rec.starred);
          }).map((rec) => ({ ...rec, ...keep.get(rec.id) }));
          await db.putMany('articles', existing);
          await db.put('meta', r.sources, 'sources');
        }
        const recs = r.articles.map(toRecord);
        await db.putMany('articles', recs);
        recs.forEach((rec) => { have.add(rec.id); added.push(rec); });
        if (!r.more) break;
      }
      status.lastSync = Date.now();
      await db.put('meta', status.lastSync, 'lastSync');
      status.count = await db.count('articles');
      setOnline(true);
      if (getPrefs().offline_images) prefetchImages(added);
      prefetchIcons();
    } catch (e) {
      if (isNetworkError(e)) reportFailure();
    } finally {
      status.syncing = false;
      emit();
    }
  })().finally(() => { syncing = null; });
  return syncing;
}

// Images go through the service worker, which keeps them in its cache.
async function prefetchImages(recs) {
  const urls = new Set();
  for (const r of recs) {
    if (r.thumbnail) urls.add(r.thumbnail);
    const html = (r.full_content || '') + (r.content || '');
    for (const m of html.matchAll(/<img[^>]+src="([^"]+)"/g)) urls.add(m[1].replaceAll('&amp;', '&'));
    if (urls.size >= MAX_IMAGES_PER_SYNC) break;
  }
  await fetchAll([...urls].filter((u) => u.startsWith('/img?')).slice(0, MAX_IMAGES_PER_SYNC));
}

async function prefetchIcons() {
  const src = await snapshot();
  if (!src) return;
  const feeds = src.feeds.concat(...src.folders.map((f) => f.feeds)).filter((f) => f.has_icon);
  await fetchAll(feeds.map((f) => `/api/feeds/${f.id}/icon`));
}

async function fetchAll(urls, concurrency = 4) {
  let i = 0;
  const worker = async () => {
    while (i < urls.length && status.online) {
      const u = urls[i++];
      try { await fetch(u, { credentials: 'same-origin' }); } catch (e) { /* ignore */ }
    }
  };
  await Promise.all(Array.from({ length: concurrency }, worker));
}

// ---- maintenance ----

export async function storageInfo() {
  let usage = 0;
  try {
    if (navigator.storage && navigator.storage.estimate) usage = (await navigator.storage.estimate()).usage || 0;
  } catch (e) { /* ignore */ }
  return { ...status, usage };
}

// Removes the saved articles and images (the queue is kept).
export async function clearSaved() {
  if (enabled()) {
    await db.clear('articles');
    await db.del('meta', 'lastSync');
  }
  status.count = 0;
  status.lastSync = 0;
  if (window.caches) await caches.delete(IMG_CACHE).catch(() => {});
  emit();
}

// Called on logout: nothing of this user stays on the device.
export async function wipe() {
  if (userId !== null) await db.destroy(userId);
  userId = null;
  try { localStorage.removeItem(ME_KEY); } catch (e) { /* ignore */ }
  if (window.caches) await caches.delete(IMG_CACHE).catch(() => {});
}
