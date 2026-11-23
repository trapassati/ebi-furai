// Article list pane.

import { dockInBar, isDesk } from '../desk.js';
import { t } from '../i18n.js';
import { get, isNetworkError, patch, post } from '../api.js';
import { longPress, pullToRefresh, rowSwipe } from '../gestures.js';
import { getPrefs, onPrefsChange, setPref } from '../prefs.js';
import { state, sourceTitle, findFeed, findFolder } from '../state.js';
import { status as offlineStatus, syncNow as offlineSync } from '../offline/index.js';
import { actionSheet, centerEmpty, clear, collapsingTitle, confirmDialog, debounce, emptyBlock, feedIcon, h, icon, keepPlace, readingMinutes, relTime, showError, toast } from '../ui.js';
import * as motion from '../motion.js';
import * as sounds from '../sounds.js';
import { openSaveHelp, removeSaved } from './saved.js';

let ctx;
let titleEl, subtitleEl, smallTitle, listEl, sentinel, emptyEl, scroller, filterBtns, searchBox, searchInput, searchBtn;
let observer;

export function mountList(pane, context) {
  let barSlot;
  ctx = context;
  titleEl = h('h1', { class: 'page-title !text-[1.875rem] truncate' });
  subtitleEl = h('div', { class: 'page-sub truncate' });
  const bar = h('header', { class: 'bar' },
    h('div', { class: 'bar-inner' },
      h('button', { class: 'gbtn glass back-list', 'aria-label': t('Back'), onclick: () => ctx.back('list') },
        icon('o-chevron-left')),
      smallTitle = h('div', { class: 'bar-small-title' }),
      barSlot = h('div', { class: 'bar-slot' }),
      h('button', { class: 'gbtn glass', 'aria-label': t('Order'), onclick: orderMenu }, icon('o-ellipsis-horizontal'))));

  // Bottom dock: mark all read | starred, unread, all | search
  const fbtn = (v, label) => h('button', { class: 'seg-btn', dataset: { v }, 'aria-label': label, onclick: () => setFilter(v) });
  filterBtns = [fbtn('starred', t('Starred only')), fbtn('unread', t('Unread|filter')), fbtn('all', t('All articles'))];
  searchBtn = h('button', { class: 'gbtn glass', 'aria-label': t('Search'), onclick: toggleSearch }, icon('o-magnifying-glass'));
  const dock = h('footer', { class: 'dock' },
    h('button', { class: 'gbtn glass', 'aria-label': t('Mark all as read'), dataset: { key: '⇧A' }, onclick: markAllRead }, icon('o-check-circle')),
    h('div', { class: 'dock-group glass', role: 'group', 'aria-label': t('Filter') }, filterBtns),
    searchBtn);

  searchInput = h('input', {
    class: 'field', type: 'search', placeholder: t('Search articles'), enterkeyhint: 'search',
    autocomplete: 'off', autocorrect: 'off', autocapitalize: 'off', spellcheck: 'false',
  });
  const runSearch = debounce(() => {
    state.list.query = searchInput.value.trim();
    resetList();
  }, 350);
  searchInput.addEventListener('input', runSearch);
  searchInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') searchInput.blur(); });
  searchBox = h('div', { class: 'flex items-center gap-2 px-4 pt-3 hidden' },
    h('div', { class: 'field-wrap flex-1' }, icon('o-magnifying-glass'), searchInput),
    h('button', { class: 'icon-btn text-[0.9375rem] font-medium px-2', onclick: toggleSearch, text: t('Cancel') }));

  const head = h('div', { class: 'list-head pb-4' },
    h('div', { class: 'px-5' }, titleEl, subtitleEl),
    searchBox);

  listEl = h('div', { class: 'alist noselect' });
  sentinel = h('div', { class: 'h-10 flex items-center justify-center text-muted text-sm' });
  emptyEl = h('div', { class: 'hidden px-8 text-center' });
  scroller = h('div', { class: 'pane-scroll' }, h('div', { class: 'safe-x' }, head, listEl, emptyEl, sentinel, h('div', { class: 'dock-pad' })));
  new ResizeObserver(() => centerEmpty(scroller, emptyEl)).observe(scroller);
  pane.append(bar, scroller);
  dockInBar(pane, dock, barSlot);
  collapsingTitle(scroller, smallTitle, () => 40);
  keepPlace(scroller, '.arow');
  setupScrollRead();
  let thumbs = getPrefs().thumbs !== false;
  onPrefsChange(() => {
    const next = getPrefs().thumbs !== false;
    if (next !== thumbs) { thumbs = next; toggleThumbs(next); }
  });
  motion.smoothScroll(scroller);

  pullToRefresh(scroller, async () => {
    await ctx.refreshAll();
    reloadList();
  });

  rowSwipe(listEl, {
    onLeft: (row) => toggle(Number(row.dataset.id), 'read'),
    onRight: (row) => toggle(Number(row.dataset.id), 'starred'),
  });

  // Keyboard (iPad with a keyboard): Enter or Space opens the article.
  listEl.addEventListener('keydown', (e) => {
    const row = e.target.closest('.arow');
    if (row && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); openRow(Number(row.dataset.id)); }
  });

  listEl.addEventListener('click', (e) => {
    const row = e.target.closest('.arow');
    if (!row || row.dataset.swiped) return;
    openRow(Number(row.dataset.id));
  });

  observer = new IntersectionObserver((entries) => {
    if (entries.some((en) => en.isIntersecting)) loadMore();
  }, { root: scroller, rootMargin: '800px 0px' });
  observer.observe(sentinel);
}

