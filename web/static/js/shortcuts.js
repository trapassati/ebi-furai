// Keyboard shortcuts, for desktop and the iPad keyboard, as in the classic
// RSS readers. They never fire while typing in a field or with a modifier
// key (Cmd, Ctrl, Alt) held, so the browser's own shortcuts keep working.

import { t } from './i18n.js';
import { state } from './state.js';
import { closeTopSheet, h, openSheet, sheetOpen, toast } from './ui.js';
import { readerActions } from './views/reader.js';
import { markAllReadShortcut, openListSearch } from './views/list.js';

export const SHORTCUTS = [
  ['J', t('Next article')],
  ['K', t('Previous article')],
  ['M', t('Mark as read or unread')],
  ['S', t('Add to or remove from starred')],
  ['F', t('Full text')],
  ['V', t('Open the original article')],
  ['L', t('Listen to the article')],
  ['⇧ A', t('Mark all as read')],
  ['/', t('Search')],
  ['R', t('Refresh feeds')],
  ['1 – 4', t('Unread, Today, Starred, All')],
  ['B', t('Show or hide the column')],
  ['Esc', t('Close or go back')],
  ['?', t('This list')],
];

const COLLECTIONS = ['unread', 'today', 'starred', 'all'];

function typing(t) {
  return t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
}

function listVisible() {
  return state.cols > 1 || state.view === 'list';
}

export function showShortcuts() {
  const ref = {};
  const rows = SHORTCUTS.map(([k, label]) => h('div', { class: 'flex items-center gap-4 px-4 py-2.5' },
    h('kbd', { class: 'kbd', text: k }), h('span', { class: 'text-[1rem]', text: label })));
  ref.s = openSheet(h('div', { class: 'px-4 pb-6' },
    h('div', { class: 'flex items-center h-14' },
      h('div', { class: 'flex-1 sheet-title', text: t('Keyboard shortcuts') }),
      h('button', { class: 'icon-btn on font-medium text-[1.0625rem] px-3', text: t('Done'), onclick: () => ref.s.close() })),
    h('div', { class: 'group' }, rows)), { wide: true });
}

export function installShortcuts(ctx) {
  document.addEventListener('keydown', (e) => {
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || e.isComposing) return;
    if (e.key === 'Escape') {
      if (closeTopSheet()) { e.preventDefault(); return; }
      if (typing(e.target)) { e.target.blur(); return; }
      if (state.view === 'sources') return;
      e.preventDefault();
      if (state.cols > 1 && state.articleId) ctx.closeArticle();
      else ctx.back(state.view === 'reader' ? 'reader' : 'list');
      return;
    }
    if (typing(e.target) || sheetOpen() || !state.me) return;
    const key = e.key;
    const article = !!state.article && state.articleId;
    let done = true;
    if (key === 'j' || key === 'k') {
      if (article) (key === 'j' ? readerActions.next : readerActions.prev)();
      else if (listVisible() && state.list.items.length) ctx.openArticle(state.list.items[0].id);
      else done = false;
    } else if (key === 'm' && article) readerActions.toggleRead();
    else if (key === 's' && article) readerActions.toggleStar();
    else if (key === 'f' && article) readerActions.toggleFull();
    else if (key === 'v' && article) readerActions.openOriginal();
    else if (key === 'l' && article) readerActions.listen();
    else if (key === 'A' && e.shiftKey && listVisible()) markAllReadShortcut();
    else if (key === '/') {
      const home = document.querySelector('#pane-sources input[type="search"], #pane-sources input');
      if (home && home.offsetParent) home.focus();
      else if (listVisible()) openListSearch();
      else done = false;
    } else if (key === 'r') {
      toast(t('Refreshing feeds…'));
      ctx.refreshAll().then(() => ctx.reloadSources());
    }
    else if (key >= '1' && key <= '4') ctx.selectSource(COLLECTIONS[Number(key) - 1]);
    else if (key === 'b' && state.cols > 1) ctx.toggleSidebar();
    else if (key === '?') showShortcuts();
    else done = false;
    if (done) e.preventDefault();
  });
}
