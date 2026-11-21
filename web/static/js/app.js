// Application bootstrap, layout and navigation.

import './nozoom.js';
import { t } from './i18n.js';
import { get, post, setCsrf, setUnauthorizedHandler } from './api.js';
import { loadPrefs } from './prefs.js';
import { state, findFeed, findFolder, todayStart } from './state.js';
import { loadSprite, showError, toast, debounce } from './ui.js';
import { edgeBack } from './gestures.js';
import { mountSources, renderSources, syncSelected } from './views/sources.js';
import { installShortcuts } from './shortcuts.js';
import { mountList, resetList, renderListHeader, updateRow, rowsChanged, startSearch, listAtTop, listHasStale } from './views/list.js';
import { mountReader, showArticle, showEmptyReader, articleStateChanged } from './views/reader.js';
import { showLogin, hideLogin } from './views/login.js';
import * as offline from './offline/index.js';
import * as push from './push.js';
import * as motion from './motion.js';
import { maybeShowInstallHint } from './install.js';

const body = document.body;
const panes = {};

// ---- layout ----

function computeCols() {
  // Measured in interface points: at a larger scale fewer columns fit.
  const ui = window.__uiScale || 1;
  const w = window.innerWidth / ui;
  const hgt = window.innerHeight / ui;
  const tablet = Math.min(w, hgt) >= 600;
  if (tablet && w >= 1024 && w > hgt) return 3;
  if (tablet && w >= 700) return 2;
  return 1;
}

function applyLayout() {
  const cols = computeCols();
  if (cols !== state.cols) {
    const before = state.cols;
    state.cols = cols;
    body.dataset.cols = String(cols);
    if (cols > 1 && state.view === 'reader' && !state.articleId) state.view = 'list';
    // Folding (iPhone Duo) or narrowing: the open article stays in front,
    // as it was on the larger screen.
    if (cols === 1 && before > 1 && state.view === 'list' && state.articleId) state.view = 'reader';
    if (cols === 1 && before > 1) writeHash();
    body.dataset.view = state.view;
    syncSelected();
    if (state.articleId) updateRow(state.articleId); // selection shows only with columns
    if (cols > 1 && state.articleId) requestAnimationFrame(() => requestAnimationFrame(showSelectedRow));
  }
  // iOS can leave the page itself scrolled after a rotation, which shows
  // as a gap above or below the app.
  if (window.scrollY || window.scrollX) window.scrollTo(0, 0);
}

// After a rotation to two columns the open article's row is in view in
// the list, unless it already is.
function showSelectedRow() {
  const sc = panes.list.querySelector('.pane-scroll');
  const row = sc && sc.querySelector('.arow.selected');
  if (!row || sc.clientHeight === 0) return;
  const r = row.getBoundingClientRect();
  const box = sc.getBoundingClientRect();
  if (r.top >= box.top + 60 && r.bottom <= box.bottom - 20) return;
  motion.scrollTop(sc, Math.max(0, sc.scrollTop + r.top - box.top - sc.clientHeight / 3));
}

// Rotation: iOS may report the new size a little late, so the layout is
// checked again shortly after, not only on the first resize event.
function onRotate() {
  applyLayout();
  setTimeout(applyLayout, 250);
  setTimeout(applyLayout, 700);
}

// ---- navigation ----
// History entries are never pushed: the back buttons and the edge swipe
// handle navigation, so a system back gesture cannot desync the panes.

function writeHash() {
  let hash = '#/' + state.view;
  if (state.view !== 'sources' || state.cols > 1) hash += '/' + state.source;
  if (state.articleId) hash += '/' + state.articleId;
  history.replaceState(null, '', hash);
  try { localStorage.setItem('lastRoute', JSON.stringify({ source: state.source })); } catch (e) { /* ignore */ }
}

// Each pane keeps its scroll position while hidden: WebKit resets the
// scroll of an element with display:none, so it is saved and restored.
const savedScroll = new WeakMap();
function trackScroll(pane) {
  const sc = pane.querySelector('.pane-scroll');
  if (!sc) return;
  sc.addEventListener('scroll', () => { if (sc.clientHeight > 0) savedScroll.set(pane, sc.scrollTop); }, { passive: true });
  sc.addEventListener('scrollset', (e) => savedScroll.set(pane, e.detail));
}
function revealPane(pane) {
  const sc = pane.querySelector('.pane-scroll');
  const y = savedScroll.get(pane);
  if (!sc || !y || sc.clientHeight === 0 || Math.abs(sc.scrollTop - y) < 2) return;
  motion.scrollTop(sc, y);
}

