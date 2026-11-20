// "Salute dei feed": the feeds that keep failing, in one place, with what
// the error means and what can be done (retry, new address, password,
// delete). Opened from the home ("N feed non si aggiornano") and from the
// settings.

import { locale, t } from '../i18n.js';
import { post } from '../api.js';
import { state } from '../state.js';
import { feedIcon, h, icon, openSheet, showError, toast } from '../ui.js';
import * as motion from '../motion.js';
import { feedMenu, sourcesChanged } from './sources.js';

const ago = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });

function since(ts) {
  if (!ts) return t('never');
  const s = Math.round(ts - Date.now() / 1000);
  const abs = Math.abs(s);
  if (abs < 3600) return ago.format(Math.round(s / 60), 'minute');
  if (abs < 86400) return ago.format(Math.round(s / 3600), 'hour');
  return ago.format(Math.round(s / 86400), 'day');
}

// What the error probably means, in plain words.
function hint(err) {
  const e = (err || '').toLowerCase();
  if (/http 40[13]|password|accesso|access denied/.test(e)) return t('The site asks you to sign in: set a username and password from the menu.');
  if (/http 404|http 410|non trovato|not found/.test(e)) return t('The address no longer exists: the site probably changed it.');
  if (/http 429/.test(e)) return t('The site limits requests: try again later, it usually passes by itself.');
  if (/http 5\d\d/.test(e)) return t('The site has a problem of its own: it usually passes by itself.');
  if (/rete privata|non consentito|private network|not allowed/.test(e)) return t('The address leads to an internal network, which the server does not reach for safety.');
  if (/no such host|lookup|dns/.test(e)) return t('The domain no longer exists or cannot be reached.');
  if (/deadline|timeout|timed out|tempo scaduto|did not respond|non ha risposto|refused|reset|eof|tls|certificate/.test(e)) return t('The site doesn\'t respond or the connection fails.');
  if (/non valido|invalid|xml|parse|feed/.test(e)) return t('The page is not (or no longer) a feed: change the address to the site\'s one.');
  return '';
}

function failingFeeds() {
  const s = state.sources;
  if (!s) return [];
  return s.feeds.concat(...s.folders.map((f) => f.feeds))
    .filter((f) => f.error_count > 0)
    .sort((a, b) => b.error_count - a.error_count || a.title.localeCompare(b.title, locale));
}

export function failingCount() {
  return failingFeeds().length;
}

export function openFeedHealth() {
  const ref = {};
  const list = h('div', { class: 'flex flex-col gap-3' });
  const retryAll = h('button', { class: 'btn btn-plain w-full' }, icon('o-arrow-path', 'w-5 h-5'), t('Retry all'));

  const render = () => {
    const feeds = failingFeeds();
    retryAll.hidden = feeds.length === 0;
    if (!feeds.length) {
      list.replaceChildren(h('div', { class: 'group' }, h('div', { class: 'px-4 py-6 text-center' },
        icon('o-check-circle', 'w-10 h-10 mx-auto text-ok'),
        h('p', { class: 'text-[1.0625rem] font-semibold mt-2', text: t('All feeds are updating') }),
        h('p', { class: 'text-muted text-[0.9375rem] mt-1', text: t('No errors in the latest updates.') }))));
      return;
    }
    list.replaceChildren(...feeds.map((f) => {
      const tip = hint(f.last_error);
      return h('button', { class: 'group health-row text-left', onclick: () => feedMenu(f) },
        h('div', { class: 'flex items-center gap-3' },
          feedIcon(f.id, f.has_icon, f.title),
          h('div', { class: 'flex-1 min-w-0' },
            h('div', { class: 'text-[1rem] font-semibold truncate', text: f.title }),
            h('div', { class: 'text-[0.8125rem] text-muted truncate', text: f.url })),
          icon('o-ellipsis-horizontal', 'w-5 h-5 text-muted flex-none')),
        h('div', { class: 'health-error', text: f.last_error || t('Unknown error') }),
        tip ? h('div', { class: 'text-[0.875rem]', text: tip }) : null,
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('Last successful update: {0}. Failed attempts in a row: {1}.', since(f.last_ok_at), f.error_count) }));
    }));
  };

  retryAll.addEventListener('click', async () => {
    const feeds = failingFeeds();
    retryAll.disabled = true;
    let fixed = 0;
    for (const f of feeds) {
      try {
        const r = await post(`/api/feeds/${f.id}/refresh`);
        if (!r.feed.error_count) fixed++;
      } catch (e) { showError(e); break; }
    }
    retryAll.disabled = false;
    toast(fixed ? t('{0} of {1} work now', fixed, feeds.length) : t('Still none responds'));
    await sourcesChanged();
  });

  // The page follows the counters: a feed fixed from its menu leaves it.
  const onRendered = () => {
    const before = list.children.length;
    render();
    if (list.children.length !== before) motion.swapIn(list.children, 0);
  };
  window.addEventListener('sources:rendered', onRendered);

  render();
  const body = h('div', { class: 'min-h-full' },
    h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
      h('div', { class: 'w-20' }),
      h('div', { class: 'flex-1 text-center sheet-title', text: t('Feed health') }),
      h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') })),
    h('div', { class: 'px-4 pb-10 flex flex-col gap-4' },
      h('p', { class: 'text-[0.9375rem] text-muted px-1', text: t('Feeds that didn\'t update at the last attempt. The server retries by itself, less and less often; tap a feed to retry now, change its address or delete it.') }),
      list, retryAll));
  ref.sheet = openSheet(body, { full: true, wide: true });
  ref.sheet.done.then(() => window.removeEventListener('sources:rendered', onRendered));
}
