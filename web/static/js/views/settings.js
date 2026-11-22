// Settings: appearance, account, OPML and user administration.

import { langPref, locale, setLang, t } from '../i18n.js';
import { api, del, get, post, put } from '../api.js';
import { FONTS, LINE_HEIGHTS, LIST_SIZES, getPrefs, setPref } from '../prefs.js';
import { state } from '../state.js';
import { actionSheet, clear, confirmDialog, formDialog, h, icon, openSheet, picker, showError, tile, toast, toggleSwitch } from '../ui.js';
import * as offline from '../offline/index.js';
import * as push from '../push.js';
import * as motion from '../motion.js';
import { canInstall, showInstallHint } from '../install.js';
import { openAbout } from './about.js';
import { openApps } from './apps.js';
import { openUpdate } from './update.js';
import { failingCount, openFeedHealth } from './health.js';
import { openFilters } from './filters.js';
import { openSessions, openTwoFactor, twoFactorLabel } from './security.js';
import { showShortcuts } from '../shortcuts.js';
import { isDesk } from '../desk.js';

// ---- appearance (also used by the "Aa" button in the reader) ----

export function segmented(options, current, onPick) {
  const seg = h('div', { class: 'segmented w-full' });
  const paint = (v) => seg.querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.v === v));
  options.forEach(([v, label]) => seg.append(h('button', {
    type: 'button', class: 'flex-1', dataset: { v }, text: label,
    onclick: () => { paint(v); onPick(v); },
  })));
  paint(current);
  return seg;
}

function appearanceControls() {
  const p = getPrefs();
  const sizeLabel = h('span', { class: 'w-16 text-center tabular-nums text-[0.9375rem] font-medium', text: p.size + ' pt' });
  const changeSize = (d) => {
    const v = Math.min(28, Math.max(13, (Number(getPrefs().size) || 18) + d));
    setPref('size', v);
    sizeLabel.textContent = v + ' pt';
  };
  const fonts = h('div', { class: 'group' });
  const paintFonts = () => fonts.querySelectorAll('button').forEach((b) => {
    const on = b.dataset.v === getPrefs().font;
    b.querySelector('.check').classList.toggle('invisible', !on);
  });
  Object.entries(FONTS).forEach(([key, f]) => {
    const label = h('span', { class: 'flex-1', text: f.label });
    label.style.fontFamily = f.stack;
    fonts.append(h('button', {
      class: 'row-btn px-4 py-2.5 text-[1.0625rem]', dataset: { v: key },
      onclick: () => { setPref('font', key); paintFonts(); },
    }, label, icon('o-check', 'check w-5 h-5 text-accent')));
  });
  paintFonts();

  const book = toggleSwitch(p.book !== false);
  book.addEventListener('change', () => setPref('book', book.checked));
  // Side-by-side pages exist only on touch screens (iPhone Duo).
  const touch = window.matchMedia('(pointer: coarse)').matches;
  // The interface size is for screens with a mouse; it stays on this device.
  const desktop = window.matchMedia('(hover: hover) and (pointer: fine)').matches;
  let uiNow = 'auto';
  try { uiNow = localStorage.getItem('uiScale') || 'auto'; } catch { /* ignore */ }
  const uiSize = desktop ? h('div', { class: 'flex flex-col gap-1.5' },
    h('div', { class: 'text-[0.8125rem] text-muted px-1', text: t('Interface size on this computer') }),
    segmented([['auto', t('Automatic|f')], ['1', '100%'], ['1.15', '115%'], ['1.3', '130%']], uiNow, (v) => {
      try { if (v === 'auto') localStorage.removeItem('uiScale'); else localStorage.setItem('uiScale', v); } catch { /* ignore */ }
      if (window.__applyUiScale) window.__applyUiScale();
      window.dispatchEvent(new Event('resize'));
    })) : null;

  // Line spacing of the article and text size of the article list. The
  // nearest choice is shown as selected, whatever the stored value.
  const near = (list, v, def) => String(list.reduce((a, [x]) => (Math.abs(x - v) < Math.abs(a - v) ? x : a), def));
  const labelled = (label, control) => h('div', { class: 'flex flex-col gap-1.5' },
    h('div', { class: 'text-[0.8125rem] text-muted px-1', text: label }), control);
  const lineHeight = labelled(t('Article line spacing'),
    segmented(LINE_HEIGHTS.map(([v, l]) => [String(v), l]), near(LINE_HEIGHTS, Number(p.line_height) || 1.65, 1.65),
      (v) => setPref('line_height', Number(v))));
  const listSize = labelled(t('Article list text'),
    segmented(LIST_SIZES.map(([v, l]) => [String(v), l]), near(LIST_SIZES, Number(p.list_size) || 1, 1),
      (v) => setPref('list_size', Number(v))));

  // Language: the page reloads to apply it everywhere.
  const language = labelled(t('Language'),
    segmented([['auto', t('Automatic|f')], ['it', 'Italiano'], ['en', 'English']], langPref(), (v) => { setLang(v); location.reload(); }));

  return h('div', { class: 'flex flex-col gap-3' },
    h('div', { class: 'text-[0.8125rem] text-muted px-1', text: t('They only apply to this device.') }),
    language,
    segmented([['auto', t('Automatic')], ['light', t('Light')], ['dark', t('Dark')]], p.theme, (v) => setPref('theme', v)),
    fonts,
    h('div', { class: 'size-box bg-raise rounded-[calc(20px*var(--ui))] shadow-[var(--shadow-card)] flex items-center justify-between px-2 py-2' },
      h('button', { class: 'btn btn-plain !min-h-11 !px-5 text-[0.9375rem]', 'aria-label': t('Smaller text'), onclick: () => changeSize(-1), text: 'A−' }),
      sizeLabel,
      h('button', { class: 'btn btn-plain !min-h-11 !px-5 text-[1.25rem]', 'aria-label': t('Larger text'), onclick: () => changeSize(1), text: 'A+' })),
    lineHeight,
    listSize,
    touch ? h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' },
        h('div', {}, h('div', { class: 'text-[1rem]', text: t('Side-by-side pages') }),
          h('div', { class: 'text-[0.8125rem] text-muted', text: t('On an open iPhone Duo, full width: two columns like a book') })),
        book)) : null,
    uiSize);
}