const paneFor = (view) => (view === 'sources' ? panes.sources : view === 'list' ? panes.list : panes.reader);

// animated: false when a back swipe already moved the panes.
function setView(view, { animated = true } = {}) {
  if (state.view === view) return;
  const order = { sources: 0, list: 1, reader: 2 };
  const forward = order[view] > order[state.view];
  const from = paneFor(state.view);
  const to = paneFor(view);
  if (animated && state.cols === 1) {
    if (forward) motion.pushPane(from, to); else motion.popPane(from, to);
  }
  state.view = view;
  body.dataset.view = view;
  syncSelected();
  revealPane(to);
  if (view === 'sources') refreshCountersIfOld(15 * 1000);
  if (animated && state.cols === 2 && view !== 'reader') motion.enterPane(to, forward ? 1 : -1);
  writeHash();
}

function selectSource(source) {
  const same = source === state.source && state.list.items.length > 0;
  state.source = source;
  renderSources();
  setView('list');
  if (!same) {
    state.list.query = '';
    state.list.searchOpen = false;
    resetList();
  } else if (listHasStale()) {
    // Same list, but some articles no longer match its filter (read while
    // scrolling or reading, in "Non letti"): load it again without them.
    resetList();
  }
  renderListHeader();
  writeHash();
}

function openArticle(id) {
  state.articleId = id;
  setView('reader');
  writeHash();
  showArticle(id);
}

// from: the pane whose back button was used ('list' or 'reader').
function back(from, opts) {
  if (from === 'list') {
    setView('sources', opts);
    return;
  }
  if (state.view === 'reader') {
    setView('list', opts);
    if (state.cols === 1) {
      state.articleId = null;
      writeHash();
    }
  } else if (state.view === 'list') {
    setView('sources', opts);
  }
}

// closeArticle empties the reader column when the list has nothing left to
// show (everything read): the open article fades out and the column shows
// "Nessun articolo aperto". On a phone the reader is a screen of its own and
// stays as it is while it is on screen.
async function closeArticle() {
  if (!state.articleId) return;
  if (state.cols === 1 && state.view === 'reader') return;
  state.articleId = null;
  if (state.view === 'reader') {
    state.view = 'list';
    body.dataset.view = 'list';
  }
  writeHash();
  const content = panes.reader.querySelector('.pane-scroll');
  if (state.cols > 1 && motion.enabled() && content && window.gsap) {
    await new Promise((res) => window.gsap.to(content, { opacity: 0, y: 8, duration: 0.2, ease: 'power2.in', onComplete: res }));
    if (state.articleId) { window.gsap.set(content, { clearProps: 'opacity,transform' }); return; } // another one opened meanwhile
    showEmptyReader();
    window.gsap.fromTo(content, { opacity: 0, y: 8 }, { opacity: 1, y: 0, duration: 0.3, ease: 'power2.out', clearProps: 'opacity,transform' });
  } else {
    showEmptyReader();
  }
}

// ---- data ----

// Only the latest request is applied: two loads in flight can answer in
// the other order, and the older figures must not replace the newer ones.
let sourcesReq = 0;
let sourcesAt = 0;
async function reloadSources({ quiet = false } = {}) {
  const req = ++sourcesReq;
  try {
    const s = await get('/api/sources');
    if (req !== sourcesReq) return;
    state.sources = s;
    sourcesAt = Date.now();
    push.setBadge(state.sources.unread);
    renderSources();
    renderListHeader();
  } catch (e) {
    if (!quiet) showError(e);
  }
}
const reloadSourcesSoon = debounce(reloadSources, 1200);

// The server fetches feeds on its own schedule: while the app is open the
// counters are read again every minute, and when going back to the home if
// they are older than 15 seconds, so new articles always show up there.
function refreshCountersIfOld(maxAge) {
  if (!state.me || document.visibilityState !== 'visible') return;
  if (Date.now() - sourcesAt > maxAge) reloadSources({ quiet: true });
}