// The smart collections are themselves filters: on them the dock switches
// collection (Non letti / Preferiti / Tutti) instead of filtering inside.
const SMART = ['unread', 'starred', 'all'];
const isSmart = () => SMART.includes(state.source);

function effectiveFilter() {
  return isSmart() ? state.source : getPrefs().filter;
}

function setFilter(v) {
  if (isSmart()) {
    if (state.source !== v) ctx.selectSource(v);
    return;
  }
  if (getPrefs().filter === v) return;
  setPref('filter', v);
  renderListHeader();
  reloadList();
}

function orderMenu() {
  const cur = getPrefs().order;
  const onScroll = getPrefs().mark_on_scroll === true;
  const set = (v) => () => {
    if (getPrefs().order === v) return;
    setPref('order', v);
    reloadList();
  };
  actionSheet({
    title: t('Order'),
    actions: [
      { label: t('Newest first'), icon: 'o-bars-arrow-down', tone: 'sky', checked: cur !== 'old', onSelect: set('new') },
      { label: t('Oldest first'), icon: 'o-bars-arrow-up', tone: 'aurora', checked: cur === 'old', onSelect: set('old') },
      { label: t('Mark as read while scrolling'), icon: 'o-check-circle', tone: 'mint', checked: onScroll,
        onSelect: () => {
          setPref('mark_on_scroll', !onScroll);
          toast(onScroll ? t('No longer marked while scrolling') : t('Articles scrolling away are marked as read'));
        } },
    ],
  });
}

// startSearch opens the search box with a query (from the sources pane).
export function startSearch(q) {
  state.list.searchOpen = true;
  state.list.query = q;
  searchBox.classList.remove('hidden');
  searchInput.value = q;
  renderListHeader();
  resetList();
}

function toggleSearch() {
  state.list.searchOpen = !state.list.searchOpen;
  searchBox.classList.toggle('hidden', !state.list.searchOpen);
  renderListHeader();
  if (state.list.searchOpen) {
    searchInput.value = state.list.query;
    motion.scrollTop(scroller, 0);
    searchInput.focus();
  } else if (state.list.query) {
    state.list.query = '';
    searchInput.value = '';
    resetList();
  }
}

// Where an article comes from: its feed, or the site of a page saved for later.
export function sourceLabel(a) {
  if (a.saved && a.url) {
    try { return new URL(a.url).hostname.replace(/^www\./, ''); } catch (e) { /* the feed name */ }
  }
  return a.feed_title;
}