export function openAppearance() {
  const body = h('div', { class: 'px-4 pt-1 pb-3 flex flex-col gap-3' },
    h('div', { class: 'sheet-title pb-1', text: t('Appearance') }),
    appearanceControls());
  openSheet(body);
}

// ---- settings sheet ----

// A section of the settings. On the desktop each one is a page of the
// settings window, listed on its left with its icon.
function section(label, iconName, tone, ...children) {
  return h('div', { class: 'settings-section', dataset: { label, icon: iconName, tone } },
    h('div', { class: 'group-label', text: label }), ...children);
}

let lastSection = 0; // the page of the settings window shown last (desktop)

function rowButton(iconName, tone, label, onclick, cls = '') {
  return h('button', { class: 'row-btn px-3 py-2 text-[1.0625rem] ' + cls, onclick },
    tile(iconName, tone, 'sm'), h('span', { class: 'flex-1', text: label }), icon('o-chevron-right', 'w-4 h-4 text-faint'));
}

export function openSettings(ctx) {
  const me = state.me;
  const usersBox = h('div', { class: 'group' });

  const fileInput = h('input', { type: 'file', accept: '.opml,.xml,text/xml,application/xml,text/x-opml', class: 'hidden' });
  fileInput.addEventListener('change', async () => {
    const file = fileInput.files && fileInput.files[0];
    fileInput.value = '';
    if (!file) return;
    const fd = new FormData();
    fd.append('file', file);
    try {
      const r = await api('POST', '/api/opml', fd);
      toast(t('Imported {0} feeds', r.added) + (r.skipped ? t(', {0} already there', r.skipped) : '') + (r.invalid ? t(', {0} not valid', r.invalid) : ''));
      await ctx.afterSourcesChanged();
      setTimeout(() => ctx.reloadSources(), 8000); // first fetches run in background
    } catch (e) { showError(e); }
  });

  const content = h('div', { class: 'px-4 pb-6' },
    section(t('Appearance'), 'o-swatch', 'aurora', appearanceControls()),
    section('Account', 'o-user-circle', 'indigo', h('div', { class: 'group' },
      h('div', { class: 'flex items-center gap-3 px-3 py-3' },
        tile('o-user-circle', 'aurora'),
        h('div', {}, h('div', { class: 'text-[1.0625rem] font-semibold', text: me.user.username }),
          h('div', { class: 'text-[0.8125rem] text-muted', text: me.user.is_admin ? t('Administrator') : t('User') }))),
      rowButton('o-key', 'sky', t('Change password'), changePassword),
      twoFactorRow(),
      rowButton('o-device-phone-mobile', 'indigo', t('Connected devices'), openSessions),
      rowButton('o-squares-2x2', 'sunset', t('Reeder and other apps'), openApps),
      canInstall() ? rowButton('o-plus', 'indigo', t('Add to Home Screen'), () => { sheet.close(); setTimeout(showInstallHint, 350); }) : null,
      rowButton('o-arrow-right-start-on-rectangle', 'rose', t('Sign out'), logout, 'text-danger'))),
    section(t('Notifications'), 'o-bell', 'rose', notifyControls()),
    section(t('Reading'), 'o-newspaper', 'sky', readingControls()),
    section('Offline', 'o-signal-slash', 'mint', offlineControls()),
    section(t('Ads'), 'o-no-symbol', 'sunset', adControls(me.user.is_admin)),
    section(t('Feeds'), 'o-rss', 'sunset', h('div', { class: 'group' },
      rowButton('o-exclamation-triangle', failingCount() ? 'rose' : 'mint',
        failingCount() ? t('Feed health: {0} with problems', failingCount()) : t('Feed health'), openFeedHealth),
      rowButton('o-funnel', 'indigo', t('Keyword filters'), () => openFilters()),
      rowButton('o-arrow-up-tray', 'mint', t('Import OPML'), () => fileInput.click()),
      rowButton('o-arrow-down-tray', 'sunset', t('Export OPML'), exportOPML)), fileInput),
    me.user.is_admin ? section('Backup', 'o-circle-stack', 'mint', backupControls()) : null,
    me.user.is_admin ? section(t('Users'), 'o-users', 'indigo', usersBox,
      h('button', { class: 'btn btn-primary w-full mt-4', onclick: () => createUser(usersBox) }, icon('o-user-plus', 'w-5 h-5'), t('New user'))) : null,
    section(t('About'), 'o-information-circle', 'plain', h('div', { class: 'group' },
      window.matchMedia('(hover: hover) and (pointer: fine)').matches || navigator.keyboard
        ? rowButton('o-command-line', 'plain', t('Keyboard shortcuts'), showShortcuts) : null,
      state.update ? rowButton('o-sparkles', 'aurora', t('Update available: {0}', state.update.latest), () => openUpdate(state.update)) : null,
      rowButton('o-information-circle', 'plain', t('About Ebi Furai'), openAbout))),
    h('div', { class: 'text-center text-[0.75rem] text-muted pt-10', text: t('{0}, version {1}', me.app_name, me.version) }));

  const header = h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10 settings-head' },
    h('div', { class: 'w-20' }),
    h('div', { class: 'flex-1 text-center sheet-title', text: t('Settings') }),
    h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => sheet.close(), text: t('Done') }));
  let root;
  if (isDesk()) {
    // Desktop: the sections on the left, one at a time on the right.
    const sections = [...content.querySelectorAll(':scope > .settings-section')];
    const footer = content.lastElementChild;
    const page = h('div', { class: 'settings-page' });
    const nav = h('nav', { class: 'settings-nav', 'aria-label': t('Settings') });
    const items = sections.map((sec, i) => h('button', {
      type: 'button', class: 'settings-nav-item', onclick: () => show(i),
    }, tile(sec.dataset.icon, sec.dataset.tone, 'xs'), h('span', { class: 'truncate', text: sec.dataset.label })));
    const show = (i) => {
      lastSection = i;
      items.forEach((b, j) => { b.classList.toggle('on', i === j); b.setAttribute('aria-current', i === j ? 'page' : 'false'); });
      page.replaceChildren(sections[i]);
      page.scrollTop = 0;
      if (motion.enabled()) window.gsap.fromTo(sections[i], { opacity: 0, y: 6 }, { opacity: 1, y: 0, duration: 0.2, ease: 'power2.out', clearProps: 'transform' });
    };
    nav.append(...items, h('div', { class: 'flex-1' }), footer);
    root = h('div', { class: 'settings-root' }, header, h('div', { class: 'settings-split' }, nav, page));
    show(Math.min(lastSection, sections.length - 1));
  } else {
    root = h('div', { class: 'min-h-full' }, header, content);
  }
  const sheet = openSheet(root, { full: true, wide: true, cls: 'settings-sheet' });
  if (me.user.is_admin) loadUsers(usersBox);
}

