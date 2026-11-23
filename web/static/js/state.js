import { t } from './i18n.js';
// Shared application state.

function loadExpanded() {
  try { return new Set(JSON.parse(localStorage.getItem('expandedFolders') || '[]')); } catch (e) { return new Set(); }
}

export const state = {
  me: null,
  sources: null,
  cols: 1,
  view: 'sources', // sources | list | reader
  source: 'unread', // today | unread | starred | all | feed:N | folder:N
  articleId: null,
  expanded: loadExpanded(),
  list: {
    items: [],
    cursor: '',
    maxId: 0,
    loading: false,
    done: false,
    query: '',
    searchOpen: false,
    loadedAt: 0,
    req: 0,
  },
  article: null,
};

export function saveExpanded() {
  try { localStorage.setItem('expandedFolders', JSON.stringify([...state.expanded])); } catch (e) { /* ignore */ }
}

// Finds a feed in the sources tree.
export function findFeed(id) {
  const s = state.sources;
  if (!s) return null;
  for (const f of s.feeds) if (f.id === id) return { feed: f, folder: null };
  for (const folder of s.folders) {
    for (const f of folder.feeds) if (f.id === id) return { feed: f, folder };
  }
  return null;
}

export function findFolder(id) {
  return state.sources ? state.sources.folders.find((f) => f.id === id) || null : null;
}

// Human readable name of a source.
export function sourceTitle(src) {
  switch (src) {
    case 'today': return t('Today');
    case 'unread': return t('Unread');
    case 'starred': return t('Starred');
    case 'all': return t('All articles');
    case 'saved': return t('Read later');
  }
  const [kind, raw] = src.split(':');
  const id = Number(raw);
  if (kind === 'feed') { const r = findFeed(id); return r ? r.feed.title : 'Feed'; }
  if (kind === 'folder') { const f = findFolder(id); return f ? f.name : t('Folder'); }
  return '';
}

export function todayStart() {
  const d = new Date();
  return Math.floor(new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() / 1000);
}
