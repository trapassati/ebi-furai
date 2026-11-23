// Reader pane: article content, actions and navigation.

import { dockInBar } from '../desk.js';
import { t } from '../i18n.js';
import { get, patch, post } from '../api.js';
import { state, findFeed } from '../state.js';
import { getPrefs, onPrefsChange } from '../prefs.js';
import { centerEmpty, clear, collapsingTitle, emptyBlock, feedIcon, keepPlace, fullDate, h, icon, iconButton, readingMinutes, showError, toast } from '../ui.js';
import { openAppearance } from './settings.js';
import * as motion from '../motion.js';
import * as sounds from '../sounds.js';
import { enableImageViewer } from './imageviewer.js';
import { startListening, stopListening, supported as canListen } from './listen.js';

let ctx;
let navDir = 0; // +1 next, -1 previous: direction of the content animation
let navPrev, navNext, pageEl;
let book = false, page = 0, pages = 1, pageStep = 1;
let scroller, wrap, feedTitleEl, prevBtn, nextBtn, toolbar;
let btnRead, btnStar, btnFull;
let req = 0;
let showFull = false;
let loadingFull = false;

export function mountReader(pane, context) {
  ctx = context;
  feedTitleEl = h('div', { class: 'bar-small-title !text-[0.9375rem]' });
  prevBtn = iconButton('o-chevron-up', t('Previous article'), () => step(-1), { attrs: { 'data-key': 'K' } });
  nextBtn = iconButton('o-chevron-down', t('Next article'), () => step(1), { attrs: { 'data-key': 'J' } });
  const barSlot = h('div', { class: 'bar-slot' });
  const bar = h('header', { class: 'bar' },
    h('div', { class: 'bar-inner' },
      h('button', { class: 'gbtn glass back-reader', 'aria-label': t('Back'), onclick: () => ctx.back('reader') },
        icon('o-chevron-left')),
      h('button', { class: 'gbtn glass sidebar-btn', 'aria-label': t('Show or hide the column'), dataset: { key: 'B' }, onclick: () => ctx.toggleSidebar() },
        icon('x-sidebar')),
      feedTitleEl,
      barSlot,
      h('div', { class: 'gpair glass' }, prevBtn, nextBtn)));

  wrap = h('article', { class: 'reader-wrap' });
  scroller = h('div', { class: 'pane-scroll' }, h('div', { class: 'safe-x' }, wrap));

  const seg = (name, label, fn, key) => h('button', { class: 'seg-btn', 'aria-label': label, dataset: key ? { key } : undefined, onclick: fn }, icon(name));
  btnRead = seg('o-check-circle', t('Read / unread'), toggleRead, 'M');
  btnStar = seg('o-star', t('Starred|one'), toggleStar, 'S');
  btnFull = seg('o-document-text', t('Full text'), toggleFull, 'F');
  // Navigation group, shown only when the controls sit on the side
  // (iPhone Duo open): previous/next and the page of the book layout.
  navPrev = seg('o-chevron-up', t('Previous article'), () => step(-1));
  navNext = seg('o-chevron-down', t('Next article'), () => step(1));
  pageEl = h('div', { class: 'book-page', 'aria-live': 'polite' });
  toolbar = h('footer', { class: 'dock' },
    h('div', { class: 'dock-group glass dock-nav' }, navPrev, navNext, pageEl),
    h('div', { class: 'dock-group wide glass' },
      btnRead, btnStar, btnFull,
      h('button', { class: 'seg-btn font-semibold text-[1.1875rem]', 'aria-label': t('Appearance'), onclick: openAppearance, text: 'Aa' }),
      canListen() ? seg('o-speaker-wave', t('Listen'), listen, 'L') : null,
      seg('o-arrow-top-right-on-square', t('Open on the site'), openOriginal, 'V'),
      seg('o-arrow-up-on-square', t('Share'), share)));
  pane.append(bar, scroller);
  dockInBar(pane, toolbar, barSlot);
  motion.smoothScroll(scroller);
  new ResizeObserver(() => { if (emptyEl) centerEmpty(scroller, emptyEl); }).observe(scroller);
  // The feed name joins the bar once the article header scrolls away.
  collapsingTitle(scroller, feedTitleEl, () => 70);
  setupBook();
  keepPlace(scroller, '.reader-title, .article-body > *');
  showEmptyReader();
}

