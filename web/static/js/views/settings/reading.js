// Reading: how often feeds are refreshed, duplicates and the like.

import { t } from '../../i18n.js';
import { put } from '../../api.js';
import { getPrefs, setPref } from '../../prefs.js';
import { state } from '../../state.js';
import { h, picker, showError, toast, toggleSwitch } from '../../ui.js';

// "5 minutes", "1 hour", "6 hours", "1 day"
function intervalLabel(m) {
  if (m < 60) return t('{0} minutes', m);
  if (m < 1440) return m === 60 ? t('1 hour') : t('{0} hours', m / 60);
  return m === 1440 ? t('1 day') : t('{0} days', m / 1440);
}

// How often the server refreshes this user's feeds (stored on the server).
export function refreshControl() {
  const def = state.me.default_refresh_minutes || 20;
  const choices = [0, 5, 10, 15, 20, 30, 60, 120, 360, 720, 1440];
  const sel = picker({
    title: t('Refresh feeds every'),
    value: state.me.user.refresh_minutes || 0,
    options: choices.map((m) => ({ value: m, label: m === 0 ? t('{0} (default)', intervalLabel(def)) : intervalLabel(m) })),
  });
  sel.addEventListener('change', async () => {
    const minutes = Number(sel.value);
    try {
      await put('/api/me/refresh', { minutes });
      state.me.user.refresh_minutes = minutes;
      toast(t('Feeds refresh every {0}', intervalLabel(minutes || def)));
    } catch (e) { showError(e); }
  });
  return h('div', { class: 'flex flex-col gap-1.5' },
    h('span', { class: 'field-label', text: t('Refresh feeds every') }), sel,
    h('span', { class: 'text-[0.75rem] text-muted px-1', text: t('It applies to all your feeds. Pulling down the list refreshes them now.') }));
}

export function readingControls() {
  const thumbs = toggleSwitch(getPrefs().thumbs !== false);
  thumbs.addEventListener('change', () => setPref('thumbs', thumbs.checked));
  const sw = toggleSwitch(getPrefs().mark_on_scroll === true);
  sw.addEventListener('change', () => setPref('mark_on_scroll', sw.checked));
  // Account-wide (the server applies it when articles arrive).
  const dups = toggleSwitch(state.me.user.dedup !== false);
  dups.addEventListener('change', async () => {
    try {
      await put('/api/me/dedup', { on: dups.checked });
      state.me.user.dedup = dups.checked;
      toast(dups.checked ? t('Duplicates of new articles are merged') : t('Duplicates are no longer merged'));
    } catch (e) { dups.setChecked(!dups.checked); showError(e); }
  });
  return h('div', { class: 'flex flex-col gap-3' }, refreshControl(), h('div', { class: 'group px-4' },
    h('div', { class: 'switch-row' },
      h('div', {}, h('div', { class: 'text-[1rem]', text: t('Images in the list') }),
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('Show the preview on the right of each article') })),
      thumbs),
    h('div', { class: 'switch-row' },
      h('div', {}, h('div', { class: 'text-[1rem]', text: t('Mark as read while scrolling') }),
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('Articles leaving the screen upwards become read') })),
      sw),
    h('div', { class: 'switch-row' },
      h('div', {}, h('div', { class: 'text-[1rem]', text: t('Merge duplicates') }),
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('The same article from two feeds (a newspaper and Google News) shows up only once and is marked read everywhere. It applies to all devices.') })),
      dups)));
}
