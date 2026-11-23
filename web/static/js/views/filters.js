// "Filtri per parole": hide or mark as read the articles that talk about
// something (football on a news site). The words match whole, without
// caring for capitals and accents; "juve*" also matches "Juventus".

import { locale, t } from '../i18n.js';
import { del, get, post } from '../api.js';
import { state, findFeed, findFolder } from '../state.js';
import { actionSheet, confirmDialog, debounce, h, icon, openSheet, picker, showError, tile, toast, toggleSwitch } from '../ui.js';
import * as motion from '../motion.js';
import * as offline from '../offline/index.js';
import { segmented } from './settings.js';
import { sourcesChanged } from './sources.js';

const numFmt = new Intl.NumberFormat(locale);

async function loadFilters() {
  const r = await get('/api/filters');
  return r.filters;
}

// The filters that act on a feed: its own, its folder's, and the general ones.
export async function filtersFor(feed) {
  try {
    const list = await loadFilters();
    return list.filter((f) => (f.feed_id ? f.feed_id === feed.id : f.folder_id ? f.folder_id === feed.folder_id : true));
  } catch { return []; }
}

function scopeLabel(f) {
  if (f.feed_id) { const r = findFeed(f.feed_id); return r ? r.feed.title : t('A feed'); }
  if (f.folder_id) { const c = findFolder(f.folder_id); return c ? t('Folder {0}', c.name) : t('A folder'); }
  return t('All feeds');
}

function describe(f) {
  const parts = [f.action === 'hide' ? t('Hides') : t('Marks as read'), scopeLabel(f)];
  if (f.in_body) parts.push(t('also in the text'));
  let txt = parts.join(', ');
  if (f.action === 'hide' && f.hidden) txt += f.hidden === 1 ? t('. 1 article hidden') : t('. {0} articles hidden', numFmt.format(f.hidden));
  return txt;
}

// After a change the counters, the list and the offline copy follow.
async function refreshAll() {
  await sourcesChanged();
  offline.syncNow();
}

export function openFilters({ feed = null, onChange = null } = {}) {
  const ref = {};
  const list = h('div', { class: 'flex flex-col gap-3' });

  const render = async (animate) => {
    let filters;
    try { filters = await loadFilters(); } catch (e) { showError(e); return; }
    if (feed) filters = filters.filter((f) => (f.feed_id ? f.feed_id === feed.id : f.folder_id ? f.folder_id === feed.folder_id : true));
    if (!filters.length) {
      // One child only: .group draws a line between its children.
      list.replaceChildren(h('div', { class: 'group' }, h('div', { class: 'px-4 py-6 text-center' },
        h('div', { class: 'mx-auto w-fit' }, tile('o-funnel', 'plain')),
        h('p', { class: 'text-[1.0625rem] font-semibold mt-3', text: t('No filters') }),
        h('p', { class: 'text-muted text-[0.9375rem] mt-1', text: feed ? t('No filter acts on this feed.') : t('All articles arrive as the sites publish them.') }))));
    } else {
      list.replaceChildren(h('div', { class: 'group' }, ...filters.map((f) => h('button', {
        class: 'row-btn px-3 py-2.5 text-left', onclick: () => filterMenu(f),
      },
      tile(f.action === 'hide' ? 'o-eye-slash' : 'o-check-circle', f.action === 'hide' ? 'rose' : 'mint', 'sm'),
      h('div', { class: 'flex-1 min-w-0' },
        h('div', { class: 'text-[1rem] font-medium break-words', text: f.terms.join(', ') }),
        h('div', { class: 'text-[0.8125rem] text-muted', text: describe(f) })),
      icon('o-ellipsis-horizontal', 'w-5 h-5 text-muted flex-none')))));
    }
    if (animate) motion.swapIn(list.children, 0);
    if (onChange) onChange();
  };

  const filterMenu = async (f) => {
    const choice = await actionSheet({
      title: f.terms.join(', '), message: describe(f),
      actions: [{ label: t('Delete filter'), icon: 'o-trash', destructive: true, value: 'delete' }],
    });
    if (!choice) return;
    if (f.hidden) {
      const ok = await confirmDialog({
        title: t('Delete the filter?'),
        message: f.hidden === 1 ? t('The hidden article will be back among the unread.') : t('The {0} hidden articles will be back among the unread.', numFmt.format(f.hidden)),
        okLabel: t('Delete'), destructive: true, icon: 'o-funnel',
      });
      if (!ok) return;
    }
    try {
      const r = await del(`/api/filters/${f.id}`);
      toast(r.restored ? t('Filter deleted, {0} articles visible again', numFmt.format(r.restored)) : t('Filter deleted'));
      await render(true);
      await refreshAll();
    } catch (e) { showError(e); }
  };

  render(false);
  const body = h('div', { class: 'min-h-full' },
    h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
      h('div', { class: 'w-20' }),
      h('div', { class: 'flex-1 text-center sheet-title', text: t('Keyword filters') }),
      h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') })),
    h('div', { class: 'px-4 pb-10 flex flex-col gap-4' },
      h('p', { class: 'text-[0.9375rem] text-muted px-1', text: feed
        ? t('The filters acting on "{0}": its own, its folder\'s and those for all feeds.', feed.title)
        : t('Articles containing these words are hidden or marked as read as soon as they arrive. Deleting a filter brings back what it hid.') }),
      list,
      h('button', { class: 'btn btn-primary w-full', onclick: () => newFilter({ feed, after: () => render(true) }) },
        icon('o-plus', 'w-5 h-5'), t('New filter'))));
  ref.sheet = openSheet(body, { full: true, wide: true });
}