export function showEmptyReader() {
  stopListening();
  state.article = null;
  clear(wrap);
  feedTitleEl.textContent = '';
  toolbar.classList.add('hidden');
  prevBtn.disabled = true;
  nextBtn.disabled = true;
  emptyEl = h('div', { class: 'px-8 text-center' },
    ...emptyBlock(h('div', { class: 'empty-art' }, icon('o-newspaper')), t('No article open'), t('Pick one from the list.')));
  wrap.append(emptyEl);
  centerEmpty(scroller, emptyEl);
}
let emptyEl = null;

export async function showArticle(id) {
  const my = ++req;
  showFull = false;
  loadingFull = false;
  updateNav();
  const listItem = state.list.items.find((a) => a.id === id);
  if (listItem) renderHeaderOnly(listItem);
  try {
    const a = await get(`/api/articles/${id}?mark_read=1`);
    if (my !== req) return;
    state.article = a;
    if (listItem && !listItem.read) ctx.articleChanged(listItem, { read: true });
    else ctx.articleChanged(a, {});
    ctx.alsoRead(a.also_read);
    // "Scarica sempre il testo completo": show it straight away; if the
    // server has not got it yet, ask for it now (quietly: on failure the
    // feed's text stays).
    const feed = findFeed(a.feed_id);
    const wantsFull = !!(feed && feed.feed.full_text && a.url);
    if (wantsFull && a.has_full) showFull = true;
    render();
    if (wantsFull && !a.has_full) toggleFull(true);
  } catch (e) {
    if (my !== req) return;
    if (e.status === 0) {
      showEmptyReader();
      clear(wrap).append(h('div', { class: 'pt-[28vh] text-center' },
        h('div', { class: 'empty-art' }, icon('o-arrow-down-tray')),
        h('p', { class: 'text-[1.0625rem] font-medium mb-1', text: t('Article not saved') }),
        h('p', { class: 'text-muted', text: e.message })));
      return;
    }
    showError(e);
    if (e.status === 404) showEmptyReader();
  }
}

function renderHeaderOnly(item) {
  clear(wrap);
  motion.scrollTop(scroller, 0);
  feedTitleEl.textContent = item.feed_title;
  wrap.append(h('h1', { class: 'reader-title mt-2', text: item.title }),
    h('div', { class: 'pt-10 flex justify-center text-muted' }, icon('o-arrow-path', 'w-6 h-6 spin')));
}

function render() {
  const a = state.article;
  if (!a) return;
  stopListening(); // another article, or the full text in place of the feed's
  clear(wrap);
  toolbar.classList.remove('hidden');
  motion.scrollTop(scroller, 0);
  feedTitleEl.textContent = a.feed_title;

  const mins = readingMinutes(a.words);
  const meta = h('div', { class: 'text-[0.875rem] text-muted mt-3 mb-7' },
    a.author ? h('span', { class: 'font-medium text-fg', text: a.author }) : null,
    a.author ? h('br') : null,
    fullDate(a.published_at),
    mins ? h('span', { class: 'reading-time ml-3' }, icon('o-clock'), t('{0} min read', mins)) : null);
  const title = h('h1', { class: 'reader-title mt-2' });
  if (a.url) title.append(h('a', { href: a.url, target: '_blank', rel: 'noopener noreferrer', text: a.title }));
  else title.textContent = a.title;
  wrap.append(
    h('div', { class: 'reader-source mb-4' }, feedIcon(a.feed_id, a.feed_has_icon, a.feed_title, 'big'), h('span', { class: 'truncate', text: a.feed_title })),
    title,
    meta);

  const body = h('div', { class: 'article-body' });
  const html = showFull ? a.full_content : a.content;
  if (html) {
    body.innerHTML = html; // sanitized server-side; CSP blocks scripts
    body.querySelectorAll('img').forEach((img) => img.addEventListener('error', () => img.remove(), { once: true }));
    enableImageViewer(body);
  } else {
    body.append(h('p', { class: 'text-muted', text: t('The feed doesn\'t include the content of this article.') }));
    if (a.url) body.append(h('button', { class: 'btn btn-plain mt-2', onclick: toggleFull, text: t('Load the full text') }));
  }
  wrap.append(body, h('div', { class: 'reader-pad' }));
  page = 0;
  applyBook();
  motion.swapIn(wrap.children, navDir);
  navDir = 0;
  updateToolbar();
  updateNav();
}

