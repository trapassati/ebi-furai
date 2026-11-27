// Feed statistics: how much is read day by day, the feeds read most, the
// ones that publish but are never opened and the ones that stopped. The
// last two come with a button to unsubscribe. Opens are counted since
// version 1.9.0: until there are enough days the suggestions wait.

import { locale, t } from '../../i18n.js';
import { del, get } from '../../api.js';
import { state } from '../../state.js';
import { confirmDialog, feedIcon, h, icon, showError, toast } from '../../ui.js';
import { feedMenu, sourcesChanged } from '../sources.js';
import { segmented } from './parts.js';
import * as motion from '../../motion.js';

const numFmt = new Intl.NumberFormat(locale);
const pctFmt = new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 });
const avgFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
const dayFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', timeZone: 'UTC' });
const longFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'long', timeZone: 'UTC' });
const ago = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });

const STOPPED_DAYS = 60; // no new article for this long: the feed stopped
const IGNORED_MIN = 5; // at least this many articles arrived, none opened
const TOP = 8;

const dayDate = (day) => new Date(day * 86400000);

function agoDays(unix) {
  const d = Math.round((unix - Date.now() / 1000) / 86400);
  if (d > -45) return ago.format(d, 'day');
  if (d > -365) return ago.format(Math.round(d / 30), 'month');
  return ago.format(Math.round(d / 365), 'year');
}

const stat = (value, label) => h('div', { class: 'websub-stat' },
  h('div', { class: 'websub-stat-value stats-value', text: value }),
  h('div', { class: 'websub-stat-label', text: label }));

// Opens per day: one bar each, today on the right. Tapping or pointing at a
// bar tells its day and number.
function chart(st) {
  const max = Math.max(1, ...st.daily);
  const first = st.today - st.days + 1;
  const tip = h('div', { class: 'stats-tip', role: 'status', 'aria-live': 'polite' });
  const bars = h('div', { class: 'stats-bars' + (st.days > 45 ? ' dense' : '') });
  const show = (i, bar) => {
    const day = first + i;
    bars.querySelectorAll('.on').forEach((b) => b.classList.remove('on'));
    if (bar) bar.classList.add('on');
    tip.textContent = `${dayFmt.format(dayDate(day))}: ` + (st.daily[i] === 1 ? t('1 article opened') : t('{0} articles opened', numFmt.format(st.daily[i])));
  };
  st.daily.forEach((n, i) => {
    const before = first + i < st.since;
    const bar = h('button', {
      type: 'button', class: 'stats-bar' + (before ? ' nodata' : ''),
      'aria-label': `${dayFmt.format(dayDate(first + i))}: ${n}`,
      onclick: () => show(i, bar),
      onpointerenter: (e) => { if (e.pointerType === 'mouse') show(i, bar); },
    }, h('span', { class: 'stats-fill', dataset: { h: String(n ? Math.max(4, (n / max) * 100) : 0) } }));
    bars.append(bar);
  });
  bars.addEventListener('pointerleave', (e) => { if (e.pointerType === 'mouse') reset(); });
  const reset = () => {
    bars.querySelectorAll('.on').forEach((b) => b.classList.remove('on'));
    tip.textContent = t('Tap a day to see how many');
  };
  reset();
  const axis = h('div', { class: 'stats-axis' },
    h('span', { text: dayFmt.format(dayDate(first)) }), h('span', { text: t('today') }));
  return h('div', { class: 'group stats-chart' }, bars, axis, tip);
}

function feedLine(f, detail, extra) {
  const src = state.sources && [...state.sources.feeds, ...state.sources.folders.flatMap((x) => x.feeds)].find((x) => x.id === f.id);
  const row = h('div', { class: 'stats-row', role: src ? 'button' : null, tabindex: src ? '0' : null },
    h('span', { class: 'stats-icon' }, feedIcon(f.id, f.has_icon, f.title, 'big')),
    h('div', { class: 'flex-1 min-w-0' },
      h('div', { class: 'truncate text-[1rem] font-medium', text: f.title }),
      h('div', { class: 'text-[0.8125rem] text-muted', text: detail }),
      extra || null));
  if (src) {
    row.addEventListener('click', (e) => { if (!e.target.closest('button')) feedMenu(src); });
    row.addEventListener('keydown', (e) => { if (e.target === row && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); feedMenu(src); } });
  }
  return row;
}

async function unsubscribe(f, row, reload) {
  const ok = await confirmDialog({
    title: t('Delete the feed?'), message: t('"{0}" and its articles (starred ones too) will be deleted.', f.title),
    okLabel: t('Delete'), destructive: true,
  });
  if (!ok) return;
  try {
    await del(`/api/feeds/${f.id}`);
    await motion.collapse([row]);
    toast(t('Feed deleted'));
    await sourcesChanged();
    reload();
  } catch (e) { showError(e); }
}

const removeBtn = (f, reload) => {
  const b = h('button', { type: 'button', class: 'stats-remove', 'aria-label': t('Delete "{0}"', f.title) }, t('Remove'));
  b.addEventListener('click', () => unsubscribe(f, b.closest('.stats-row'), reload));
  return b;
};

