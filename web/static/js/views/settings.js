// Settings: the window with its sections (each in views/settings/).

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
import { rowButton, section } from './settings/parts.js';
import { appearanceControls } from './settings/appearance.js';
import { notifyControls } from './settings/notifications.js';
import { offlineControls } from './settings/offline.js';
import { readingControls } from './settings/reading.js';
import { adControls } from './settings/adfilter.js';
import { backupControls } from './settings/backups.js';
import { changePassword, exportOPML, logout, twoFactorRow } from './settings/account.js';
import { createUser, loadUsers } from './settings/admin.js';

let lastSection = 0; // the page of the settings window shown last (desktop)

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
      rowButton('o-bookmark', 'rose', t('Save for later'), openSaveHelp),
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