// ---- notifications ----

const HALF_HOURS = Array.from({ length: 48 }, (_, i) => `${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}`);
const dayFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });

function notifyControls() {
  const box = h('div', { class: 'flex flex-col gap-3' }, h('div', { class: 'group px-4 py-4 text-muted', text: t('Loading…') }));
  let info = null;

  const save = async () => {
    try { await put('/api/push/settings', info.settings); } catch (e) { showError(e); }
  };

  const render = () => {
    clear(box);
    const reason = push.unavailableReason();
    const st = info.settings;

    // This device
    const devSwitch = toggleSwitch(info.subscribed);
    devSwitch.disabled = !!reason;
    devSwitch.addEventListener('change', async () => {
      devSwitch.disabled = true;
      try {
        if (devSwitch.checked) {
          await push.enable(info.public_key); // first await: keeps the tap gesture for iOS
          toast(t('Notifications on for this device'));
        } else {
          await push.disable();
        }
      } catch (e) {
        showError(e);
      }
      info = await push.info().catch(() => info);
      render();
    });
    const top = h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' },
        h('div', {}, h('div', { class: 'text-[1rem]', text: t('Notifications on this device') }),
          reason ? null : h('div', { class: 'text-[0.75rem] text-muted', text: info.subscribed ? t('On') : t('Off') })),
        devSwitch));
    if (reason) top.append(h('div', { class: 'py-3 text-[0.8125rem] text-muted', text: reason }));
    box.append(top);

    // Schedule (shared by all devices of the user)
    const enabled = toggleSwitch(st.enabled);
    enabled.addEventListener('change', () => { st.enabled = enabled.checked; save(); render(); });
    const chips = h('div', { class: 'flex flex-wrap gap-2 py-3' });
    st.times.forEach((hm) => chips.append(h('button', {
      class: 'chip', 'aria-label': t('Remove {0}', hm),
      onclick: () => { st.times = st.times.filter((x) => x !== hm); save(); render(); },
    }, hm, icon('o-x-mark', 'w-4 h-4'))));
    if (st.times.length < 8) {
      chips.append(h('button', {
        class: 'chip chip-add',
        onclick: async () => {
          const choice = await actionSheet({
            title: t('Add a time'),
            actions: HALF_HOURS.filter((hm) => !st.times.includes(hm)).map((hm) => ({ label: hm, value: hm })),
          });
          if (!choice) return;
          st.times = [...st.times, choice.value].sort();
          save();
          render();
        },
      }, icon('o-plus', 'w-4 h-4'), t('Time')));
    }

    const quiet = toggleSwitch(st.quiet_enabled);
    quiet.addEventListener('change', () => { st.quiet_enabled = quiet.checked; save(); render(); });
    const qStart = picker({ title: t('Quiet hours start'), value: st.quiet_start, options: HALF_HOURS.map((hm) => ({ value: hm, label: hm })) });
    const qEnd = picker({ title: t('Quiet hours end'), value: st.quiet_end, options: HALF_HOURS.map((hm) => ({ value: hm, label: hm })) });
    qStart.addEventListener('change', () => { st.quiet_start = qStart.value; save(); });
    qEnd.addEventListener('change', () => { st.quiet_end = qEnd.value; save(); });

    const sched = h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' },
        h('div', {}, h('div', { class: 'text-[1rem]', text: t('Unread summary') }),
          h('div', { class: 'text-[0.75rem] text-muted', text: t('At the chosen times, if there are articles to read') })),
        enabled));
    if (st.enabled) {
      sched.append(h('div', {}, chips));
      sched.append(h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: t('Quiet hours') }), quiet));
      if (st.quiet_enabled) {
        sched.append(h('div', { class: 'flex items-center gap-3 py-3' },
          h('span', { class: 'text-muted text-[0.875rem]', text: t('From') }), qStart,
          h('span', { class: 'text-muted text-[0.875rem]', text: 'alle' }), qEnd));
      }
    }
    box.append(sched);

    // Devices
    if (info.devices.length) {
      const list = h('div', { class: 'group' });
      info.devices.forEach((d) => {
        let sub = t('Never received a notification');
        if (d.last_ok_at) sub = t('Last notification ') + dayFmt.format(new Date(d.last_ok_at * 1000));
        if (d.fail_count) sub = t('Delivery error: {0}', d.last_error);
        list.append(h('div', { class: 'row-btn px-3 py-2' },
          tile(d.current ? 'o-check-circle' : 'o-user-circle', d.current ? 'aurora' : 'plain', 'sm'),
          h('div', { class: 'flex-1 min-w-0' },
            h('div', { class: 'text-[1rem] truncate', text: d.device + (d.current ? t(' (this one)') : '') }),
            h('div', { class: 'text-[0.75rem] truncate ' + (d.fail_count ? 'text-danger' : 'text-muted'), text: sub })),
          h('button', {
            class: 'icon-btn', 'aria-label': t('Remove device'),
            onclick: async () => {
              const ok = await confirmDialog({ title: t('Remove the device?'), message: t('{0} will no longer receive notifications.', d.device), okLabel: t('Remove'), destructive: true, icon: 'o-device-phone-mobile' });
              if (!ok) return;
              try {
                if (d.current) await push.disable(); else await del(`/api/push/subscriptions/${d.id}`);
                info = await push.info();
                render();
              } catch (e) { showError(e); }
            },
          }, icon('o-trash', 'w-5 h-5'))));
      });
      box.append(h('div', { class: 'field-label pt-1', text: t('Devices receiving notifications') }), list);
      const testBtn = h('button', { class: 'btn btn-plain w-full' }, icon('o-sparkles', 'w-5 h-5'), t('Send a test notification'));
      testBtn.addEventListener('click', async () => {
        testBtn.disabled = true;
        try {
          const r = await post('/api/push/test');
          toast(r.failed ? t('Sent to {0} devices, {1} not reached', r.sent, r.failed) : (r.sent === 1 ? t('Sent to 1 device') : t('Sent to {0} devices', r.sent)));
          info = await push.info();
          render();
        } catch (e) { showError(e); }
        testBtn.disabled = false;
      });
      box.append(testBtn);
    }
    box.append(h('div', { class: 'px-1 text-[0.75rem] text-muted', text: t('Times in the {0} time zone. Each summary also updates the number on the app\'s icon.', info.timezone) }));
  };

  push.info().then((i) => { info = i; render(); }).catch((e) => {
    clear(box).append(h('div', { class: 'group px-4 py-4 text-muted', text: t('Notification settings are not available offline.') }));
    showError(e);
  });
  return box;
}