function sourceUnread() {
  const s = state.sources;
  if (!s) return 0;
  const [kind, raw] = state.source.split(':');
  if (kind === 'today') return s.today;
  if (kind === 'unread' || kind === 'all') return s.unread;
  if (kind === 'saved') return s.saved || 0;
  if (kind === 'feed') { const r = findFeed(Number(raw)); return r ? r.feed.unread : 0; }
  if (kind === 'folder') { const f = findFolder(Number(raw)); return f ? f.unread : 0; }
  return 0;
}

export function renderListHeader() {
  if (!titleEl) return;
  const title = sourceTitle(state.source);
  if (titleEl.textContent && titleEl.textContent !== title) motion.swapText([titleEl, subtitleEl]);
  titleEl.textContent = title;
  smallTitle.textContent = title;
  let sub;
  if (state.source === 'starred') {
    sub = state.sources ? t('{0} articles', state.sources.starred) : '';
  } else {
    const n = sourceUnread();
    sub = n ? t('{0} unread', n) : t('All read');
  }
  const f = effectiveFilter();
  if (f === 'starred' && state.source !== 'starred') sub += t(', starred only');
  if (!offlineStatus.online) sub = t('Offline, saved articles');
  subtitleEl.textContent = sub;
  const icons = { starred: ['o-star', 's-star'], unread: ['x-ring', 'x-dot'], all: ['o-list-bullet', 'o-list-bullet'] };
  for (const b of filterBtns) {
    const on = b.dataset.v === f;
    b.classList.toggle('on', on);
    b.setAttribute('aria-pressed', String(on));
    clear(b).append(icon(icons[b.dataset.v][on ? 1 : 0]));
  }
  searchBtn.classList.toggle('text-accent', state.list.searchOpen);
}

// listHasStale: the list shows articles its filter now excludes (read ones
// in an unread list, unstarred ones in a starred list).
export function listHasStale() {
  const f = effectiveFilter();
  if (f === 'unread') return state.list.items.some((a) => a.read);
  if (f === 'starred') return state.list.items.some((a) => !a.starred);
  return false;
}

export function listAtTop() {
  return !scroller || scroller.scrollTop < 80;
}

export function resetList() {
  if (pendingReads.size) flushReads(true); // marked rows are not lost on reload
  const l = state.list;
  l.req++;
  l.items = [];
  l.cursor = '';
  l.done = false;
  l.loading = false;
  l.maxId = 0;
  l.offline = false;
  l.loadedAt = Date.now();
  scanIndex = 0;
  clear(listEl);
  emptyEl.classList.add('hidden');
  swim(null);
  motion.scrollTop(scroller, 0);
  loadMore();
}

// Re-render after sources changed (feed renamed/moved/deleted).
export function rowsChanged() {
  renderListHeader();
  reloadList();
}

// reloadList: the current rows leave with an animation, then the list
// loads again from the top (and the new rows arrive in sequence).
export async function reloadList() {
  const req = state.list.req;
  await motion.leave(listEl.children);
  if (req === state.list.req) resetList();
}

async function loadMore() {
  const l = state.list;
  if (l.loading || l.done || !state.me) return;
  l.loading = true;
  const req = l.req;
  clear(sentinel).append(icon('o-arrow-path', 'w-5 h-5 spin'));
  const params = new URLSearchParams({
    source: state.source, filter: effectiveFilter(), order: getPrefs().order, limit: '40',
  });
  if (l.cursor) params.set('cursor', l.cursor);
  if (l.query) params.set('q', l.query);
  try {
    if (marking && !l.cursor) await marking.catch(() => {});
    if (req !== l.req) return;
    const r = await get('/api/articles?' + params.toString());
    if (req !== l.req) return;
    if (!l.maxId) l.maxId = r.max_id;
    l.offline = !!r.offline;
    l.items.push(...r.items);
    const firstPage = l.items.length === r.items.length;
    const rows = r.items.map(rowEl);
    rows.forEach((row) => listEl.append(row));
    if (firstPage) motion.stagger(rows, { max: 8 });
    l.cursor = r.next_cursor;
    l.done = !r.next_cursor;
    showEmpty();
  } catch (e) {
    // An answer for a list that has since been replaced changes nothing.
    if (req !== l.req) return;
    showError(e);
    l.loading = false;
    // The list stops here, with a way to try again.
    clear(sentinel).append(h('button', {
      class: 'btn btn-plain text-[0.9375rem]', text: t('Retry'),
      onclick: () => { clear(sentinel); loadMore(); },
    }));
    return;
  } finally {
    if (req === l.req && l.loading) {
      l.loading = false;
      clear(sentinel);
      // The sentinel may still be visible: keep loading.
      if (!l.done) requestAnimationFrame(() => {
        const rect = sentinel.getBoundingClientRect();
        if (rect.top < window.innerHeight + 800) loadMore();
      });
    }
  }
}

