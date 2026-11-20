// "Add to Home Screen" hint. The app works best installed (full screen,
// notifications, badge, offline), and iOS has no install prompt of its own:
// a sheet explains the steps when the app is open in the browser.

import { t } from './i18n.js';
import { state } from './state.js';
import { h, icon, openSheet } from './ui.js';
import { isIOS, isStandalone } from './push.js';
import * as motion from './motion.js';

const KEY = 'installHint';
const LATER_DAYS = 3;

// Chrome/Edge on Android and desktop offer a real install prompt.
let deferred = null;
window.addEventListener('beforeinstallprompt', (e) => {
  e.preventDefault();
  deferred = e;
});
window.addEventListener('appinstalled', () => { deferred = null; });

export function canInstall() {
  return !isStandalone() && (isIOS() || !!deferred);
}

function readState() {
  try { return JSON.parse(localStorage.getItem(KEY) || '{}') || {}; } catch (e) { return {}; }
}
function writeState(v) {
  try { localStorage.setItem(KEY, JSON.stringify(v)); } catch (e) { /* ignore */ }
}

// Called after login: shows the hint unless dismissed recently or forever.
export function maybeShowInstallHint() {
  if (!isIOS() || isStandalone()) return;
  const st = readState();
  if (st.never || (st.later && Date.now() - st.later < LATER_DAYS * 86400000)) return;
  setTimeout(() => { if (!document.querySelector('#overlay .sheet')) showInstallHint(); }, 1200);
}

const isPad = () => /iPad/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);

function step(n, title, detail, glyph) {
  return h('div', { class: 'install-step' },
    h('span', { class: 'install-num', text: String(n) }),
    h('div', { class: 'flex-1 min-w-0' },
      h('div', { class: 'text-[1rem] font-semibold leading-snug' }, ...title),
      detail ? h('div', { class: 'text-[0.8125rem] text-muted leading-snug mt-0.5', text: detail }) : null),
    glyph || null);
}

const chip = (...kids) => h('span', { class: 'install-chip' }, ...kids);

export function showInstallHint() {
  const name = (state.me && state.me.app_name) || document.title;
  const v = (state.me && state.me.version) || '';
  const appIcon = h('img', { class: 'install-icon', src: `/static/icons/apple-touch-icon.png?v=${encodeURIComponent(v)}`, alt: '' });

  let steps;
  let primary;
  if (isIOS()) {
    steps = [
      step(1, [t('Tap '), chip(icon('o-arrow-up-on-square')), t(' Share')],
        isPad() ? t('At the top right, next to the address.') : t('In the bottom bar. If you can\'t see it, tap ••• next to the address first.')),
      step(2, [t('Choose '), chip(icon('o-plus')), t(' Add to Home Screen')],
        t('If it\'s not in the list, scroll down or tap "More".')),
      step(3, [t('Tap Add')], t('Leave "Open as Web App" on. Then open the app from its icon on the Home Screen.')),
    ];
    primary = h('button', { class: 'btn btn-primary w-full', text: t('Got it') });
  } else {
    steps = [step(1, [t('Install the app on this device')], t('It will show up among your apps, with its icon.'))];
    primary = h('button', { class: 'btn btn-primary w-full' }, icon('o-arrow-down-tray', 'w-5 h-5'), t('Install'));
  }
  const never = h('button', { class: 'btn w-full !min-h-11 text-muted !font-medium', text: t('Don\'t show again') });

  const perks = h('div', { class: 'install-perks' },
    ...[['o-arrows-pointing-out', t('Full screen')], ['o-bell', t('Notifications')], ['o-arrow-down-tray', t('Offline reading')]]
      .map(([ic, label]) => h('div', {}, icon(ic), h('span', { text: label }))));

  const body = h('div', { class: 'px-5 pb-3 pt-2 flex flex-col items-center text-center' },
    appIcon,
    h('div', { class: 'sheet-title mt-3', text: t('Install {0}', name) }),
    h('p', { class: 'text-[0.875rem] text-muted mt-1 max-w-[30ch]', text: t('Add it to the Home Screen: it opens like a real app.') }),
    perks,
    h('div', { class: 'install-steps' }, ...steps),
    h('div', { class: 'w-full flex flex-col gap-1 mt-5' }, primary, never));

  const s = openSheet(body);
  primary.addEventListener('click', async () => {
    if (!isIOS() && deferred) {
      deferred.prompt();
      try { await deferred.userChoice; } catch (e) { /* ignore */ }
      deferred = null;
    }
    writeState({ later: Date.now() });
    s.close();
  });
  never.addEventListener('click', () => { writeState({ never: true }); s.close(); });
  motion.stagger(body.querySelectorAll('.install-step'), { y: 10, delay: 0.15 });
}