async function refreshAll() {
  try {
    const r = await post('/api/refresh');
    if (r.failed > 0) toast(t('{0} feeds not updated', r.failed), 'error');
  } catch (e) {
    if (!offline.status.online) toast(t('You\'re offline: saved articles can still be read'));
    else showError(e);
  }
  await reloadSources();
  offline.syncNow();
}

// Keeps counters in sync when an article changes read state locally.
function readChanged(item, read) {
  const s = state.sources;
  if (s) {
    const delta = read ? -1 : 1;
    s.unread = Math.max(0, s.unread + delta);
    push.setBadge(s.unread);
    if (item.published_at >= todayStart()) s.today = Math.max(0, s.today + delta);
    const r = findFeed(item.feed_id);
    if (r) {
      r.feed.unread = Math.max(0, r.feed.unread + delta);
      if (r.folder) r.folder.unread = Math.max(0, r.folder.unread + delta);
    }
    renderSoon();
  }
  reloadSourcesSoon();
}

// "Mark all as read": the counters of the sources change at once, before
// the server's figures arrive, so going back to the home never shows the
// old numbers. Today and starred have no per-feed counts: the server's
// figures follow right after.
function markedAllRead(source) {
  const s = state.sources;
  if (!s) return;
  const [kind, raw] = source.split(':');
  const id = Number(raw);
  const feeds = s.feeds.concat(...s.folders.map((f) => f.feeds));
  let hit = null;
  if (kind === 'feed') hit = feeds.filter((f) => f.id === id);
  else if (kind === 'folder') hit = (s.folders.find((f) => f.id === id) || { feeds: [] }).feeds;
  else if (kind === 'unread' || kind === 'all') hit = feeds;
  if (!hit) return;
  let n = 0;
  hit.forEach((f) => { n += f.unread; f.unread = 0; });
  s.folders.forEach((f) => { f.unread = f.feeds.reduce((t, x) => t + x.unread, 0); });
  s.unread = Math.max(0, s.unread - n);
  if (kind !== 'feed' && kind !== 'folder') s.today = 0;
  push.setBadge(s.unread);
  renderSources();
  renderListHeader();
}

// Many articles can change in a row (marked while scrolling): the sources
// pane and the list header are drawn once per frame, not once per article.
let renderFrame = 0;
function renderSoon() {
  if (renderFrame) return;
  renderFrame = requestAnimationFrame(() => {
    renderFrame = 0;
    renderSources();
    renderListHeader();
  });
}

function starChanged(item, starred) {
  if (state.sources) {
    state.sources.starred = Math.max(0, state.sources.starred + (starred ? 1 : -1));
    renderSoon();
  }
  reloadSourcesSoon();
}

// Called by views when an article's read/starred state changed.
function articleChanged(item, changes) {
  const old = { read: item.read, starred: item.starred };
  const targets = new Set([item, state.list.items.find((a) => a.id === item.id)]);
  if (state.article && state.article.id === item.id) targets.add(state.article);
  targets.forEach((o) => { if (o) Object.assign(o, changes); });
  if ('read' in changes && changes.read !== old.read) readChanged(item, changes.read);
  if ('starred' in changes && changes.starred !== old.starred) starChanged(item, changes.starred);
  updateRow(item.id);
  articleStateChanged(item.id);
}

// iPad: hide the first visible column (sources in 3 columns, the list in
// 2 columns) to give the article more room. Remembered on the device.
function applySidebar(hidden) {
  body.dataset.sidebar = hidden ? 'hidden' : 'shown';
  try { localStorage.setItem('sidebarHidden', hidden ? '1' : ''); } catch (e) { /* ignore */ }
}
function toggleSidebar() {
  applySidebar(body.dataset.sidebar !== 'hidden');
}
try { applySidebar(localStorage.getItem('sidebarHidden') === '1'); } catch (e) { applySidebar(false); }