// "All read": the two koi turn slowly around each other, as if they were
// swimming in a circle. One tween at a time, stopped when the list fills.
let koiTween = null;
function swim(img) {
  if (koiTween) { koiTween.kill(); koiTween = null; }
  if (!img || !motion.enabled() || !window.gsap) return;
  koiTween = window.gsap.to(img, { rotation: -360, duration: 40, ease: 'none', repeat: -1 }); // counter-clockwise: the way they swim
}

function showEmpty() {
  const l = state.list;
  const empty = l.items.length === 0 && l.done;
  emptyEl.classList.toggle('hidden', !empty);
  if (!empty) { swim(null); return; }
  // Nothing left in the list: the reader column does not keep an article.
  ctx.closeArticle();
  clear(emptyEl);
  let title = t('No articles');
  let hint = t('Pull down to refresh.');
  let art = 'o-inbox';
  if (l.offline) { title = t('Nothing saved here'); hint = t('You\'re offline: only the articles downloaded for offline reading are available.'); art = 'o-arrow-down-tray'; }
  else if (l.query) { title = t('No results'); hint = t('Try different words.'); art = 'o-magnifying-glass'; }
  else if (state.source === 'saved' && effectiveFilter() !== 'unread') { title = t('Nothing saved'); hint = t('Save pages to read later from the browser or the share button.'); art = 'o-bookmark'; }
  else if (state.source === 'starred') { title = t('No starred articles'); hint = t('Swipe an article to the right to add it here.'); art = 's-star'; }
  else if (effectiveFilter() === 'starred') { title = t('No starred articles here'); hint = t('Swipe an article to the right to star it.'); art = 's-star'; }
  else if (effectiveFilter() === 'unread') { title = t('You\'ve read everything'); hint = isDesk() ? t('Click the list at the top to see the articles you already read.') : t('Tap the list at the bottom to see the articles you already read.'); art = 's-sparkles'; }
  const koi = art === 's-sparkles'
    ? h('img', { class: 'empty-koi', src: `/static/icons/koi.png?v=${encodeURIComponent(state.me.version || '')}`, alt: '' })
    : null;
  emptyEl.append(...emptyBlock(koi || h('div', { class: 'empty-art' }, icon(art)), title, hint));
  if (state.source === 'saved' && !l.query && !l.offline) {
    emptyEl.append(h('button', { class: 'btn btn-plain mt-4', onclick: openSaveHelp }, icon('o-bookmark', 'w-5 h-5'), t('How to save pages')));
  }
  swim(koi);
  centerEmpty(scroller, emptyEl);
}

// Feeds with "Apri sempre nel sito" open the page instead of the reader:
// window.open runs in the same tap (or Safari blocks it) and the article
// becomes read.
function opensOnSite(a) {
  if (!a || !a.url) return false;
  const r = findFeed(a.feed_id);
  return !!(r && r.feed.open_web);
}

function openRow(id) {
  const a = state.list.items.find((x) => x.id === id);
  if (!opensOnSite(a)) {
    ctx.openArticle(id);
    return;
  }
  window.open(a.url, '_blank', 'noopener');
  if (!a.read) toggle(id, 'read');
}

function rowLabel(a) {
  const mins = readingMinutes(a.words);
  return (a.read ? '' : t('Unread. ')) + `${a.title}. ${sourceLabel(a)}, ${relTime(a.published_at)}` + (mins ? t('. {0} minutes read', mins) : '') + (opensOnSite(a) ? t('. Opens on the site') : '');
}