// ---- offline ----

function formatSize(bytes) {
  if (!bytes) return '';
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} kB`;
  return `${(bytes / 1024 / 1024).toFixed(1).replace('.', ',')} MB`;
}

const timeFmt = new Intl.DateTimeFormat(locale, { weekday: 'short', hour: '2-digit', minute: '2-digit' });

function offlineControls() {
  const p = getPrefs();
  const info = h('div', { class: 'text-[0.8125rem] text-muted' });
  const paintInfo = async () => {
    const st = await offline.storageInfo();
    const parts = [st.count === 1 ? t('1 article saved') : t('{0} articles saved', st.count)];
    if (st.lastSync) parts.push(t('updated {0}', timeFmt.format(new Date(st.lastSync))));
    if (st.usage) parts.push(t('space used {0}', formatSize(st.usage)));
    let text = parts.join(', ');
    if (st.pending) text += st.pending === 1 ? t('. 1 change to send to the server.') : t('. {0} changes to send to the server.', st.pending);
    info.textContent = text;
  };

  const enable = toggleSwitch(p.offline !== false);
  const limit = picker({
    title: t('Unread to save'),
    value: p.offline_limit || 200,
    options: [50, 100, 200, 500, 1000].map((n) => ({ value: n, label: t('{0} articles', n) })),
  });
  const images = toggleSwitch(!!p.offline_images);
  const syncBtn = h('button', { class: 'btn btn-plain flex-1' }, icon('o-arrow-path', 'w-5 h-5'), t('Sync'));
  const clearBtn = h('button', { class: 'btn btn-plain flex-1 !text-danger' }, icon('o-trash', 'w-5 h-5'), t('Delete data'));

  const paint = () => {
    const on = getPrefs().offline !== false;
    [limit, images, syncBtn].forEach((el) => { el.disabled = !on; el.classList.toggle('opacity-40', !on); });
  };
  enable.addEventListener('change', async () => {
    setPref('offline', enable.checked);
    paint();
    if (enable.checked) offline.syncNow().then(paintInfo);
    else { await offline.clearSaved(); paintInfo(); }
  });
  limit.addEventListener('change', () => { setPref('offline_limit', Number(limit.value)); offline.syncNow().then(paintInfo); });
  images.addEventListener('change', () => {
    setPref('offline_images', images.checked);
    if (images.checked) toast(t('Images will be downloaded at the next sync of new articles'));
  });
  syncBtn.addEventListener('click', async () => {
    syncBtn.disabled = true;
    await offline.syncNow();
    syncBtn.disabled = false;
    paintInfo();
    toast(offline.status.online ? t('Sync complete') : t('You\'re offline: try again later'));
  });
  clearBtn.addEventListener('click', async () => {
    const ok = await confirmDialog({
      title: t('Delete the offline data?'), message: t('Articles and images saved on this device will be removed. Changes not sent yet are kept.'),
      okLabel: t('Delete'), destructive: true,
    });
    if (!ok) return;
    await offline.clearSaved();
    paintInfo();
  });
  paint();
  paintInfo();

  return h('div', { class: 'flex flex-col gap-3' },
    h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: t('Save articles for offline reading') }), enable),
      h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: t('Download images too') }), images)),
    h('div', { class: 'flex flex-col gap-1.5' }, h('span', { class: 'field-label', text: t('Unread to save (plus starred)') }), limit),
    h('div', { class: 'px-1' }, info),
    h('div', { class: 'flex gap-3' }, syncBtn, clearBtn));
}

// ---- reading ----

// "5 minutes", "1 hour", "6 hours", "1 day"
function intervalLabel(m) {
  if (m < 60) return t('{0} minutes', m);
  if (m < 1440) return m === 60 ? t('1 hour') : t('{0} hours', m / 60);
  return m === 1440 ? t('1 day') : t('{0} days', m / 1440);
}

// How often the server refreshes this user's feeds (stored on the server).
function refreshControl() {
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

function readingControls() {
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

// ---- ad filter (EasyList) ----

const numFmt = new Intl.NumberFormat(locale);

// ---- backups (admin) ----

const sizeFmt = (n) => (n >= 1048576 ? `${(n / 1048576).toLocaleString(locale, { maximumFractionDigits: 1 })} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);
const backupDay = new Intl.DateTimeFormat(locale, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });

function backupControls() {
  const info = h('div', { class: 'text-[0.8125rem] text-muted px-1' });
  const list = h('div', { class: 'group' });
  const make = h('button', { class: 'btn btn-plain w-full' }, icon('o-circle-stack', 'w-5 h-5'), t('Create a backup now'));
  const paint = (st) => {
    if (!st.enabled) {
      info.textContent = t('Automatic backups are off (BACKUPS=false in the .env file).');
      list.replaceChildren();
      make.hidden = true;
      davBox.hidden = true;
      return;
    }
    info.textContent = t('A compressed copy of the database every day. The last {0} days and {1} weeks are kept, in the {2} folder of the server. It contains accounts and feed passwords: keep it somewhere safe.', st.keep_daily, st.keep_weekly, st.dir);
    if (!st.items.length) {
      list.replaceChildren(h('div', { class: 'px-4 py-3 text-muted text-[0.9375rem]', text: t('No backup yet: the first one is made a few minutes after start.') }));
      return;
    }
    list.replaceChildren(...st.items.map((b) => {
      const when = backupDay.format(new Date(b.time));
      return h('div', { class: 'flex items-center gap-3 px-3 py-2.5' },
        tile('o-circle-stack', 'indigo', 'sm'),
        h('span', { class: 'flex-1 min-w-0' },
          h('span', { class: 'block text-[1rem] truncate', text: when }),
          h('span', { class: 'block text-[0.8125rem] text-muted', text: sizeFmt(b.size) })),
        h('button', { class: 'icon-btn', 'aria-label': t('Restore the backup of {0}', when), onclick: () => restoreBackup({ name: b.name, when }) },
          icon('o-arrow-uturn-left', 'w-5 h-5')),
        h('a', { class: 'icon-btn', href: '/api/admin/backups/' + encodeURIComponent(b.name), download: b.name, 'aria-label': t('Download') },
          icon('o-arrow-down-tray', 'w-5 h-5')),
        h('button', { class: 'icon-btn', 'aria-label': t('Delete the backup of {0}', when), onclick: () => deleteBackup(b, when, st) },
          icon('o-trash', 'w-5 h-5')));
    }));
    paintDav(st.webdav || {});
  };
  // Deleting removes the file from this server only.
  const deleteBackup = async (b, when, st) => {
    const ok = await confirmDialog({
      title: t('Delete the backup of {0}?', when),
      message: st.webdav && st.webdav.configured
        ? t('It is removed from this server. Its copy on the WebDAV folder stays until the rotation removes it there.')
        : t('It is removed from this server and cannot be recovered.'),
      okLabel: t('Delete'), destructive: true, icon: 'o-trash',
    });
    if (!ok) return;
    try {
      await del(`/api/admin/backups/${encodeURIComponent(b.name)}`);
      toast(t('Backup deleted'));
      load();
    } catch (e) { showError(e); }
  };

  // ---- copies on a WebDAV server ----
  const dav = h('div', { class: 'group' });
  const davNote = h('div', { class: 'text-[0.8125rem] text-muted px-1' });
  let davTimer = 0;
  const paintDav = (w) => {
    clearTimeout(davTimer);
    davNote.textContent = t('Every backup is also copied to a folder of a WebDAV server (Nextcloud, a NAS, Koofr…), where the same days and weeks are kept. The copies hold every account: use a server you trust.');
    if (!w.configured) {
      dav.replaceChildren(h('button', { class: 'row-btn px-3 py-2.5 w-full text-left', onclick: () => editDav(w) },
        tile('o-cloud-arrow-up', 'sky', 'sm'),
        h('span', { class: 'flex-1 text-[1rem]', text: t('Copy to a WebDAV server…') }),
        icon('o-chevron-right', 'w-5 h-5 text-faint')));
      return;
    }
    const s = w.status || {};
    let line;
    let tone = 'mint';
    if (s.syncing) line = t('Copying now…');
    else if (s.last_error) { line = t('Last attempt failed: {0}', s.last_error); tone = 'rose'; }
    else if (s.last_ok) line = t('Up to date, {0} copies there. Last check {1}.', s.count, backupDay.format(new Date(s.last_ok)));
    else line = t('Waiting for the first copy.');
    dav.replaceChildren(
      h('button', { class: 'row-btn px-3 py-2.5 w-full text-left', onclick: () => editDav(w) },
        tile('o-cloud-arrow-up', tone, 'sm'),
        h('span', { class: 'flex-1 min-w-0' },
          h('span', { class: 'block text-[1rem] truncate', text: w.url }),
          h('span', { class: 'block text-[0.8125rem] ' + (s.last_error ? 'text-danger' : 'text-muted'), text: line })),
        icon('o-chevron-right', 'w-5 h-5 text-faint')),
      h('button', {
        class: 'row-btn px-3 py-2.5 w-full text-left', disabled: !!s.syncing,
        onclick: async () => {
          try { paintDav(await post('/api/admin/backups/webdav/sync')); setTimeout(load, 1500); } catch (e) { showError(e); }
        },
      }, tile('o-arrow-path', 'indigo', 'sm'), h('span', { class: 'flex-1 text-[1rem]', text: t('Copy now') })));
    // While copying, look again in a moment.
    if (s.syncing) davTimer = setTimeout(load, 2500);
  };
  const editDav = async (w) => {
    const fields = [
      { name: 'url', label: t('Folder address'), value: w.url || '', placeholder: 'https://cloud.example.org/remote.php/dav/files/me/Backup/', inputmode: 'url' },
      { name: 'user', label: t('Username'), value: w.user || '', autocomplete: 'off' },
      { name: 'password', type: 'password', label: t('Password'), placeholder: w.has_password ? t('Unchanged') : '', autocomplete: 'new-password' },
      { name: 'current', type: 'password', label: t('Confirm with your password'), autocomplete: 'current-password' },
    ];
    const values = await formDialog({
      title: t('Copies on WebDAV'),
      message: t('The address of the folder: it is created if it does not exist. For Nextcloud and ownCloud, an app password is better than the account password.'),
      fields, okLabel: t('Save'),
      validate: async (v) => {
        if (!v.url) return t('Enter the address of the folder');
        if (!v.current) return t('Enter the password');
        try { await put('/api/admin/backups/webdav', { url: v.url, user: v.user, password: v.password, current: v.current }); } catch (e) { return e.message; }
        return null;
      },
    });
    if (!values) return;
    toast(t('Saved: the backups are being copied'));
    load();
  };
  const stopDav = h('button', { class: 'btn btn-plain w-full', onclick: async () => {
    const ok = await confirmDialog({
      title: t('Stop the copies on WebDAV?'), message: t('The copies already there stay on the server.'),
      okLabel: t('Stop'), destructive: true, icon: 'o-cloud-arrow-up',
    });
    if (!ok) return;
    try { await del('/api/admin/backups/webdav'); load(); } catch (e) { showError(e); }
  } }, t('Stop the copies on WebDAV'));
  const davBox = h('div', { class: 'flex flex-col gap-3' }, h('div', { class: 'group-label', text: t('Copies elsewhere') }), davNote, dav, stopDav);
  const paintStop = (st) => { stopDav.hidden = !(st.webdav && st.webdav.configured); };
  const load = async () => {
    try { const st = await get('/api/admin/backups'); paint(st); paintStop(st); } catch (e) { info.textContent = ''; showError(e); }
  };
  make.addEventListener('click', async () => {
    make.disabled = true;
    try {
      await post('/api/admin/backups');
      await load();
      if (list.firstElementChild) motion.swapIn([list.firstElementChild], 1);
      toast(t('Backup created'));
    } catch (e) { showError(e); }
    make.disabled = false;
  });
  // Restore from a file saved elsewhere (or from another installation).
  const fileInput = h('input', { type: 'file', accept: '.gz,.db,application/gzip,application/x-sqlite3', class: 'hidden' });
  fileInput.addEventListener('change', () => {
    const file = fileInput.files && fileInput.files[0];
    fileInput.value = '';
    if (file) restoreBackup({ file });
  });
  const fromFile = h('button', { class: 'btn btn-plain w-full', onclick: () => fileInput.click() },
    icon('o-arrow-up-tray', 'w-5 h-5'), t('Restore from a file…'));
  load();
  return h('div', { class: 'flex flex-col gap-3' }, info, list, make, fromFile, fileInput, davBox);
}