const ctx = {
  selectSource, openArticle, back, reloadSources, refreshAll, articleChanged, toggleSidebar, markedAllRead, closeArticle,
  searchAll(q) {
    selectSource('all');
    startSearch(q);
  },
  sourceExists(src) {
    const [kind, raw] = src.split(':');
    if (kind === 'feed') return !!findFeed(Number(raw));
    if (kind === 'folder') return !!findFolder(Number(raw));
    return ['today', 'unread', 'starred', 'all'].includes(src);
  },
  async afterSourcesChanged() {
    await reloadSources();
    if (!ctx.sourceExists(state.source)) selectSource('unread');
    else rowsChanged();
  },
  // The server also marked as read other copies of an article (the same
  // story from another feed): the rows on screen follow, the counters too.
  alsoRead(ids) {
    if (!ids || !ids.length) return;
    let missing = false;
    ids.forEach((id) => {
      const it = state.list.items.find((x) => x.id === id);
      if (!it) missing = true;
      else if (!it.read) articleChanged(it, { read: true });
    });
    if (missing) reloadSources({ quiet: true });
  },
  get state() { return state; },
};

// ---- start ----

function parseHash() {
  const parts = location.hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  const r = { view: 'sources', source: null, articleId: null };
  if (parts[0] === 'list' || parts[0] === 'reader' || parts[0] === 'sources') r.view = parts[0];
  if (parts[1]) r.source = decodeURIComponent(parts[1]);
  if (parts[2] && /^\d+$/.test(parts[2])) r.articleId = Number(parts[2]);
  return r;
}

async function start(me) {
  state.me = me;
  setCsrf(me.csrf);
  await offline.init(me);
  loadPrefs(me.prefs);
  document.title = me.app_name;
  hideLogin();
  document.getElementById('app').classList.remove('hidden');

  if (!panes.sources) {
    panes.sources = document.getElementById('pane-sources');
    panes.list = document.getElementById('pane-list');
    panes.reader = document.getElementById('pane-reader');
    mountSources(panes.sources, ctx);
    mountList(panes.list, ctx);
    mountReader(panes.reader, ctx);
    Object.values(panes).forEach(trackScroll);
    motion.setRevealHook(revealPane);
    edgeBack(panes.list, () => state.cols === 1 && state.view === 'list', () => panes.sources, () => back('list', { animated: false }));
    edgeBack(panes.reader, () => state.cols === 1 && state.view === 'reader', () => panes.list, () => back('reader', { animated: false }));
  }

  applyLayout();
  const route = parseHash();
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem('lastRoute') || '{}') || {}; } catch (e) { /* ignore */ }
  state.source = route.source || saved.source || 'unread';

  await reloadSources();
  if (!ctx.sourceExists(state.source)) state.source = 'unread';

  state.view = route.view;
  if (state.view === 'reader' && !route.articleId) state.view = 'list';
  body.dataset.view = state.view;
  renderSources();
  resetList();
  renderListHeader();
  if (route.articleId && state.view === 'reader') {
    state.articleId = route.articleId;
    showArticle(route.articleId);
  } else {
    showEmptyReader();
  }
  writeHash();
  offline.syncNow();
  push.refresh();
  maybeShowInstallHint();
}

// Reacts to the offline layer: status badges, and fresh data on reconnect.
function onOfflineEvent(event) {
  if (!state.me) return;
  renderSources();
  renderListHeader();
  if (event === 'reconnected') {
    const queued = offline.status.pending;
    reloadSources();
    // Refresh the list only if it was showing saved articles, and never
    // under the user's finger (scrolled down, or reading on a phone).
    if (state.list.offline && listAtTop() && !(state.cols === 1 && state.view === 'reader')) resetList();
    if (queued === 0) toast(t('Back online'));
  }
}

// The launch screen fades out once the first screen is ready.
let splashGone = false;
function hideSplash() {
  if (splashGone) return;
  splashGone = true;
  const el = document.getElementById('splash');
  if (!el) return;
  if (!motion.enabled()) { el.remove(); return; }
  window.gsap.to(el, { opacity: 0, duration: 0.35, delay: 0.05, ease: 'power2.out', onComplete: () => el.remove() });
  window.gsap.fromTo(el.querySelector('img'), { scale: 1 }, { scale: 1.06, duration: 0.35, ease: 'power2.out' });
}