function rowEl(a) {
  const selected = state.articleId === a.id && state.cols > 1;
  const thumb = a.thumbnail && getPrefs().thumbs !== false
    ? h('img', { class: 'thumb', src: a.thumbnail, alt: '', loading: 'lazy', decoding: 'async' }) : null;
  if (thumb) thumb.addEventListener('error', () => thumb.remove(), { once: true });
  const main = h('div', { class: 'arow-main px-4 pt-3.5 pb-4' },
    h('div', { class: 'a-head flex items-center gap-2 mb-2' },
      h('span', { class: 'unread-dot' }),
      feedIcon(a.feed_id, a.feed_has_icon && !a.saved, sourceLabel(a), 'small'),
      h('div', { class: 'crumbs flex-1' },
        h('span', { class: 'truncate font-medium', text: sourceLabel(a) }),
        icon('o-chevron-right'),
        h('span', { class: 'flex-none', text: relTime(a.published_at) })),
      readingMinutes(a.words) ? h('span', { class: 'reading-time a-time', title: t('Reading time') }, icon('o-clock'), `${readingMinutes(a.words)} min`) : null,
      opensOnSite(a) ? icon('o-arrow-top-right-on-square', 'w-4 h-4 text-muted flex-none a-site') : null,
      a.starred ? icon('s-star', 'w-4 h-4 text-star flex-none a-star') : null),
    h('div', { class: 'flex gap-3' },
      h('div', { class: 'flex-1 min-w-0' },
        h('div', { class: 'a-title font-semibold leading-snug line-clamp-3', text: a.title }),
        a.preview ? h('div', { class: 'a-preview text-soft leading-snug line-clamp-2 mt-1', text: a.preview }) : null),
      thumb));
  const bg = h('div', { class: 'arow-bg' },
    h('div', { class: 'left' }, icon('s-star'), h('span', { text: a.starred ? t('Remove') : t('Star') })),
    h('div', { class: 'right' }, h('span', { text: a.read ? t('Unread|one') : t('Read|one') }), icon('o-check-circle')));
  const row = h('div', {
    class: 'arow' + (a.read ? ' read' : '') + (selected ? ' selected' : ''), dataset: { id: a.id },
    role: 'button', tabindex: '0', 'aria-label': rowLabel(a), 'aria-haspopup': 'menu',
  }, bg, main);
  longPress(row, () => rowMenu(a.id));
  return row;
}

// ---- menu of an article (long press, right click) ----

function rowMenu(id) {
  const a = state.list.items.find((x) => x.id === id);
  if (!a) return;
  const idx = state.list.items.indexOf(a);
  actionSheet({
    title: a.title,
    actions: [
      { label: a.read ? t('Mark as unread') : t('Mark as read'), icon: 'o-check-circle', tone: 'sky',
        onSelect: () => toggle(id, 'read') },
      { label: a.starred ? t('Remove from starred') : t('Add to starred'), icon: 's-star', tone: 'sunset',
        onSelect: () => toggle(id, 'starred') },
      { label: t('Mark the ones above as read'), icon: 'o-bars-arrow-up', tone: 'mint', disabled: idx === 0,
        onSelect: () => markRange(id, 'above') },
      { label: t('Mark the ones below as read'), icon: 'o-bars-arrow-down', tone: 'mint',
        disabled: idx === state.list.items.length - 1 && state.list.done,
        onSelect: () => markRange(id, 'below') },
      a.url ? { label: t('Open on the site'), icon: 'o-arrow-top-right-on-square', tone: 'indigo',
        onSelect: () => window.open(a.url, '_blank', 'noopener') } : null,
      a.saved ? { label: t('Remove from Read later'), icon: 'o-trash', destructive: true,
        onSelect: () => removeSavedRow(id) } : null,
    ].filter(Boolean),
  });
}