// Restoring replaces every account, feed and article with the backup's.
// The server saves the current state first, then restarts: here we wait
// for it and reload, and everyone signs in again.
async function restoreBackup({ name, when, file }) {
  const values = await formDialog({
    title: t('Restore the backup?'),
    message: (file ? t('The database will be replaced with the file "{0}".', file.name) : t('The database will be replaced with the backup of {0}.', when))
      + ' ' + t('A copy of how things are now is saved first, so you can go back. Every device, this one too, will have to sign in again.'),
    fields: [{ name: 'password', type: 'password', label: t('Confirm with your password'), autocomplete: 'current-password' }],
    okLabel: t('Restore'),
    destructive: true,
    validate: async (v) => {
      if (!v.password) return t('Enter the password');
      if (file) {
        const fd = new FormData();
        fd.append('password', v.password); // first: a wrong one stops the upload early
        fd.append('file', file);
        await api('POST', '/api/admin/backups/restore', fd);
      } else {
        await post(`/api/admin/backups/${encodeURIComponent(name)}/restore`, { password: v.password });
      }
      return null;
    },
  });
  if (values) waitForRestart();
}

function waitForRestart() {
  const box = h('div', { class: 'restore-overlay', role: 'status' },
    h('div', { class: 'restore-card' },
      h('div', { class: 'restore-spinner', 'aria-hidden': 'true' }),
      h('div', { class: 'text-[1.125rem] font-semibold', text: t('Restoring…') }),
      h('div', { class: 'text-muted text-[0.9375rem]', text: t('The server is restarting with the backup. It takes a few seconds.') })));
  document.body.append(box);
  motion.swapIn([box.firstElementChild], 0);
  const started = Date.now();
  const poll = async () => {
    // The old server answers until it stops: wait a moment before asking.
    if (Date.now() - started > 2000) {
      try {
        const r = await fetch('/healthz', { cache: 'no-store' });
        if (r.ok) {
          try { await offline.wipe(); } catch { /* nothing saved */ }
          location.reload();
          return;
        }
      } catch { /* still restarting */ }
    }
    if (Date.now() - started > 120000) {
      box.querySelector('.text-muted').textContent = t('The server is taking long: check its log, then reload the page.');
      return;
    }
    setTimeout(poll, 1000);
  };
  poll();
}

