// Sources pane: smart feeds, folders and feeds.

import { locale, t } from '../i18n.js';
import { del, patch, post, put } from '../api.js';
import { pullToRefresh, longPress } from '../gestures.js';
import { state, saveExpanded } from '../state.js';
import {
  actionSheet, clear, collapsingTitle, confirmDialog, feedIcon, formDialog, h, icon, openSheet, picker, promptDialog, showError, tile, toast,
} from '../ui.js';
import { openSettings } from './settings.js';
import { openFeedHealth } from './health.js';
import { openFeedSettings } from './feedsettings.js';
import { status as offlineStatus } from '../offline/index.js';
import * as motion from '../motion.js';

let ctx;
let content;
let firstRender = true;
// What the previous render showed, to animate the differences.
let prevNums = new Map(); // deck source -> count
let prevBars = new Map(); // row key -> progress %
let prevKeys = new Set();

const TONES = ['sky', 'aurora', 'rose', 'sunset', 'mint', 'indigo'];
const TONE_VAR = { sky: '--k-blue', aurora: '--k-purple', rose: '--k-pink', sunset: '--k-orange', mint: '--k-teal', indigo: '--k-indigo' };
const toneFor = (id) => TONES[id % TONES.length];
const dateFmt = new Intl.DateTimeFormat(locale, { weekday: 'long', day: 'numeric', month: 'long' });

export function mountSources(pane, context) {
  ctx = context;
  const smallTitle = h('div', { class: 'bar-small-title home-small pl-24', 'aria-hidden': 'true' }, wordmark());
  const bar = h('header', { class: 'bar' },
    h('div', { class: 'bar-inner' },
      smallTitle,
      h('button', { class: 'gbtn glass', 'aria-label': t('Add'), onclick: addMenu }, icon('o-plus')),
      h('button', { class: 'gbtn glass', 'aria-label': t('Settings'), onclick: () => openSettings(ctx) }, icon('o-cog-6-tooth'))));
  content = h('div', { class: 'pb-12' });
  content.addEventListener('keydown', (e) => {
    const row = e.target.closest('.feed-row');
    if (row && e.target === row && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); row.click(); }
  });
  const scroller = h('div', { class: 'pane-scroll' }, h('div', { class: 'safe-x' }, content));
  pane.append(bar, scroller);
  collapsingTitle(scroller, smallTitle, () => 44);
  pullToRefresh(scroller, () => ctx.refreshAll());
  motion.smoothScroll(scroller);
}

function isSelected(src) {
  return state.source === src && (state.cols > 1 || state.view !== 'sources');
}

// The selection depends on the view and the columns too: on a phone, back
// on the home nothing is selected. It is updated in place whenever they
// change, since the home is not redrawn on the way back (it was drawn while
// the list was on screen, with the source selected).
export function syncSelected() {
  if (!content) return;
  content.querySelectorAll('[data-sel], .feed-row[data-key]').forEach((el) => {
    el.classList.toggle('selected', isSelected(el.dataset.sel || el.dataset.key));
  });
}

// The name in the home: "ebi · furai", the middle dot (as the Japanese
// nakaguro between words) in the orange of the shrimp's tail. The katakana
// エビフライ stays on the launch screen.
function wordmark() {
  return h('span', { class: 'wordmark' }, 'ebi', h('span', { class: 'wm-dot', 'aria-hidden': 'true' }), 'furai');
}