// A page saved for later, removed for good.
async function removeSavedRow(id) {
  if (!(await removeSaved(id))) return;
  const row = listEl.querySelector(`.arow[data-id="${id}"]`);
  if (row) await motion.collapse([row]);
  if (row) row.remove();
  const i = state.list.items.findIndex((x) => x.id === id);
  if (i >= 0) state.list.items.splice(i, 1);
  if (state.articleId === id) ctx.closeArticle();
  showEmpty();
  ctx.reloadSources();
}

// "Segna come letti quelli sopra / sotto": the rows on screen change at
// once; the server marks the whole range, also the part not loaded yet.
// Offline only the loaded rows can be marked, one by one (queued).
async function markRange(id, direction) {
  const items = state.list.items;
  const idx = items.findIndex((x) => x.id === id);
  if (idx < 0) return;
  const part = direction === 'above' ? items.slice(0, idx) : items.slice(idx + 1);
  const changed = part.filter((x) => !x.read);
  const source = state.source;
  changed.forEach((x) => ctx.articleChanged(x, { read: true }));
  const body = {
    source, q: state.list.query, filter: getPrefs().filter, order: getPrefs().order,
    id, direction, max_id: state.list.maxId,
  };
  let r = null;
  try {
    r = await post('/api/articles/mark-range', body);
  } catch (e) {
    if (!isNetworkError(e)) {
      changed.forEach((x) => ctx.articleChanged(x, { read: false }));
      showError(e);
      return;
    }
    // Offline: the loaded rows, one by one.
    for (const x of changed) patch(`/api/articles/${x.id}`, { read: true }).catch(() => {});
    toast(changed.length ? t('{0} marked as read', changed.length) : t('Nothing to mark'));
    return;
  }
  const n = r.marked || 0;
  await ctx.reloadSources({ quiet: true });
  offlineSync();
  if (!n) { toast(t('They were all read already')); return; }
  toast(n === 1 ? t('1 marked as read') : t('{0} marked as read', n), '', {
    duration: 6000,
    action: {
      label: t('Undo'),
      onClick: async () => {
        try {
          await post('/api/articles/mark-read/undo', { token: r.undo });
        } catch (e) { showError(e); return; }
        changed.forEach((x) => ctx.articleChanged(x, { read: false }));
        await ctx.reloadSources({ quiet: true });
        offlineSync();
        toast(t('Undone'));
      },
    },
  });
}

export function updateRow(id) {
  const a = state.list.items.find((x) => x.id === id);
  listEl.querySelectorAll('.arow.selected').forEach((r) => {
    if (Number(r.dataset.id) !== state.articleId || state.cols === 1) r.classList.remove('selected');
  });
  if (state.cols > 1 && state.articleId) {
    const sel = listEl.querySelector(`.arow[data-id="${state.articleId}"]`);
    if (sel) sel.classList.add('selected');
  }
  if (!a) return;
  const old = listEl.querySelector(`.arow[data-id="${id}"]`);
  if (!old) return;
  const wasRead = old.classList.contains('read');
  const wasStarred = !!old.querySelector('.a-star');
  // Only the read state changed: update the row in place (the colours fade
  // via CSS) instead of replacing it, so nothing flickers. If its height
  // changes while it is above the visible area, the scroll is compensated
  // and the content on screen stays still.
  if (wasStarred === !!a.starred && wasRead !== !!a.read) {
    const top = scroller.getBoundingClientRect().top + (parseFloat(getComputedStyle(scroller).paddingTop) || 0);
    const above = old.getBoundingClientRect().bottom <= top;
    const h0 = old.offsetHeight;
    old.classList.toggle('read', !!a.read);
    old.setAttribute('aria-label', rowLabel(a));
    const dh = old.offsetHeight - h0;
    if (dh && above) scroller.scrollTop += dh;
    if (!a.read) motion.rowChanged(old, { dot: old.querySelector('.unread-dot') });
    return;
  }
  const fromOpacity = parseFloat(getComputedStyle(old).opacity);
  const row = rowEl(a);
  old.replaceWith(row);
  if (wasRead === !!a.read && wasStarred === !!a.starred) return;
  motion.rowChanged(row, {
    fromOpacity: wasRead !== !!a.read ? fromOpacity : null,
    dot: wasRead && !a.read ? row.querySelector('.unread-dot') : null,
    star: !wasStarred && a.starred ? row.querySelector('.a-star') : null,
  });
}