function adControls(isAdmin) {
  const info = h('div', { class: 'text-[0.8125rem] text-muted px-1' });
  const enable = toggleSwitch(true);
  const updateBtn = h('button', { class: 'btn btn-plain w-full' }, icon('o-arrow-path', 'w-5 h-5'), t('Update the lists now'));
  const paint = (st) => {
    enable.setChecked(st.enabled);
    if (!st.enabled) info.textContent = t('Filter off: articles are shown as the site publishes them.');
    else if (!st.rules) info.textContent = t('The EasyList lists haven\'t been downloaded by the server yet.') + (st.error ? t(' Last error: {0}', st.error) : '');
    else {
      let txt = t('On with {0} EasyList and EasyList Italia rules', numFmt.format(st.rules));
      if (st.updated && !st.updated.startsWith('0001')) txt += t(', updated {0}', dayFmt.format(new Date(st.updated)));
      txt += t('. Elements removed since the server started: {0}.', numFmt.format(st.removed));
      info.textContent = txt;
    }
  };
  const load = async () => { try { paint(await get('/api/adfilter')); } catch (e) { info.textContent = ''; } };
  enable.addEventListener('change', async () => {
    try { paint(await put('/api/admin/adfilter', { enabled: enable.checked })); } catch (e) { enable.setChecked(!enable.checked); showError(e); }
  });
  updateBtn.addEventListener('click', async () => {
    updateBtn.disabled = true;
    try { paint(await post('/api/admin/adfilter/update')); toast(t('Lists updated')); } catch (e) { showError(e); }
    updateBtn.disabled = false;
  });
  load();
  const note = h('div', { class: 'text-[0.8125rem] text-muted px-1',
    text: t('Removes banners, sponsored boxes and tracking images from articles and full text. It applies to all users.') });
  if (!isAdmin) return h('div', { class: 'flex flex-col gap-2' }, info, note);
  return h('div', { class: 'flex flex-col gap-3' },
    h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: t('Filter ads') }), enable)),
    info, note, updateBtn);
}

// The row shows whether two-step verification is on, and follows changes.
function twoFactorRow() {
  const row = rowButton('o-shield-check', state.me.totp_enabled ? 'mint' : 'plain', twoFactorLabel(), () => openTwoFactor(paint));
  const label = [...row.querySelectorAll('span')].find((s) => s.textContent === twoFactorLabel());
  function paint() {
    if (label) label.textContent = twoFactorLabel();
    const tl = row.querySelector('.tile');
    if (tl) tl.className = tl.className.replace(/tile-\w+/, state.me.totp_enabled ? 'tile-mint' : 'tile-plain');
  }
  return row;
}