function header(s) {
  let sub = dateFmt.format(new Date());
  sub = sub.charAt(0).toUpperCase() + sub.slice(1);
  let line = h('div', { class: 'page-sub', text: sub });
  if (!offlineStatus.online) {
    let msg = t('You\'re offline');
    if (offlineStatus.pending) msg += offlineStatus.pending === 1 ? t(', 1 change to send') : t(', {0} changes to send', offlineStatus.pending);
    line = h('div', { class: 'page-sub flex items-center gap-2' }, h('span', { class: 'offline-dot' }), msg);
  } else if (offlineStatus.syncing) {
    line = h('div', { class: 'page-sub flex items-center gap-2' }, icon('o-arrow-path', 'w-4 h-4 spin'), t('Syncing…'));
  }
  // App name with the shrimp (transparent).
  const v = encodeURIComponent(state.me.version || '');
  return h('div', { class: 'home-head px-5 pt-1' },
    h('h1', { class: 'page-title home-title', 'aria-label': state.me.app_name },
      h('img', { class: 'home-logo', src: `/static/icons/shrimp.png?v=${v}`, alt: '' }),
      wordmark()),
    line);
}

function searchPill() {
  const input = h('input', {
    type: 'search', placeholder: t('Search articles…'), enterkeyhint: 'search',
    autocomplete: 'off', autocorrect: 'off', autocapitalize: 'off', spellcheck: 'false', 'aria-label': t('Search articles'),
  });
  const form = h('form', { class: 'search-pill', role: 'search' },
    input, h('button', { type: 'submit', 'aria-label': t('Search') }, icon('o-magnifying-glass')));
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    const q = input.value.trim();
    if (!q) return;
    input.blur();
    ctx.searchAll(q);
  });
  return form;
}