async function toggle(id, field) {
  const a = state.list.items.find((x) => x.id === id);
  if (!a) return;
  const value = !a[field];
  if (value) sounds.play(field === 'starred' ? 'star' : 'read');
  ctx.articleChanged(a, { [field]: value });
  try {
    const r = await patch(`/api/articles/${id}`, { [field]: value });
    if (r) ctx.alsoRead(r.also_read);
  } catch (e) {
    ctx.articleChanged(a, { [field]: !value });
    showError(e);
  }
}

// For the keyboard shortcuts.
export function markAllReadShortcut() { return markAllRead(); }
export function openListSearch() {
  if (state.list.searchOpen) searchInput.focus();
  else toggleSearch();
}

async function markAllRead() {
  const name = sourceTitle(state.source);
  const ok = await confirmDialog({
    title: t('Mark everything as read?'),
    message: state.list.query ? t('Search results in "{0}"', name) : t('All articles in "{0}"', name),
    okLabel: t('Mark as read|filter'),
    icon: 'o-check',
  });
  if (!ok) return;
  sounds.play('allread');
  // Everything on screen changes at once; the server and the offline copy
  // follow in the background, and the list reloads when they are done.
  const source = state.source;
  const body = { source, max_id: state.list.maxId, q: state.list.query };
  state.list.items.forEach((a) => { a.read = true; });
  if (!body.q) ctx.markedAllRead(source);
  const rows = listEl.querySelectorAll('.arow');
  // Only the dots on screen animate: the others just disappear with them.
  const dots = [...listEl.querySelectorAll('.unread-dot')].filter((d) => {
    const r = d.getBoundingClientRect();
    return r.bottom > 0 && r.top < window.innerHeight;
  });
  const grey = () => rows.forEach((row) => row.classList.add('read'));
  if (motion.enabled() && dots.length) {
    window.gsap.to(dots, { scale: 0, duration: 0.22, stagger: { amount: 0.18 }, ease: 'back.in(2)', onComplete: grey });
  } else grey();
  const posted = post('/api/articles/mark-read', body).catch((e) => { showError(e); return null; });
  const job = (async () => {
    await posted;
    await ctx.reloadSources(); // the server's exact figures
    if (marking === job) marking = null;
    if (state.source === source) await reloadList();
  })();
  marking = job;
  // A few seconds to change one's mind: "Annulla" brings back as unread
  // exactly the articles this operation marked.
  toast(t('Everything marked as read'), '', {
    duration: 6000,
    action: { label: t('Undo'), onClick: () => undoMarkAll(posted, job, source) },
  });
}

async function undoMarkAll(posted, job, source) {
  const run = (async () => {
    const r = await posted;
    if (!r) return; // the marking itself failed: nothing to undo
    if (!r.undo) {
      toast(r.queued ? t('Offline it can\'t be undone') : t('Nothing to undo'));
      return;
    }
    await job.catch(() => {});
    try {
      await post('/api/articles/mark-read/undo', { token: r.undo });
    } catch (e) {
      showError(e);
      return;
    }
    toast(t('Undone'));
  })();
  marking = run; // lists wait for the undo too
  await run;
  if (marking === run) marking = null;
  await ctx.reloadSources();
  if (state.source === source) await reloadList();
}

// A "mark all as read" still running: lists wait for it before loading, so
// they never show as unread what has just been marked.
let marking = null;

// ---- mark as read while scrolling (option) ----
// Every unread row that the user scrolls past, i.e. that leaves the screen
// completely at the top (also from under the glass bar), is marked as read
// and stays in the list, greyed out, like in Reeder.
//
// It works on the scroll position, not on visibility events: on iOS a fast
// flick can carry a row from below the screen to above it between two
// frames, and an observer would never see it on screen (the row would stay
// unread). Rows are checked in order: scanIndex is the first row not yet
// passed, so each scroll frame only looks at the few rows around it.

