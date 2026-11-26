// Settings: a list of sections, each one a page (built in views/settings/).
//
// Wide windows (the desktop, an iPad held sideways) show the list on the
// left and one page at a time on the right. Narrow ones (the iPhone, an
// iPad upright or in Split View) work like the Settings app of iOS: the
// list first, a page slides in from the right, "‹ Settings" or a swipe from
// the left edge goes back.

import { t } from '../i18n.js';
import { api } from '../api.js';
import { state } from '../state.js';
import { h, icon, openSheet, showError, tile, toast } from '../ui.js';
import * as motion from '../motion.js';
import { canInstall, showInstallHint } from '../install.js';
import { openAbout } from './about.js';
import { openApps } from './apps.js';
import { openSaveHelp } from './saved.js';
import { openUpdate } from './update.js';
import { failingCount, openFeedHealth } from './health.js';
import { openFilters } from './filters.js';
import { openSessions } from './security.js';
import { showShortcuts } from '../shortcuts.js';
import { isDesk } from '../desk.js';
import { pageRow, rowButton, section } from './settings/parts.js';
import { appearanceControls, soundControls } from './settings/appearance.js';
import { notifyControls } from './settings/notifications.js';
import { offlineControls } from './settings/offline.js';
import { readingControls } from './settings/reading.js';
import { adControls } from './settings/adfilter.js';
import { backupControls } from './settings/backups.js';
import { websubControls } from './settings/websub.js';
import { changePassword, exportOPML, logout, twoFactorRow } from './settings/account.js';
import { createUser, loadUsers } from './settings/admin.js';

let lastPage = 'appearance'; // the page shown last in a wide window

// Wide enough for the list and a page side by side: the desktop, and an
// iPad held sideways (not in Split View).
const sideBySide = () => isDesk() || (window.innerWidth >= 1000 && window.innerHeight >= 600);

const subLabel = (text) => h('div', { class: 'group-label', text });
const note = (text) => h('div', { class: 'text-[0.8125rem] text-muted px-1', text });