function deckCard(src, iconName, tone, name, count, label) {
  return h('button', {
    class: 'deck-card' + (isSelected(src) ? ' selected' : ''),
    dataset: { sel: src },
    'aria-label': count === null ? name : `${name}, ${label}`,
    onclick: () => ctx.selectSource(src),
  },
  h('div', { class: 'deck-top' }, tile(iconName, tone), count === null ? null : h('span', { class: 'deck-num', dataset: { src }, text: String(count) })),
  h('div', { class: 'deck-name', text: name }));
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

function badge(feed, nested) {
  const tone = toneFor(feed.id);
  if (!feed.has_icon) {
    return h('span', { class: `tile tile-${tone} ${nested ? 'sm' : ''}` },
      h('span', { class: 'text-[0.9375rem] font-semibold uppercase', text: (feed.title || '?').trim().charAt(0) }));
  }
  const b = h('span', { class: 'feed-badge' }, feedIcon(feed.id, true, feed.title, 'big'));
  b.style.color = `var(${TONE_VAR[tone]})`;
  return b;
}

function rowBody(name, count, pct, color, meta, error) {
  const bar = h('span');
  bar.dataset.pct = String(Math.max(pct, count ? 4 : 0));
  bar.style.width = bar.dataset.pct + '%';
  bar.style.background = `var(${color})`;
  return h('div', { class: 'flex-1 min-w-0' },
    h('div', { class: 'flex items-center gap-2' },
      h('span', { class: 'name flex-1 truncate', text: name }),
      error ? icon('o-exclamation-triangle', 'w-4 h-4 text-danger flex-none') : null,
      h('span', { class: 'count', text: count ? String(count) : '' })),
    h('div', { class: 'progress' }, bar),
    h('div', { class: 'meta mt-1.5' }, h('span', { class: 'truncate ' + (error ? 'text-danger' : ''), text: meta })));
}

function feedRow(feed, nested, max) {
  const src = 'feed:' + feed.id;
  const meta = feed.error_count ? t('Error: {0}', feed.last_error) : (feed.unread ? plural(feed.unread, t('unread|lower'), t('unread|lower')) : t('All read'));
  const row = h('div', {
    dataset: { key: src },
    class: 'row-btn feed-row ' + (nested ? 'nested ' : '') + (isSelected(src) ? 'selected' : ''),
    role: 'button', tabindex: '0',
    title: feed.error_count ? meta : undefined, // on the desktop the line under the name is hidden
    onclick: () => { if (!row.dataset.longpress) ctx.selectSource(src); },
  },
  badge(feed, nested),
  rowBody(feed.title, feed.unread, (feed.unread / max) * 100, TONE_VAR[toneFor(feed.id)], meta, feed.error_count > 0),
  );
  if (feed.auth_user) row.querySelector('.name').after(icon('o-lock-closed', 'w-3.5 h-3.5 text-muted flex-none'));
  longPress(row, () => feedMenu(feed));
  return row;
}

function folderRow(folder, max) {
  const src = 'folder:' + folder.id;
  const open = state.expanded.has(folder.id);
  const toggle = h('button', {
    class: 'icon-btn -mr-2', 'aria-label': open ? t('Close folder') : t('Open folder'), 'aria-expanded': String(open),
    onclick: (e) => {
      e.stopPropagation();
      const nestedRows = () => content.querySelectorAll(`[data-folder="${folder.id}"] .feed-row.nested`);
      if (open) {
        state.expanded.delete(folder.id);
        saveExpanded();
        motion.collapse(nestedRows()).then(renderSources);
      } else {
        state.expanded.add(folder.id);
        saveExpanded();
        renderSources();
        motion.expand(nestedRows());
      }
    },
  }, icon(open ? 'o-chevron-up' : 'o-chevron-down', 'w-5 h-5'));
  const meta = plural(folder.feeds.length, t('feed|one'), t('feeds|lower')) + (folder.unread ? t(', {0} unread', folder.unread) : '');
  const row = h('div', {
    dataset: { key: src },
    class: 'row-btn feed-row ' + (isSelected(src) ? 'selected' : ''),
    role: 'button', tabindex: '0',
    onclick: () => { if (!row.dataset.longpress) ctx.selectSource(src); },
  },
  tile(open ? 'o-folder-open' : 'o-folder', 'aurora'),
  rowBody(folder.name, folder.unread, (folder.unread / max) * 100, '--k-purple', meta, false),
  toggle);
  longPress(row, () => folderMenu(folder));
  const wrap = h('div', { dataset: { folder: folder.id } }, row);
  if (open) folder.feeds.forEach((f) => wrap.append(feedRow(f, true, max)));
  return wrap;
}

export function renderSources() {
  if (!content) return;
  const s = state.sources;
  clear(content);
  if (!s) return;
  const deck = h('div', { class: 'deck' },
    deckCard('unread', 'o-inbox', 'sky', t('Unread'), s.unread, plural(s.unread, t('article to read'), t('articles to read'))),
    deckCard('today', 'o-calendar-days', 'aurora', t('Today'), s.today, plural(s.today, t('new to read|one'), t('new to read'))),
    deckCard('starred', 's-star', 'sunset', t('Starred'), s.starred, plural(s.starred, t('saved|one'), t('saved'))),
    deckCard('all', 'o-squares-2x2', 'mint', t('All'), null, ''));
  content.append(
    header(s),
    searchPill(),
    h('div', { class: 'section-title', text: t('Collections') }),
    deck,
  );
  if (s.folders.length || s.feeds.length) {
    const all = s.feeds.concat(...s.folders.map((f) => f.feeds));
    const max = Math.max(1, ...s.folders.map((f) => f.unread), ...all.map((f) => f.unread));
    content.append(h('div', { class: 'section-title' }, t('Feeds'), h('small', { text: plural(all.length, t('source'), t('sources')) })));
    // Feeds that keep failing are shown in one place, with what to do.
    const failing = all.filter((f) => f.error_count > 0).length;
    if (failing) {
      content.append(h('button', { class: 'health-pill', onclick: () => openFeedHealth() },
        icon('o-exclamation-triangle', 'w-5 h-5 flex-none'),
        h('span', { class: 'flex-1 text-left', text: failing === 1 ? t('1 feed isn\'t updating') : t('{0} feeds aren\'t updating', failing) }),
        icon('o-chevron-right', 'w-4 h-4 flex-none opacity-70')));
    }
    const list = h('div', { class: 'feed-rows' });
    s.folders.forEach((f) => list.append(folderRow(f, max)));
    s.feeds.forEach((f) => list.append(feedRow(f, false, max)));
    content.append(list);
  } else {
    content.append(h('div', { class: 'px-8 pt-12 text-center' },
      h('div', { class: 'empty-art' }, icon('o-rss')),
      h('p', { class: 'text-[1.0625rem] font-semibold mb-1', text: t('No feeds yet') }),
      h('p', { class: 'text-muted mb-6', text: t('Add the first one from its address, or import an OPML file from the settings.') }),
      h('button', { class: 'btn btn-primary', onclick: addFeedDialog }, icon('o-plus', 'w-5 h-5'), t('Add feed'))));
  }
  const rowKeys = new Set();
  content.querySelectorAll('[data-key]').forEach((el) => rowKeys.add(el.dataset.key));
  if (firstRender) {
    firstRender = false;
    motion.stagger(deck.children, { y: 14 });
    motion.stagger(content.querySelectorAll('.feed-rows > *'), { delay: 0.15 });
    motion.bars(content.querySelectorAll('.progress > span'));
  } else {
    // Counts roll to their new value, bars resize, new feeds slide in.
    content.querySelectorAll('.deck-num').forEach((el) => {
      const before = prevNums.get(el.dataset.src);
      const now = Number(el.textContent);
      if (before != null && before !== now) motion.countUp(el, now, { from: before });
    });
    content.querySelectorAll('[data-key]').forEach((row) => {
      const bar = row.querySelector('.progress > span');
      const before = prevBars.get(row.dataset.key);
      if (bar && before != null && Math.abs(before - Number(bar.dataset.pct)) > 0.5) motion.widthFrom(bar, before);
    });
    const added = [...content.querySelectorAll('[data-key]')].filter((r) => !prevKeys.has(r.dataset.key) && !r.classList.contains('nested'));
    if (added.length && added.length < 5) motion.stagger(added, { y: 10 });
  }
  prevNums = new Map([...content.querySelectorAll('.deck-num')].map((el) => [el.dataset.src, Number(el.textContent)]));
  prevBars = new Map([...content.querySelectorAll('[data-key]')].map((r) => {
    const bar = r.querySelector('.progress > span');
    return [r.dataset.key, bar ? Number(bar.dataset.pct) : 0];
  }));
  prevKeys = rowKeys;
  window.dispatchEvent(new Event('sources:rendered'));
}

// ---- menus ----

function addMenu() {
  actionSheet({
    actions: [
      { label: t('Add feed'), icon: 'o-rss', tone: 'aurora', onSelect: addFeedDialog },
      { label: t('New folder'), icon: 'o-folder-plus', tone: 'sky', onSelect: newFolder },
    ],
  });
}

async function newFolder() {
  const name = await promptDialog({ title: t('New folder'), placeholder: t('Name'), okLabel: t('Create') });
  if (!name || !name.trim()) return null;
  try {
    const r = await post('/api/folders', { name: name.trim() });
    await ctx.afterSourcesChanged();
    return r.id;
  } catch (e) {
    showError(e);
    return null;
  }
}

function folderOptions(selected) {
  return [{ value: 0, label: t('No folder') }].concat(
    (state.sources ? state.sources.folders : []).map((f) => ({ value: f.id, label: f.name })),
  ).map((o) => ({ ...o, icon: o.value ? 'o-folder' : 'o-inbox', selected: o.value === selected }));
}

function addFeedDialog() {
  const urlInput = h('input', {
    class: 'field', type: 'url', inputmode: 'url', placeholder: t('https://site.com or feed URL'),
    autocomplete: 'off', autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false',
  });
  const current = state.source.startsWith('folder:') ? Number(state.source.split(':')[1]) : 0;
  const folderSel = picker({ options: folderOptions(current), value: current, title: t('Folder') });
  const error = h('div', { class: 'text-danger text-sm px-1' });
  const results = h('div', { class: 'flex flex-col gap-2' });
  // Credentials for feeds behind a password: shown on request, or by
  // themselves when the site answers that it needs them.
  const userInput = h('input', { class: 'field', type: 'text', placeholder: t('Username'), autocomplete: 'off', autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false' });
  const passInput = h('input', { class: 'field', type: 'password', placeholder: 'Password', autocomplete: 'new-password' });
  const authBox = h('div', { class: 'flex flex-col gap-2 hidden' },
    h('span', { class: 'field-label', text: t('Feed sign-in') }),
    h('div', { class: 'field-wrap' }, icon('o-user-circle'), userInput),
    h('div', { class: 'field-wrap' }, icon('o-lock-closed'), passInput),
    h('div', { class: 'text-[0.75rem] text-muted px-1', text: t('Stored encrypted on the server, used only for this feed and only over https.') }));
  const authToggle = h('button', { type: 'button', class: 'self-start text-[0.875rem] font-medium text-accent px-1 min-h-11 inline-flex items-center gap-1.5' },
    icon('o-lock-closed', 'w-4 h-4'), t('The feed needs a username and password'));
  const showAuth = () => {
    if (!authBox.classList.contains('hidden')) return;
    authToggle.remove();
    authBox.classList.remove('hidden');
    motion.expand([authBox]);
    setTimeout(() => userInput.focus(), 250);
  };
  authToggle.addEventListener('click', showAuth);
  const submit = h('button', { type: 'submit', class: 'btn btn-primary flex-1', text: t('Add') });
  const form = h('form', { class: 'px-5 pt-1 pb-3 flex flex-col gap-3', novalidate: true },
    h('div', { class: 'sheet-title pt-1', text: t('Add feed') }),
    h('div', { class: 'text-center text-[0.875rem] text-muted -mt-1 mb-1', text: t('The site\'s address works too: the feed is found by itself.') }),
    h('div', { class: 'field-wrap' }, icon('o-link'), urlInput),
    h('div', { class: 'flex flex-col gap-1.5' }, h('span', { class: 'field-label', text: t('Folder') }), folderSel),
    authToggle, authBox,
    error, results,
    h('div', { class: 'flex gap-3 pt-1' },
      h('button', { type: 'button', class: 'btn btn-plain flex-1', text: t('Cancel'), onclick: () => sheet.close() }),
      submit));

  const add = async (url) => {
    error.textContent = '';
    submit.disabled = true;
    submit.textContent = t('Searching…');
    try {
      const body = { url, folder_id: Number(folderSel.value) };
      if (!authBox.classList.contains('hidden') && userInput.value.trim()) {
        body.username = userInput.value.trim();
        body.password = passInput.value;
      }
      const r = await post('/api/feeds', body);
      if (r.candidates) {
        clear(results);
        results.append(h('div', { class: 'text-[0.8125rem] text-muted px-1', text: t('The site offers several feeds, pick one:') }));
        r.candidates.forEach((c) => results.append(h('button', {
          type: 'button', class: 'group px-4 py-3 text-left flex items-center gap-3',
          onclick: () => add(c.url),
        }, tile('o-rss', 'aurora', 'sm'), h('div', { class: 'min-w-0' },
          h('div', { class: 'font-medium', text: c.title }), h('div', { class: 'text-[0.75rem] text-muted break-all', text: c.url })))));
        return;
      }
      sheet.close();
      toast(t('Feed added'));
      await ctx.afterSourcesChanged();
      ctx.selectSource('feed:' + r.feed.id);
    } catch (e) {
      if (e.status === 409 && e.data && e.data.feed) {
        sheet.close();
        toast(e.message);
        ctx.selectSource('feed:' + e.data.feed.id);
        return;
      }
      if (e.data && e.data.auth_required) showAuth();
      error.textContent = e.message;
      motion.shake(error);
    } finally {
      submit.disabled = false;
      submit.textContent = t('Add');
    }
  };
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    if (urlInput.value.trim()) add(urlInput.value.trim());
  });
  const sheet = openSheet(form);
  setTimeout(() => urlInput.focus(), 300);
}

// Set, change or remove the credentials of a feed. The saved password is
// never shown: leaving it empty while changing the user is not allowed.
async function feedAuthDialog(feed) {
  if (feed.auth_user) {
    const choice = await actionSheet({
      title: t('Feed sign-in'), message: t('Username: {0}', feed.auth_user),
      actions: [
        { label: t('Change username or password'), icon: 'o-key', value: 'edit' },
        { label: t('Remove sign-in'), icon: 'o-trash', destructive: true, value: 'remove' },
      ],
    });
    if (choice === 'remove') {
      const ok = await confirmDialog({ title: t('Remove the sign-in?'), message: t('This feed\'s username and password will be deleted.'), okLabel: t('Remove'), destructive: true, icon: 'o-lock-closed' });
      if (!ok) return;
      try { await put(`/api/feeds/${feed.id}/auth`, { username: '', password: '' }); toast(t('Sign-in removed')); await ctx.afterSourcesChanged(); } catch (e) { showError(e); }
      return;
    }
    if (choice !== 'edit') return;
  }
  let result = null;
  const v = await formDialog({
    title: t('Feed sign-in'),
    message: t('For password-protected feeds (HTTP authentication). They are stored encrypted on the server.'),
    fields: [
      { name: 'username', label: t('Username'), value: feed.auth_user || '' },
      { name: 'password', label: 'Password', type: 'password', autocomplete: 'new-password' },
    ],
    okLabel: t('Save and try'),
    validate: async (vals) => {
      if (!vals.username.trim()) return t('Enter the username');
      const r = await put(`/api/feeds/${feed.id}/auth`, { username: vals.username.trim(), password: vals.password });
      if (r.feed.error_count) return r.feed.last_error;
      result = r;
      return null;
    },
  });
  if (v && result) {
    toast(t('Sign-in works, feed updated'));
    await ctx.afterSourcesChanged();
  }
}

async function moveFeed(feed) {
  const folders = state.sources.folders;
  const actions = [{ label: t('No folder'), icon: 'o-inbox', checked: !feed.folder_id, value: 0 }]
    .concat(folders.map((f) => ({ label: f.name, icon: 'o-folder', checked: feed.folder_id === f.id, value: f.id })))
    .concat([{ label: t('New folder…'), icon: 'o-folder-plus', value: -1 }]);
  const choice = await actionSheet({ title: t('Move to'), message: feed.title, actions });
  if (!choice) return;
  let target = choice.value;
  if (target === -1) {
    target = await newFolder();
    if (!target) return;
  }
  try {
    await patch(`/api/feeds/${feed.id}`, { folder_id: target });
    if (target) state.expanded.add(target);
    saveExpanded();
    await ctx.afterSourcesChanged();
  } catch (e) { showError(e); }
}

async function markSourceRead(src) {
  try {
    const r = await post('/api/articles/mark-read', { source: src });
    toast(r.marked ? t('{0} articles marked as read', r.marked) : t('Nothing to mark'));
    await ctx.afterSourcesChanged();
  } catch (e) { showError(e); }
}

// changeFeedURL moves a feed to a new address (the site changed it): the
// address is checked like a new feed; articles and stars stay.
async function changeFeedURL(feed, url = null) {
  const v = url ?? await promptDialog({
    title: t('Change address'), message: t('The address of the feed or of the site: the feed is found by itself.'),
    value: feed.url, placeholder: 'https://…', type: 'url', okLabel: t('Change'),
  });
  if (v === null || !v.trim() || v.trim() === feed.url) return;
  try {
    const r = await put(`/api/feeds/${feed.id}/url`, { url: v.trim() });
    if (r.candidates) {
      const pick = await actionSheet({
        title: t('Which feed?'), message: t('The site publishes more than one'),
        actions: r.candidates.map((c) => ({ label: c.title || c.url, icon: 'o-rss', url: c.url })),
      });
      if (pick) await changeFeedURL(feed, pick.url);
      return;
    }
    if (r.feed.error_count) toast(t('Address changed, but the feed doesn\'t respond: ') + r.feed.last_error, 'error');
    else toast(t('Address updated'));
    await ctx.afterSourcesChanged();
  } catch (e) { showError(e); }
}

export function feedMenu(feed) {
  const actions = [
    {
      label: t('Refresh now'), icon: 'o-arrow-path', onSelect: async () => {
        try {
          const r = await post(`/api/feeds/${feed.id}/refresh`);
          if (r.feed.error_count) toast('Errore: ' + r.feed.last_error, 'error'); else toast(t('Feed updated'));
          await ctx.afterSourcesChanged();
        } catch (e) { showError(e); }
      },
    },
    { label: t('Mark all as read'), icon: 'o-check-circle', onSelect: () => markSourceRead('feed:' + feed.id) },
    { label: t('Feed settings…'), icon: 'o-adjustments-horizontal', onSelect: () => openFeedSettings(feed) },
    { label: t('Move to folder…'), icon: 'o-arrows-right-left', onSelect: () => moveFeed(feed) },
    { label: feed.auth_user ? t('Sign-in: {0}', feed.auth_user) : t('Sign-in with password…'), icon: 'o-lock-closed', onSelect: () => feedAuthDialog(feed) },
    { label: t('Change address…'), icon: 'o-link', onSelect: () => changeFeedURL(feed) },
  ];
  if (feed.site_url) actions.push({ label: t('Open the site'), icon: 'o-arrow-top-right-on-square', onSelect: () => window.open(feed.site_url, '_blank', 'noopener') });
  actions.push({
    label: t('Delete feed'), icon: 'o-trash', destructive: true, onSelect: async () => {
      const ok = await confirmDialog({
        title: t('Delete the feed?'), message: t('"{0}" and its articles (starred ones too) will be deleted.', feed.title),
        okLabel: t('Delete'), destructive: true,
      });
      if (!ok) return;
      try {
        await del(`/api/feeds/${feed.id}`);
        await motion.collapse(content.querySelectorAll(`[data-key="feed:${feed.id}"]`));
        toast(t('Feed deleted'));
        await ctx.afterSourcesChanged();
      } catch (e) { showError(e); }
    },
  });
  let message = feed.url;
  if (feed.error_count) message = t('Last error: {0} ({1} failed attempts)\n{2}', feed.last_error, feed.error_count, feed.url);
  actionSheet({ title: feed.title, message, actions });
}

function folderMenu(folder) {
  actionSheet({
    title: folder.name,
    actions: [
      { label: t('Mark all as read'), icon: 'o-check-circle', onSelect: () => markSourceRead('folder:' + folder.id) },
      {
        label: t('Rename'), icon: 'o-pencil-square', onSelect: async () => {
          const v = await promptDialog({ title: t('Rename folder'), value: folder.name });
          if (!v || !v.trim()) return;
          try { await patch(`/api/folders/${folder.id}`, { name: v.trim() }); await ctx.afterSourcesChanged(); } catch (e) { showError(e); }
        },
      },
      {
        label: t('Delete folder'), icon: 'o-trash', destructive: true, onSelect: async () => {
          const ok = await confirmDialog({
            title: t('Delete the folder?'), message: t('The feeds inside are not deleted: they move out of the folder.'),
            okLabel: t('Delete'), destructive: true,
          });
          if (!ok) return;
          try {
            await del(`/api/folders/${folder.id}`);
            await motion.collapse(content.querySelectorAll(`[data-folder="${folder.id}"]`));
            await ctx.afterSourcesChanged();
          } catch (e) { showError(e); }
        },
      },
    ],
  });
}

export { addFeedDialog };

// For the pages that change feeds outside the home (feed health).
export function sourcesChanged() {
  return ctx.afterSourcesChanged();
}