let scanIndex = 0;
let pendingReads = new Set();
let readTimer = 0;
let lastScroll = 0;

function setupScrollRead() {
  let raf = 0;
  scroller.addEventListener('scroll', () => {
    lastScroll = performance.now();
    if (!raf) raf = requestAnimationFrame(() => { raf = 0; scanPassed(); });
  }, { passive: true });
  // Turning the option on does not mark the rows already scrolled past.
  let on = getPrefs().mark_on_scroll === true;
  onPrefsChange(() => {
    const next = getPrefs().mark_on_scroll === true;
    if (next && !on) passRows(() => {});
    on = next;
  });
}

// passRows keeps scanIndex at the first row not entirely above the top
// edge of the scroller, and calls fn for each row passed on the way down.
// Scrolling back up moves the index back: rows that come into view again
// can be passed (and marked) again.
function passRows(fn) {
  const rows = listEl.children;
  const top = scroller.scrollTop;
  scanIndex = Math.min(scanIndex, rows.length);
  while (scanIndex > 0) {
    const r = rows[scanIndex - 1];
    if (r.offsetTop + r.offsetHeight <= top) break;
    scanIndex--;
  }
  while (scanIndex < rows.length) {
    const r = rows[scanIndex];
    if (r.offsetTop + r.offsetHeight > top) break;
    fn(r);
    scanIndex++;
  }
}

function scanPassed() {
  // A hidden pane (phone, reading an article) has no layout: skip.
  if (scroller.clientHeight === 0) return;
  if (getPrefs().mark_on_scroll !== true) { passRows(() => {}); return; }
  passRows((r) => {
    if (!r.classList.contains('read')) queueRead(Number(r.dataset.id));
  });
}

function queueRead(id) {
  pendingReads.add(id);
  clearTimeout(readTimer);
  readTimer = setTimeout(flushReads, 400);
}

// The rows are greyed out only once the list stands still. Greying changes
// the weight of the title, so a row can change height, and the scroll is
// then corrected; doing that during an inertial scroll on iOS stops the
// momentum and makes the list jerk. Rows are already off screen, so the
// wait is invisible.
const IDLE = 250;

// flushReads marks the batch: rows grey out at once, the counters update
// once, and the server gets one request for the whole batch.
async function flushReads(now = false) {
  const still = performance.now() - lastScroll;
  if (!now && still < IDLE && scroller.clientHeight > 0 && document.visibilityState === 'visible') {
    clearTimeout(readTimer);
    readTimer = setTimeout(flushReads, IDLE - still + 50);
    return;
  }
  clearTimeout(readTimer);
  const items = [...pendingReads].map((id) => state.list.items.find((x) => x.id === id)).filter((a) => a && !a.read);
  pendingReads = new Set();
  if (!items.length) return;
  items.forEach((a) => ctx.articleChanged(a, { read: true }));
  try {
    await post('/api/actions', { actions: items.map((a) => ({ kind: 'article', id: a.id, read: true })) });
  } catch (e) {
    if (isNetworkError(e)) {
      // Offline: the single-article requests go into the sync queue.
      for (const a of items) {
        try { await patch(`/api/articles/${a.id}`, { read: true }); } catch (e2) { ctx.articleChanged(a, { read: false }); }
      }
    } else {
      items.forEach((a) => ctx.articleChanged(a, { read: false }));
      showError(e);
    }
  }
}

// ---- images in the list (option) ----
// Hiding: the images shrink away, then the rows are drawn without them.
// Showing: the rows are drawn with them and they grow in.
async function toggleThumbs(show) {
  const redraw = () => {
    for (const row of listEl.querySelectorAll('.arow')) {
      const a = state.list.items.find((x) => x.id === Number(row.dataset.id));
      if (a) row.replaceWith(rowEl(a));
    }
  };
  if (!show) {
    await motion.collapseWidth(listEl.querySelectorAll('.thumb'));
    redraw();
  } else {
    redraw();
    motion.expandWidth(listEl.querySelectorAll('.thumb'));
  }
}