function updateToolbar() {
  const a = state.article;
  if (!a) return;
  clear(btnRead).append(icon(a.read ? 's-check-circle' : 'o-check-circle', a.read ? 'text-mint' : ''));
  btnRead.setAttribute('aria-label', a.read ? t('Mark as unread') : t('Mark as read'));
  clear(btnStar).append(icon(a.starred ? 's-star' : 'o-star', a.starred ? 'text-star' : ''));
  btnStar.setAttribute('aria-label', a.starred ? t('Remove from starred') : t('Add to starred'));
  clear(btnFull).append(icon(loadingFull ? 'o-arrow-path' : 'o-document-text', loadingFull ? 'spin' : ''));
  btnFull.classList.toggle('on', showFull);
  btnFull.disabled = !a.url;
}

function updateNav() {
  const items = state.list.items;
  const i = items.findIndex((x) => x.id === state.articleId);
  prevBtn.disabled = navPrev.disabled = i <= 0;
  nextBtn.disabled = navNext.disabled = i < 0 || i >= items.length - 1;
}

// Called when read/starred changed elsewhere (e.g. swipe in the list).
export function articleStateChanged(id) {
  if (state.article && state.article.id === id) updateToolbar();
  updateNav();
}

// Actions for the keyboard shortcuts.
export const readerActions = {
  next: () => step(1), prev: () => step(-1),
  toggleRead: () => toggleRead(), toggleStar: () => toggleStar(),
  toggleFull: () => toggleFull(), openOriginal: () => openOriginal(),
  listen: () => listen(),
};

function listen() {
  if (!state.article) return;
  startListening(scroller.parentElement, scroller, wrap, { book: () => book });
}

function step(dir) {
  const items = state.list.items;
  const i = items.findIndex((x) => x.id === state.articleId);
  const next = items[i + dir];
  if (!next) return;
  navDir = dir;
  ctx.openArticle(next.id);
}

async function toggleRead() {
  const a = state.article;
  if (!a) return;
  const value = !a.read;
  if (value) sounds.play('read');
  ctx.articleChanged(a, { read: value });
  motion.pop(btnRead.firstChild);
  try { await patch(`/api/articles/${a.id}`, { read: value }); } catch (e) { ctx.articleChanged(a, { read: !value }); showError(e); }
}

async function toggleStar() {
  const a = state.article;
  if (!a) return;
  const value = !a.starred;
  if (value) sounds.play('star');
  ctx.articleChanged(a, { starred: value });
  motion.pop(btnStar.firstChild);
  try { await patch(`/api/articles/${a.id}`, { starred: value }); } catch (e) { ctx.articleChanged(a, { starred: !value }); showError(e); }
}

// quiet: started by itself (feed option), so errors are not shown.
async function toggleFull(quiet) {
  const a = state.article;
  if (!a || loadingFull) return;
  if (showFull) {
    showFull = false;
    render();
    return;
  }
  if (a.has_full) {
    showFull = true;
    render();
    return;
  }
  loadingFull = true;
  updateToolbar();
  const id = a.id;
  try {
    const r = await post(`/api/articles/${id}/fulltext`);
    if (!state.article || state.article.id !== id) return;
    a.full_content = r.full_content;
    a.has_full = true;
    if (r.words) a.words = r.words;
    showFull = true;
    loadingFull = false;
    render();
  } catch (e) {
    loadingFull = false;
    updateToolbar();
    if (quiet !== true) showError(e);
  }
}

function openOriginal() {
  const a = state.article;
  if (a && a.url) window.open(a.url, '_blank', 'noopener');
}

