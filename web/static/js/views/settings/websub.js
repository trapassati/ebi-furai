// Instant updates (WebSub), for administrators: whether they work, the
// public address the hubs call, how many feeds have them and what the hubs
// answered when something went wrong. Nothing to read in the log.

import { locale, t } from '../../i18n.js';
import { get, post } from '../../api.js';
import { h, icon, showError, tile, toast } from '../../ui.js';
import * as motion from '../../motion.js';

const numFmt = new Intl.NumberFormat(locale);
const ago = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });

function since(ts) {
  const s = Math.round(ts - Date.now() / 1000);
  const abs = Math.abs(s);
  if (abs < 60) return t('just now');
  if (abs < 3600) return ago.format(Math.round(s / 60), 'minute');
  if (abs < 86400) return ago.format(Math.round(s / 3600), 'hour');
  return ago.format(Math.round(s / 86400), 'day');
}

// What the hub's answer probably means, in plain words.
function hint(err) {
  const e = (err || '').toLowerCase();
  if (/did not confirm/.test(e)) return t('The hub never called back: it may not reach this server from the internet.');
  if (/denied/.test(e)) return t('The hub refused the subscription. It is asked again later.');
  if (/http 4\d\d/.test(e)) return t('The hub did not accept the request. It is asked again later.');
  if (/http 5\d\d|timeout|deadline|refused|reset|eof|lookup|tls/.test(e)) return t('The hub doesn\'t respond: it usually passes by itself.');
  return '';
}

const stat = (value, label, tone) => h('div', { class: 'websub-stat' },
  h('div', { class: `websub-stat-value ${tone}`, text: numFmt.format(value) }),
  h('div', { class: 'websub-stat-label', text: label }));

export function websubControls() {
  const box = h('div', { class: 'flex flex-col gap-4' },
    h('div', { class: 'group px-4 py-6 text-center text-muted', text: t('Loading…') }));

  const paint = (st) => {
    const parts = [];

    // How it is going, in one line.
    let tone = 'ok', title = t('Working'), text;
    if (st.mode === 'off') {
      tone = 'plain'; title = t('Turned off');
      text = t('PUBLIC_URL=off: feeds are checked at regular intervals only.');
    } else if (!st.public_url) {
      tone = 'sunset'; title = t('Waiting for the address');
      text = t('Open the app from its https address on the internet: the server learns it by itself.');
    } else {
      text = st.mode === 'fixed' ? t('Address set with PUBLIC_URL') : t('Address learned from the app');
      if (!st.active && (st.failing || st.pending)) { tone = 'sunset'; title = t('Not working yet'); }
      else if (!st.active) { title = t('Ready'); text += ' · ' + t('none of your feeds offers it yet'); }
    }
    parts.push(h('div', { class: 'group websub-status' },
      tile('o-bolt', tone === 'ok' ? 'sunset' : 'plain'),
      h('div', { class: 'flex-1 min-w-0' },
        h('div', { class: 'flex items-center gap-2' },
          h('span', { class: `websub-dot ${tone === 'ok' ? 'ok' : tone === 'sunset' ? 'warn' : 'off'}` }),
          h('span', { class: 'text-[1.0625rem] font-semibold', text: title })),
        st.public_url ? h('div', { class: 'text-[0.9375rem] truncate', text: st.public_url.replace(/^https:\/\//, '') }) : null,
        h('div', { class: 'text-[0.8125rem] text-muted', text }))));

    if (st.mode !== 'off') {
      parts.push(h('div', { class: 'websub-stats' },
        stat(st.active, t('instant'), 'ok'),
        stat(st.pending, t('waiting'), 'muted'),
        stat(st.failing, t('with errors'), st.failing ? 'danger' : 'muted')));
      parts.push(h('div', { class: 'text-[0.8125rem] text-muted px-1', text:
        t('{0} of the {1} feeds followed on this server offer instant updates.', numFmt.format(st.with_hub), numFmt.format(st.followed)) +
        ' ' + (st.last_notified ? t('Last notification from a hub: {0}.', since(st.last_notified)) : t('No notification from a hub yet.')) }));
    }

    if (st.hubs.length) {
      parts.push(h('div', { class: 'group-label', text: t('Hubs') }));
      parts.push(h('div', { class: 'group' }, st.hubs.map((hub) => h('div', { class: 'flex items-center gap-3 px-4 py-3' },
        h('div', { class: 'flex-1 min-w-0' },
          h('div', { class: 'truncate text-[1rem]', text: hub.host }),
          h('div', { class: 'text-[0.8125rem] text-muted', text: t('{0} instant', numFmt.format(hub.active)) +
            (hub.failing ? ' · ' + t('{0} with errors', numFmt.format(hub.failing)) : '') })),
        icon(hub.failing ? 'o-exclamation-triangle' : 'o-check-circle', 'w-5 h-5 flex-none ' + (hub.failing ? 'text-danger' : 'text-ok'))))));
    }

    if (st.problems.length) {
      parts.push(h('div', { class: 'group-label', text: t('Errors') }));
      parts.push(h('div', { class: 'flex flex-col gap-3' }, st.problems.map((p) => {
        const tip = hint(p.error);
        return h('div', { class: 'group health-row' },
          h('div', { class: 'flex items-center gap-2 min-w-0' },
            h('span', { class: 'flex-1 min-w-0 text-[1rem] font-semibold truncate', text: p.title || t('A feed of another user') }),
            h('span', { class: 'text-[0.8125rem] text-muted flex-none', text: since(p.at) })),
          h('div', { class: 'text-[0.8125rem] text-muted truncate', text: p.hub }),
          h('div', { class: 'health-error', text: tip || p.error }),
          tip ? h('div', { class: 'text-[0.8125rem] text-muted websub-raw', text: p.error }) : null);
      })));
      const retry = h('button', { class: 'btn btn-plain w-full' }, icon('o-arrow-path', 'w-5 h-5'), t('Ask the hubs again now'));
      retry.addEventListener('click', async () => {
        retry.disabled = true;
        try { paint(await post('/api/admin/websub/retry')); toast(t('Requests sent: the answers arrive within a few minutes')); } catch (e) { showError(e); retry.disabled = false; }
      });
      parts.push(retry);
    }

    parts.push(h('div', { class: 'text-[0.8125rem] text-muted px-1', text:
      t('When a feed offers it, its hub warns the server as soon as there is something new, and the articles arrive within seconds. Feeds with a password never use it. It applies to all users.') }));
    box.replaceChildren(...parts);
    motion.swapIn(box.children, 0);
  };

  get('/api/admin/websub').then(paint).catch((e) => {
    box.replaceChildren(h('div', { class: 'group px-4 py-6 text-center text-muted', text: e.message || String(e) }));
  });
  return box;
}