function scopeOptions() {
  const s = state.sources || { feeds: [], folders: [] };
  const opts = [{ value: 'all', label: t('All feeds'), icon: 'o-inbox' }];
  s.folders.forEach((f) => opts.push({ value: 'folder:' + f.id, label: f.name, icon: 'o-folder' }));
  s.feeds.concat(...s.folders.map((f) => f.feeds))
    .sort((a, b) => a.title.localeCompare(b.title, locale))
    .forEach((f) => opts.push({ value: 'feed:' + f.id, label: f.title, icon: 'o-rss' }));
  return opts;
}

function newFilter({ feed, after }) {
  let action = 'hide';
  const words = h('input', {
    class: 'field', type: 'text', placeholder: t('football, premier league, chelsea*'), autocomplete: 'off',
    autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false', enterkeyhint: 'done',
  });
  const where = picker({ options: scopeOptions(), value: feed ? 'feed:' + feed.id : 'all', title: t('Where') });
  const inBody = toggleSwitch(false);
  const existing = toggleSwitch(true);
  const preview = h('div', { class: 'text-[0.875rem] text-muted px-1 min-h-5' });
  const errorBox = h('div', { class: 'text-danger text-sm px-1 hidden' });
  const create = h('button', { type: 'submit', class: 'btn btn-primary flex-1', text: t('Create filter') });

  const payload = () => {
    const [kind, raw] = String(where.value).split(':');
    return {
      terms: [words.value], action, in_body: inBody.checked, existing: existing.checked,
      feed_id: kind === 'feed' ? Number(raw) : 0, folder_id: kind === 'folder' ? Number(raw) : 0,
    };
  };

  // What the filter would catch now, while it is being written.
  let previewReq = 0;
  const updatePreview = debounce(async () => {
    const my = ++previewReq;
    if (!words.value.trim()) { preview.replaceChildren(); return; }
    try {
      const r = await post('/api/filters/preview', payload());
      if (my !== previewReq) return;
      if (!r.count) { preview.replaceChildren(t('It doesn\'t match any unread article now.')); return; }
      preview.replaceChildren(
        h('div', { text: r.count === 1 ? t('It matches 1 unread article now, for example:') : t('It matches {0} unread articles now, for example:', numFmt.format(r.count)) }),
        h('ul', { class: 'filter-sample' }, ...r.titles.map((title) => h('li', { class: 'truncate', text: title }))));
    } catch { /* offline: no preview */ }
  }, 300);
  words.addEventListener('input', updatePreview);
  where.addEventListener('change', updatePreview);
  inBody.addEventListener('change', updatePreview);

  const labelled = (label, control, hint) => h('label', { class: 'flex flex-col gap-1.5' },
    h('span', { class: 'field-label', text: label }), control,
    hint ? h('span', { class: 'text-[0.8125rem] text-muted px-1', text: hint }) : null);
  const row = (label, hint, sw) => h('div', { class: 'switch-row' },
    h('div', {}, h('div', { class: 'text-[1rem]', text: label }), hint ? h('div', { class: 'text-[0.8125rem] text-muted', text: hint }) : null), sw);

  const form = h('form', { class: 'px-5 pt-1 pb-3 flex flex-col gap-4', novalidate: true },
    h('div', { class: 'sheet-title pt-1', text: t('New filter') }),
    labelled(t('Words'), words, t('Separate them with a comma. Whole words count, ignoring capitals and accents; with * at the end, also the words starting like that (juve* matches Juventus).')),
    labelled(t('What to do'), segmented([['hide', t('Hide')], ['read', t('Mark as read|filter')]], action, (v) => { action = v; })),
    labelled(t('Where'), where),
    h('div', { class: 'group px-4' },
      row(t('Search the text too'), t('The title is usually enough: with the text the filter catches more, also by mistake.'), inBody),
      row(t('Apply to articles already here'), t('Otherwise it only applies to new ones.'), existing)),
    preview, errorBox,
    h('div', { class: 'flex gap-3 pt-1' },
      h('button', { type: 'button', class: 'btn btn-plain flex-1', text: t('Cancel'), onclick: () => s.close() }), create));

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    errorBox.classList.add('hidden');
    create.disabled = true;
    try {
      const r = await post('/api/filters', payload());
      s.close();
      const id = r.filter.id;
      const n = r.applied;
      const undo = async () => {
        try {
          if (r.undo) await post('/api/articles/mark-read/undo', { token: r.undo });
          await del(`/api/filters/${id}`);
          toast(t('Filter undone'));
          after();
          await refreshAll();
        } catch (err) { showError(err); }
      };
      const msg = !n ? t('Filter created')
        : action === 'hide' ? (n === 1 ? t('Filter created: 1 article hidden') : t('Filter created: {0} articles hidden', numFmt.format(n)))
          : (n === 1 ? t('Filter created: 1 article marked as read') : t('Filter created: {0} articles marked as read', numFmt.format(n)));
      toast(msg, '', { action: { label: t('Undo'), onClick: undo }, duration: 6000 });
      after();
      await refreshAll();
    } catch (err) {
      errorBox.textContent = err.message;
      errorBox.classList.remove('hidden');
      motion.shake(errorBox);
    } finally {
      create.disabled = false;
    }
  });
  const s = openSheet(form);
  if (!matchMedia('(pointer: coarse)').matches) setTimeout(() => words.focus(), 250);
}