async function changePassword() {
  const r = await formDialog({
    title: t('Change password'),
    message: t('The other devices will be signed out.'),
    okLabel: t('Save'),
    fields: [
      { name: 'current', type: 'password', placeholder: t('Current password'), autocomplete: 'current-password' },
      { name: 'new', type: 'password', placeholder: t('New password (min. 8 characters)'), autocomplete: 'new-password' },
      { name: 'confirm', type: 'password', placeholder: t('Repeat the new password'), autocomplete: 'new-password' },
    ],
    validate: async (v) => {
      if (v.new !== v.confirm) return t('The two passwords don\'t match');
      try { await put('/api/me/password', { current: v.current, new: v.new }); } catch (e) { return e.message; }
      return null;
    },
  });
  if (r) toast(t('Password updated'));
}

async function logout() {
  const ok = await confirmDialog({ title: t('Sign out?'), message: t('The offline data on this device will be deleted.'), okLabel: t('Sign out'), destructive: true, icon: 'o-arrow-right-start-on-rectangle' });
  if (!ok) return;
  await push.disable();
  try { await post('/api/logout'); } catch (e) { /* ignore */ }
  await offline.wipe();
  location.reload();
}

async function exportOPML() {
  try {
    const res = await fetch('/api/opml', { credentials: 'same-origin' });
    if (!res.ok) throw new Error(t('Export failed'));
    const blob = await res.blob();
    const name = `feeds-${new Date().toISOString().slice(0, 10)}.opml`;
    const file = new File([blob], name, { type: 'text/xml' });
    if (navigator.canShare && navigator.canShare({ files: [file] })) {
      try { await navigator.share({ files: [file], title: name }); } catch (e) { /* cancelled */ }
      return;
    }
    const url = URL.createObjectURL(blob);
    const a = h('a', { href: url, download: name });
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 5000);
  } catch (e) { showError(e); }
}

// ---- admin ----

async function loadUsers(box) {
  try {
    const r = await get('/api/admin/users');
    clear(box);
    r.users.forEach((u) => {
      const badges = [];
      if (u.is_admin) badges.push('admin');
      if (u.disabled) badges.push(t('disabled'));
      box.append(h('button', { class: 'row-btn px-3 py-2', onclick: () => userMenu(u, box) },
        tile('o-users', u.disabled ? 'plain' : (u.is_admin ? 'aurora' : 'sky'), 'sm'),
        h('div', { class: 'flex-1 min-w-0' },
          h('div', { class: 'text-[1.0625rem] truncate ' + (u.disabled ? 'text-muted line-through' : ''), text: u.username }),
          h('div', { class: 'text-[0.75rem] text-muted', text: [t('{0} feeds', u.feed_count || 0), ...badges].join(', ') })),
        icon('o-ellipsis-horizontal', 'w-5 h-5 text-muted')));
    });
  } catch (e) { showError(e); }
}

function userMenu(u, box) {
  const self = u.id === state.me.user.id;
  actionSheet({
    title: u.username,
    actions: [
      {
        label: t('Reset password'), icon: 'o-key', onSelect: async () => {
          await formDialog({
            title: t('New password'), message: t('For {0}. Their sessions will be closed.', u.username), okLabel: t('Save'),
            fields: [{ name: 'password', type: 'password', placeholder: t('At least 8 characters'), autocomplete: 'new-password' }],
            validate: async (v) => {
              try { await post(`/api/admin/users/${u.id}/password`, { password: v.password }); } catch (e) { return e.message; }
              toast(t('Password reset'));
              return null;
            },
          });
        },
      },
      {
        // For a user who lost the phone and the recovery codes.
        label: t('Remove two-step verification'), icon: 'o-shield-check', onSelect: async () => {
          const ok = await confirmDialog({
            title: t('Remove the verification?'), message: t('{0} will sign in with the password alone and can turn it back on from the settings.', u.username),
            okLabel: t('Remove|2fa'), destructive: true, icon: 'o-shield-check',
          });
          if (!ok) return;
          try { await post(`/api/admin/users/${u.id}/totp/disable`); toast(t('Verification removed')); } catch (e) { showError(e); }
        },
      },
      {
        label: u.disabled ? t('Enable') : t('Disable'), icon: u.disabled ? 'o-check-circle' : 'o-x-mark', disabled: self,
        onSelect: async () => {
          try { await post(`/api/admin/users/${u.id}/${u.disabled ? 'enable' : 'disable'}`); loadUsers(box); } catch (e) { showError(e); }
        },
      },
      {
        label: t('Delete user'), icon: 'o-trash', destructive: true, disabled: self, onSelect: async () => {
          const ok = await confirmDialog({
            title: t('Delete {0}?', u.username), message: t('The user\'s feeds, folders and articles will be deleted.'),
            okLabel: t('Delete'), destructive: true,
          });
          if (!ok) return;
          try { await del(`/api/admin/users/${u.id}`); loadUsers(box); toast(t('User deleted')); } catch (e) { showError(e); }
        },
      },
    ],
  });
}

async function createUser(box) {
  const r = await formDialog({
    title: t('New user'), okLabel: t('Create'),
    fields: [
      { name: 'username', placeholder: t('Username'), autocomplete: 'off' },
      { name: 'password', type: 'password', placeholder: t('Password (min. 8 characters)'), autocomplete: 'new-password' },
      { name: 'is_admin', type: 'switch', label: t('Administrator') },
    ],
    validate: async (v) => {
      try { await post('/api/admin/users', { username: v.username.trim(), password: v.password, is_admin: v.is_admin }); } catch (e) { return e.message; }
      return null;
    },
  });
  if (r) {
    toast(t('User created'));
    loadUsers(box);
  }
}