export function openSettings(ctx) {
  const me = state.me;
  const admin = me.user.is_admin;
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

  const failing = failingCount();
  const desktopKeys = window.matchMedia('(hover: hover) and (pointer: fine)').matches || navigator.keyboard;

  // The pages, in the order of the list; group puts them in the same box.
  const pages = [
    { id: 'account', group: 'me', label: 'Account', icon: 'o-user-circle', tone: 'indigo', body: () => [
      h('div', { class: 'group' },
        rowButton('o-key', 'sky', t('Change password'), changePassword),
        twoFactorRow(),
        rowButton('o-device-phone-mobile', 'indigo', t('Connected devices'), openSessions)),
      h('div', { class: 'group mt-6' },
        rowButton('o-arrow-right-start-on-rectangle', 'rose', t('Sign out'), logout, 'text-danger'))] },
    { id: 'appearance', group: 'device', label: t('Appearance'), icon: 'o-swatch', tone: 'aurora', body: () => [appearanceControls()] },
    { id: 'sounds', group: 'device', label: t('Sounds'), icon: 'o-speaker-wave', tone: 'rose', body: () => [
      h('div', { class: 'flex flex-col gap-3' }, note(t('They only apply to this device.')), soundControls())] },
    { id: 'reading', group: 'device', label: t('Reading'), icon: 'o-newspaper', tone: 'sky', body: () => [
      readingControls(),
      subLabel('Offline'), offlineControls(),
      // Administrators change the ad filter in its own page.
      admin ? null : subLabel(t('Ads')), admin ? null : adControls(false)] },
    { id: 'notifications', group: 'device', label: t('Notifications'), icon: 'o-bell', tone: 'rose', body: () => [notifyControls()] },
    { id: 'feeds', group: 'sources', label: t('Feeds'), icon: 'o-rss', tone: 'sunset', detail: failing ? String(failing) : '', body: () => [
      h('div', { class: 'group' },
        rowButton('o-exclamation-triangle', failing ? 'rose' : 'mint',
          failing ? t('Feed health: {0} with problems', failing) : t('Feed health'), openFeedHealth),
        rowButton('o-funnel', 'indigo', t('Keyword filters'), () => openFilters())),
      h('div', { class: 'group mt-6' },
        rowButton('o-arrow-up-tray', 'mint', t('Import OPML'), () => fileInput.click()),
        rowButton('o-arrow-down-tray', 'sunset', t('Export OPML'), exportOPML)),
      fileInput] },
    { id: 'apps', group: 'sources', label: t('Apps and integrations'), icon: 'o-squares-2x2', tone: 'indigo', body: () => [
      h('div', { class: 'group' },
        rowButton('o-squares-2x2', 'sunset', t('Reeder and other apps'), openApps),
        rowButton('o-bookmark', 'rose', t('Save for later'), openSaveHelp),
        canInstall() ? rowButton('o-plus', 'indigo', t('Add to Home Screen'), () => { sheet.close(); setTimeout(showInstallHint, 350); }) : null)] },
    admin ? { id: 'users', group: 'admin', label: t('Users'), icon: 'o-users', tone: 'indigo', body: () => [usersBox,
      h('button', { class: 'btn btn-primary w-full mt-4', onclick: () => createUser(usersBox) }, icon('o-user-plus', 'w-5 h-5'), t('New user'))] } : null,
    admin ? { id: 'backup', group: 'admin', label: 'Backup', icon: 'o-circle-stack', tone: 'mint', body: () => [backupControls()] } : null,
    admin ? { id: 'ads', group: 'admin', label: t('Ad filter'), icon: 'o-no-symbol', tone: 'sunset', body: () => [adControls(true)] } : null,
    admin ? { id: 'websub', group: 'admin', label: t('Real time'), icon: 'o-bolt', tone: 'sunset', body: () => [websubControls()] } : null,
    { id: 'about', group: 'about', label: t('About'), icon: 'o-information-circle', tone: 'plain', detail: state.update ? '1' : '', body: () => [
      h('div', { class: 'group' },
        desktopKeys ? rowButton('o-command-line', 'plain', t('Keyboard shortcuts'), showShortcuts) : null,
        state.update ? rowButton('o-sparkles', 'aurora', t('Update available: {0}', state.update.latest), () => openUpdate(state.update)) : null,
        rowButton('o-information-circle', 'plain', t('About Ebi Furai'), openAbout))] },
  ].filter(Boolean);
  // Each page is built once and moved between the layouts.
  for (const p of pages) p.el = section(p.label, p.icon, p.tone, ...p.body());
  const byId = Object.fromEntries(pages.map((p) => [p.id, p]));
  const groupLabel = { admin: t('Administration') };
  const version = () => h('div', { class: 'text-center text-[0.75rem] text-muted pt-8', text: t('{0}, version {1}', me.app_name, me.version) });
  const role = me.user.is_admin ? t('Administrator') : t('User');

  // The list of pages grouped in boxes, with the account on top (narrow).
  const pageList = (open) => {
    const boxes = [];
    let box = null, group = '';
    for (const p of pages) {
      if (p.id === 'account') continue;
      if (p.group !== group) {
        group = p.group;
        if (groupLabel[group]) boxes.push(subLabel(groupLabel[group]));
        box = h('div', { class: 'group' + (groupLabel[group] ? '' : ' mt-6') });
        boxes.push(box);
      }
      box.append(pageRow(p, () => open(p.id)));
    }
    const card = h('button', { class: 'row-btn settings-me px-3 py-3', dataset: { page: 'account' }, onclick: () => open('account') },
      tile('o-user-circle', 'aurora'),
      h('span', { class: 'flex-1 min-w-0 text-left' },
        h('span', { class: 'block text-[1.0625rem] font-semibold truncate', text: me.user.username }),
        h('span', { class: 'block text-[0.8125rem] text-muted', text: `${role} · ${t('Password, devices, sign out')}` })),
      icon('o-chevron-right', 'w-4 h-4 text-faint'));
    return h('div', { class: 'px-4 pt-2 pb-6 settings-list' }, h('div', { class: 'group' }, card), ...boxes, version());
  };

  const root = h('div', { class: 'settings-root' });
  let sheet;
  let mode = '';

  // ---- wide: the list on the left, a page on the right ----
  const wide = () => {
    const header = h('div', { class: 'flex items-center px-2 h-14 bg-bg z-10 settings-head' },
      h('div', { class: 'w-20' }),
      h('div', { class: 'flex-1 text-center sheet-title', text: t('Settings') }),
      h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => sheet.close(), text: t('Done') }));
    const page = h('div', { class: 'settings-page' });
    const nav = h('nav', { class: 'settings-nav', 'aria-label': t('Settings') });
    const items = [];
    let group = '';
    for (const p of pages) {
      if (p.group !== group) {
        if (group) nav.append(groupLabel[p.group] ? h('div', { class: 'settings-nav-label', text: groupLabel[p.group] }) : h('div', { class: 'settings-nav-gap' }));
        group = p.group;
      }
      const item = h('button', { type: 'button', class: 'settings-nav-item', dataset: { page: p.id }, onclick: () => show(p.id) },
        tile(p.icon, p.tone, isDesk() ? 'xs' : 'sm'), h('span', { class: 'flex-1 truncate', text: p.id === 'account' ? me.user.username : p.label }),
        p.detail ? h('span', { class: 'settings-nav-badge', text: p.detail }) : null);
      if (p.id === 'account') item.title = 'Account';
      items.push(item);
      nav.append(item);
    }
    const show = (id) => {
      lastPage = id;
      items.forEach((b) => { const on = b.dataset.page === id; b.classList.toggle('on', on); b.setAttribute('aria-current', on ? 'page' : 'false'); });
      page.replaceChildren(byId[id].el);
      page.scrollTop = 0;
      if (motion.enabled()) window.gsap.fromTo(byId[id].el, { opacity: 0, y: 6 }, { opacity: 1, y: 0, duration: 0.2, ease: 'power2.out', clearProps: 'transform' });
    };
    nav.append(h('div', { class: 'flex-1' }), version());
    root.className = 'settings-root wide';
    root.replaceChildren(header, h('div', { class: 'settings-split' }, nav, page));
    show(byId[lastPage] ? lastPage : 'appearance');
  };

  // ---- narrow: the list, and a page slides in over it ----
  const narrow = () => {
    const back = h('button', { class: 'icon-btn on justify-self-start min-w-0 max-w-full font-medium text-[1.0625rem] settings-back invisible', type: 'button', 'aria-label': t('Back') },
      icon('o-chevron-left', 'w-5 h-5 flex-none'), h('span', { class: 'truncate', text: t('Settings') }));
    const title = h('div', { class: 'text-center sheet-title truncate min-w-0', text: t('Settings') });
    const header = h('div', { class: 'settings-bar px-2 h-14 bg-bg z-10 settings-head' },
      back, title,
      h('button', { class: 'icon-btn on justify-self-end pr-2 font-semibold text-[1.0625rem]', onclick: () => sheet.close(), text: t('Done') }));
    const stack = h('div', { class: 'settings-stack' });
    const listView = h('div', { class: 'settings-view' });
    const pageView = h('div', { class: 'settings-view settings-pageview hidden' });
    let current = null;
    const gsap = motion.enabled() ? window.gsap : null;

    const setTitle = (text) => {
      if (!gsap) { title.textContent = text; return; }
      gsap.to(title, { opacity: 0, duration: 0.1, ease: 'none', onComplete: () => { title.textContent = text; gsap.to(title, { opacity: 1, duration: 0.18, ease: 'none' }); } });
    };
    const open = (id) => {
      if (current) return;
      current = id;
      pageView.replaceChildren(h('div', { class: 'px-4 pb-8' }, byId[id].el));
      pageView.scrollTop = 0;
      pageView.classList.remove('hidden');
      back.classList.remove('invisible');
      setTitle(byId[id].label);
      if (gsap) {
        gsap.fromTo(pageView, { xPercent: 100 }, { xPercent: 0, duration: 0.42, ease: 'power3.out' });
        gsap.fromTo(listView, { xPercent: 0, opacity: 1 }, { xPercent: -28, opacity: 0.4, duration: 0.42, ease: 'power3.out' });
        gsap.fromTo(back, { opacity: 0, x: 12 }, { opacity: 1, x: 0, duration: 0.3, delay: 0.08 });
      } else listView.classList.add('hidden');
    };
    // close: back to the list, from where a swipe left the page (0..1).
    const close = (from = 0) => {
      if (!current) return;
      current = null;
      setTitle(t('Settings'));
      const done = () => { pageView.classList.add('hidden'); back.classList.add('invisible'); pageView.replaceChildren(); };
      listView.classList.remove('hidden');
      if (gsap) {
        gsap.to(back, { opacity: 0, duration: 0.15 });
        gsap.fromTo(pageView, { xPercent: from * 100 }, { xPercent: 100, duration: 0.34, ease: 'power3.out', onComplete: done });
        gsap.fromTo(listView, { xPercent: -28 * (1 - from), opacity: 0.4 + 0.6 * from }, { xPercent: 0, opacity: 1, duration: 0.34, ease: 'power3.out', clearProps: 'transform,opacity' });
      } else done();
    };
    back.addEventListener('click', () => close());

    // Swipe from the left edge of the window to go back.
    let x0 = 0, y0 = 0, dx = 0, t0 = 0, track = '', width = 1;
    stack.addEventListener('touchstart', (e) => {
      track = '';
      if (!current || e.touches.length !== 1) return;
      const r = stack.getBoundingClientRect();
      x0 = e.touches[0].clientX; y0 = e.touches[0].clientY;
      if (x0 - r.left > 28) return;
      width = r.width; dx = 0; t0 = e.timeStamp; track = 'maybe';
    }, { passive: true });
    stack.addEventListener('touchmove', (e) => {
      if (!track) return;
      const mx = e.touches[0].clientX - x0, my = e.touches[0].clientY - y0;
      if (track === 'maybe') {
        if (Math.abs(mx) < 8 && Math.abs(my) < 8) return;
        if (mx <= 0 || Math.abs(my) > Math.abs(mx)) { track = ''; return; }
        track = 'swipe';
      }
      if (e.cancelable) e.preventDefault();
      dx = Math.max(0, mx);
      const f = Math.min(dx / width, 1);
      pageView.style.transform = `translateX(${dx}px)`;
      listView.style.transform = `translateX(${-28 * (1 - f)}%)`;
      listView.style.opacity = String(0.4 + 0.6 * f);
    }, { passive: false });
    const end = (e) => {
      if (track !== 'swipe') { track = ''; return; }
      track = '';
      const f = Math.min(dx / width, 1);
      const fast = dx / Math.max(e.timeStamp - t0, 1) > 0.5;
      pageView.style.transform = '';
      if (f > 0.35 || fast) { close(f); return; }
      if (gsap) {
        gsap.fromTo(pageView, { x: dx }, { x: 0, duration: 0.28, ease: 'power3.out', clearProps: 'transform' });
        gsap.fromTo(listView, { xPercent: -28 * (1 - f), opacity: 0.4 + 0.6 * f }, { xPercent: -28, opacity: 0.4, duration: 0.28, ease: 'power3.out' });
      } else { listView.style.transform = ''; listView.style.opacity = ''; }
    };
    stack.addEventListener('touchend', end);
    stack.addEventListener('touchcancel', end);

    listView.append(pageList(open));
    stack.append(listView, pageView);
    root.className = 'settings-root narrow';
    root.replaceChildren(header, stack);
  };

  const layout = () => {
    const next = sideBySide() ? 'wide' : 'narrow';
    if (next === mode) return;
    mode = next;
    if (mode === 'wide') wide(); else narrow();
  };
  layout();
  sheet = openSheet(root, { full: true, wide: true, cls: 'settings-sheet' });
  // Turning the iPad or entering Split View changes the layout.
  const onResize = () => layout();
  window.addEventListener('resize', onResize);
  document.addEventListener('deskchange', onResize);
  sheet.done.then(() => { window.removeEventListener('resize', onResize); document.removeEventListener('deskchange', onResize); });
  if (admin) loadUsers(usersBox);
}