async function share() {
  const a = state.article;
  if (!a || !a.url) return;
  if (navigator.share) {
    try { await navigator.share({ title: a.title, url: a.url }); } catch (e) { /* cancelled */ }
    return;
  }
  try {
    await navigator.clipboard.writeText(a.url);
    toast(t('Link copied'));
  } catch (e) {
    toast(a.url);
  }
}

// ---- book layout (iPhone Duo open, article at full width) ----
// The article flows into two columns per page; pages turn with a swipe,
// a tap on the outer thirds, or the arrow keys.

const BOOK_GAP = 56;

function bookCapable() {
  return getPrefs().book !== false && window.innerHeight <= 700 && window.innerHeight >= 560 && window.innerWidth > window.innerHeight &&
    scroller.clientWidth >= 760 && window.matchMedia('(pointer: coarse)').matches;
}

function applyBook() {
  const on = !!state.article && bookCapable();
  // Keep the reading position across the switch: page fraction <-> scroll fraction.
  const range = scroller.scrollHeight - scroller.clientHeight;
  const frac = book ? (pages > 1 ? page / (pages - 1) : 0) : (range > 0 ? scroller.scrollTop / range : 0);
  const was = book;
  book = on;
  wrap.classList.toggle('book', on);
  if (!on) {
    wrap.style.removeProperty('--book-h');
    pageEl.textContent = '';
    if (was) motion.scrollTop(scroller, frac * (scroller.scrollHeight - scroller.clientHeight));
    return;
  }
  const pad = parseFloat(getComputedStyle(scroller).paddingTop) || 0;
  wrap.style.setProperty('--book-h', Math.max(240, scroller.clientHeight - pad - 28) + 'px');
  motion.scrollTop(scroller, 0);
  pageStep = wrap.clientWidth + BOOK_GAP;
  pages = Math.max(1, Math.ceil((wrap.scrollWidth + BOOK_GAP) / pageStep - 0.01));
  goPage(Math.round(frac * (pages - 1)), false);
}

function goPage(n, animate = true) {
  page = Math.min(Math.max(n, 0), pages - 1);
  const x = page * pageStep;
  if (animate && motion.enabled()) window.gsap.to(wrap, { scrollLeft: x, duration: 0.45, ease: 'power3.out' });
  else wrap.scrollLeft = x;
  pageEl.textContent = pages > 1 ? `${page + 1}/${pages}` : '';
  // Past the first page the article header is gone: show the feed name.
  feedTitleEl.classList.toggle('on', page > 0);
}

function setupBook() {
  let x0 = 0, y0 = 0, t0 = 0;
  wrap.addEventListener('touchstart', (e) => {
    if (!book || e.touches.length !== 1) return;
    x0 = e.touches[0].clientX; y0 = e.touches[0].clientY; t0 = e.timeStamp;
  }, { passive: true });
  wrap.addEventListener('touchend', (e) => {
    if (!book) return;
    const t = e.changedTouches[0];
    const dx = t.clientX - x0, dy = t.clientY - y0;
    if (Math.abs(dx) > 40 && Math.abs(dx) > Math.abs(dy) * 1.5 && e.timeStamp - t0 < 800) {
      goPage(page + (dx < 0 ? 1 : -1));
      if (e.cancelable) e.preventDefault(); // no click after a swipe
    }
  });
  wrap.addEventListener('click', (e) => {
    if (!book || e.target.closest('a, button, video, audio')) return;
    const r = wrap.getBoundingClientRect();
    const x = (e.clientX - r.left) / r.width;
    if (x > 0.7) goPage(page + 1);
    else if (x < 0.3) goPage(page - 1);
  });
  document.addEventListener('keydown', (e) => {
    if (!book || (state.cols === 1 && state.view !== 'reader')) return;
    if (e.target.closest && e.target.closest('input, textarea')) return;
    if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') { e.preventDefault(); goPage(page + 1); }
    else if (e.key === 'ArrowLeft' || e.key === 'PageUp') { e.preventDefault(); goPage(page - 1); }
  });
  // Unfolding, rotating, hiding the list, changing font or size: re-paginate.
  let timer = 0;
  const later = () => { clearTimeout(timer); timer = setTimeout(applyBook, 150); };
  new ResizeObserver(later).observe(scroller);
  onPrefsChange(later);
}