export function statsControls() {
  let days = 30;
  const body = h('div', { class: 'flex flex-col gap-4' });
  const box = h('div', { class: 'flex flex-col gap-4' },
    segmented([['30', t('30 days')], ['90', t('90 days')]], '30', (v) => { days = Number(v); load(); }),
    body);

  const paint = (st) => {
    const parts = [];
    const total = st.daily.reduce((a, b) => a + b, 0);
    const counted = Math.min(st.days, st.today - st.since + 1);
    const arrived = st.feeds.reduce((a, f) => a + f.arrived, 0);
    // Until opens cover the whole period, a share of what arrived would
    // compare 30 days of articles with a few days of reading.
    const partial = st.since > st.today - st.days + 1;
    parts.push(h('div', { class: 'websub-stats' },
      stat(numFmt.format(total), t('opened')),
      stat(avgFmt.format(total / counted), t('a day')),
      stat(arrived && !partial ? pctFmt.format(Math.min(1, total / arrived)) : '–', t('of what arrives'))));
    if (partial) {
      parts.push(h('div', { class: 'text-[0.8125rem] text-muted px-1', text:
        t('Opened articles are counted since {0}: the days before have no data.', longFmt.format(dayDate(st.since))) }));
    }
    parts.push(chart(st));

    // Read most.
    const top = st.feeds.filter((f) => f.opened > 0).sort((a, b) => b.opened - a.opened || a.title.localeCompare(b.title)).slice(0, TOP);
    parts.push(h('div', { class: 'group-label', text: t('Read most') }));
    if (top.length) {
      const most = top[0].opened;
      parts.push(h('div', { class: 'group' }, top.map((f) => {
        const bar = h('span', { dataset: { w: String(Math.max(4, (f.opened / most) * 100)) } });
        const detail = partial ? (f.opened === 1 ? t('1 article opened') : t('{0} articles opened', numFmt.format(f.opened)))
          : t('{0} opened of {1}', numFmt.format(f.opened), numFmt.format(Math.max(f.arrived, f.opened)));
        const pct = f.arrived && !partial ? ' · ' + pctFmt.format(Math.min(1, f.opened / f.arrived)) : '';
        return feedLine(f, detail + pct, h('div', { class: 'progress stats-progress' }, bar));
      })));
    } else {
      parts.push(empty(t('No article opened in these days yet.')));
    }

    // Ignored: they publish, nothing is opened. Only with enough days of
    // data, and for feeds followed for the whole period.
    const startOfPeriod = st.today - st.days + 1;
    parts.push(h('div', { class: 'group-label', text: t('Never opened') }));
    if (st.since > startOfPeriod) {
      const left = st.since - startOfPeriod;
      parts.push(empty(left === 1 ? t('Suggestions in 1 day, when there is enough data.') : t('Suggestions in {0} days, when there is enough data.', left)));
    } else {
      const ignored = st.feeds.filter((f) => f.since <= startOfPeriod && f.opened === 0 && f.arrived >= IGNORED_MIN)
        .sort((a, b) => b.arrived - a.arrived);
      if (ignored.length) {
        parts.push(h('div', { class: 'text-[0.8125rem] text-muted px-1 -mt-1', text: t('They publish, but in {0} days you never opened an article. Maybe they can go.', st.days) }));
        parts.push(h('div', { class: 'group' }, ignored.map((f) => {
          const r = feedLine(f, t('{0} articles, none opened', numFmt.format(f.arrived)) +
            (f.last_opened ? ' · ' + t('last read {0}', agoDays(f.last_opened * 86400)) : ''));
          r.append(removeBtn(f, load));
          return r;
        })));
      } else {
        parts.push(empty(t('None: every feed that publishes gets read.')));
      }
    }

    // Stopped: nothing new for two months (or never anything).
    const now = Date.now() / 1000;
    const stopped = st.feeds.filter((f) => f.last_article ? now - f.last_article > STOPPED_DAYS * 86400 : st.today - f.since > 7)
      .sort((a, b) => a.last_article - b.last_article);
    parts.push(h('div', { class: 'group-label', text: t('Stopped') }));
    if (stopped.length) {
      parts.push(h('div', { class: 'text-[0.8125rem] text-muted px-1 -mt-1', text: t('No new article for more than two months.') }));
      parts.push(h('div', { class: 'group' }, stopped.map((f) => {
        const r = feedLine(f, f.last_article ? t('Last article {0}', agoDays(f.last_article)) : t('No article ever arrived'));
        r.append(removeBtn(f, load));
        return r;
      })));
    } else {
      parts.push(empty(t('None: all your feeds are still publishing.')));
    }

    body.replaceChildren(...parts);
    motion.swapIn(body.children, 0);
    grow(body);
  };

  const load = () => get('/api/stats?days=' + days).then(paint).catch((e) => {
    body.replaceChildren(h('div', { class: 'group px-4 py-6 text-center text-muted', text: e.message || String(e) }));
  });
  body.append(h('div', { class: 'group px-4 py-6 text-center text-muted', text: t('Loading…') }));
  load();
  return box;
}

const empty = (text) => h('div', { class: 'group px-4 py-4 text-[0.9375rem] text-muted', text });

// The bars grow from zero (CSP: sizes are set from script, not inline).
function grow(root) {
  const fills = [...root.querySelectorAll('.stats-fill')];
  const lines = [...root.querySelectorAll('.stats-progress > span')];
  fills.forEach((el) => { el.style.height = el.dataset.h + '%'; });
  lines.forEach((el) => { el.style.width = el.dataset.w + '%'; });
  if (!motion.enabled()) return;
  window.gsap.from(fills, { height: 0, duration: 0.5, stagger: 0.008, ease: 'power2.out' });
  window.gsap.from(lines, { width: 0, duration: 0.6, stagger: 0.04, ease: 'power2.out', delay: 0.1 });
}