async function init() {
  loadSprite();
  setTimeout(hideSplash, 10000); // never stay on the launch screen
  body.dataset.cols = String(computeCols());
  state.cols = computeCols();
  window.addEventListener('resize', debounce(applyLayout, 100));
  window.addEventListener('orientationchange', onRotate);
  if (screen.orientation) screen.orientation.addEventListener('change', onRotate);

  setUnauthorizedHandler(() => {
    document.getElementById('app').classList.add('hidden');
    showLogin(start);
  });

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible' || !state.me) return;
    reloadSources();
    if (Date.now() - offline.status.lastSync > 10 * 60 * 1000) offline.syncNow();
    // After a long pause show fresh articles, unless the user is reading
    // one on a phone or has scrolled down the list (keep their place).
    if (Date.now() - state.list.loadedAt > 5 * 60 * 1000 && !(state.cols === 1 && state.view === 'reader') && listAtTop()) {
      resetList();
    }
  });

  // Tapping the label of a switch row toggles the switch, as in iOS Settings.
  document.addEventListener('click', (e) => {
    const row = e.target.closest('.switch-row');
    if (!row || e.target.closest('.switch, button, a, input')) return;
    const sw = row.querySelector('.switch');
    if (sw && !sw.disabled) sw.click();
  });

  installShortcuts(ctx);
  const update = registerServiceWorker();
  offline.install();
  offline.onChange(onOfflineEvent);
  // Keep the offline copy fresh while the app stays open.
  setInterval(() => { if (state.me && document.visibilityState === 'visible') offline.syncNow(); }, 15 * 60 * 1000);
  setInterval(() => refreshCountersIfOld(30 * 1000), 60 * 1000);

  try {
    const me = await get('/api/me');
    await start(me);
    // Keep the launch screen while the update check runs (a moment); a new
    // version reloads the app from here. 3 s at most, then go on anyway.
    const fresh = await Promise.race([update, new Promise((r) => setTimeout(() => r(false), 3000))]);
    // A new version is installing: the reload comes on controllerchange.
    // If it never does (install failed), carry on with this version.
    if (fresh) await new Promise((r) => setTimeout(r, 6000));
    hideSplash();
  } catch (e) {
    hideSplash();
    if (e.status === 401) showLogin(start);
    else {
      showLogin(start);
      showError(e);
    }
  }
}

// ---- updates ----
// A new version is picked up in one go: at every launch (and when the app
// comes back from the background) the service worker checks for an update
// while the launch screen is still up; if there is one, the app reloads
// right there, so the user never runs the old version and never needs to
// open the app twice. An update found later, during use, waits until the
// app is in the background, so it never refreshes under the user's eyes.
let swReg = null;
let lastShown = Date.now();

function registerServiceWorker() {
  if (!('serviceWorker' in navigator)) return Promise.resolve(false);
  const hadController = !!navigator.serviceWorker.controller;
  let reloading = false;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (!hadController || reloading) return;
    reloading = true;
    const now = () => !splashGone || document.visibilityState === 'hidden' || Date.now() - lastShown < 4000;
    if (now()) { location.reload(); return; }
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') location.reload(); });
  });
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible') return;
    lastShown = Date.now();
    if (swReg) swReg.update().catch(() => {});
  });
  const found = navigator.serviceWorker.register('/sw.js', { scope: '/', updateViaCache: 'none' })
    .then((reg) => { swReg = reg; return hadController ? updateFound(reg) : false; })
    .catch(() => false);
  // A tapped notification asks the open app to show a list.
  navigator.serviceWorker.addEventListener('message', (e) => {
    if (!e.data || e.data.type !== 'navigate' || !state.me) return;
    const parts = new URL(e.data.url).hash.replace(/^#\/?/, '').split('/');
    const source = parts[1] && ctx.sourceExists(parts[1]) ? parts[1] : 'unread';
    const same = source === state.source;
    reloadSources();
    selectSource(source);
    if (same) resetList(); // another source is loaded by selectSource already
  });
  return found;
}

// updateFound resolves true if a new version is being installed (the
// reload follows on controllerchange), false as soon as there is none.
function updateFound(reg) {
  return new Promise((resolve) => {
    const check = () => {
      const w = reg.installing || reg.waiting;
      if (!w) return false;
      w.addEventListener('statechange', () => { if (w.state === 'redundant') resolve(false); });
      resolve(true);
      return true;
    };
    if (check()) return;
    reg.addEventListener('updatefound', check, { once: true });
    reg.update().then(() => { if (!check()) resolve(false); }, () => resolve(false));
  });
}

init();
