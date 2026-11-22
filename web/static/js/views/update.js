// New version of Ebi Furai: the server asks GitHub every 12 hours; when a
// newer release is out, administrators see a card when they open the app,
// with the release notes and the command to update. "Later" hides it on
// this device for three days (a newer release shows it again at once).

import { fullDate, h, icon, openSheet, tile, toast } from '../ui.js';
import { get } from '../api.js';
import { state } from '../state.js';
import { t } from '../i18n.js';
import * as motion from '../motion.js';

const SNOOZE = 3 * 24 * 3600 * 1000;
const RECHECK = 6 * 3600 * 1000;
const KEY = 'updateSnoozed';

let lastCheck = 0;
let card = null;

const COMMANDS = {
  docker: 'docker compose pull && docker compose up -d',
  service: 'curl -fsSL https://raw.githubusercontent.com/trapassati/ebi-furai/main/scripts/install.sh | sudo sh',
};

function snoozed(version) {
  try {
    const s = JSON.parse(localStorage.getItem(KEY) || '{}');
    return s.version === version && Date.now() - s.at < SNOOZE;
  } catch (e) { return false; }
}

function snooze(version) {
  try { localStorage.setItem(KEY, JSON.stringify({ version, at: Date.now() })); } catch (e) { /* private mode */ }
}

// checkForUpdate runs when the app opens and when it comes back to the
// foreground (at most every 6 hours).
export async function checkForUpdate({ force = false } = {}) {
  if (!state.me || !state.me.user.is_admin) return;
  if (!force && Date.now() - lastCheck < RECHECK) return;
  lastCheck = Date.now();
  let r;
  try { r = await get('/api/admin/update'); } catch (e) { return; }
  state.update = r.available ? r.update : null;
  if (state.update && !snoozed(state.update.latest)) showCard(state.update);
}

function hideCard(el) {
  if (card === el) card = null;
  motion.toastOut(el).then(() => el.remove());
}

function showCard(u) {
  if (card) return;
  const el = h('div', { class: 'update-card', role: 'status' },
    tile('o-sparkles', 'aurora', 'sm'),
    h('div', { class: 'flex-1 min-w-0' },
      h('div', { class: 'font-semibold truncate', text: t('Ebi Furai {0}', u.latest) }),
      h('div', { class: 'text-[0.8125rem] text-muted truncate', text: t('New version available') })),
    h('button', { class: 'update-card-btn', type: 'button', text: t('What\'s new'), onclick: () => { hideCard(el); openUpdate(u); } }),
    h('button', {
      class: 'icon-btn update-card-close', type: 'button', 'aria-label': t('Later'), title: t('Later'),
      onclick: () => { snooze(u.latest); hideCard(el); },
    }, icon('o-x-mark', 'w-5 h-5')));
  card = el;
  document.getElementById('toasts').prepend(el);
  motion.toastIn(el);
}

// ---- release notes ----

// A small, safe Markdown reader for the release notes: paragraphs, lists,
// **bold**, `code` and [links](https://…). Text only, never HTML.
function inline(text) {
  const out = [];
  const re = /\*\*([^*]+)\*\*|`([^`]+)`|\[([^\]]+)\]\((https:\/\/[^)\s]+)\)/g;
  let last = 0;
  let m;
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index));
    if (m[1]) out.push(h('strong', { text: m[1] }));
    else if (m[2]) out.push(h('code', { text: m[2] }));
    else out.push(h('a', { href: m[4], target: '_blank', rel: 'noopener noreferrer', text: m[3] }));
    last = re.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export function renderNotes(md) {
  const box = h('div', { class: 'release-notes' });
  let list = null;
  let item = null;
  let para = null;
  for (const raw of (md || '').split('\n')) {
    const line = raw.trimEnd();
    const bullet = line.match(/^\s*[-*]\s+(.*)$/);
    if (bullet) {
      para = null;
      if (!list) { list = h('ul'); box.append(list); }
      item = h('li', {}, ...inline(bullet[1]));
      list.append(item);
    } else if (!line.trim()) {
      list = null; item = null; para = null;
    } else if (/^#{1,6}\s/.test(line)) {
      list = null; item = null; para = null;
      box.append(h('h4', { text: line.replace(/^#+\s*/, '') }));
    } else if (item && /^\s+/.test(line)) {
      item.append(' ', ...inline(line.trim())); // a list item that goes on
    } else if (para) {
      para.append(' ', ...inline(line.trim()));
    } else {
      list = null; item = null;
      para = h('p', {}, ...inline(line.trim()));
      box.append(para);
    }
  }
  return box;
}

function commandBox(label, cmd) {
  const copyBtn = h('button', { class: 'icon-btn', type: 'button', 'aria-label': t('Copy'), title: t('Copy') }, icon('o-document-duplicate', 'w-5 h-5'));
  copyBtn.addEventListener('click', () => {
    navigator.clipboard.writeText(cmd).then(() => toast(t('Copied')), () => toast(t('Copy failed'), 'error'));
  });
  return h('div', {},
    label ? h('div', { class: 'text-[0.8125rem] text-muted pb-1.5 px-1', text: label }) : null,
    h('div', { class: 'command-box' }, h('code', { text: cmd }), copyBtn));
}

export function openUpdate(u) {
  const ref = {};
  const how = [];
  if (u.install === 'docker' || !u.install) how.push(commandBox(u.install ? '' : t('With Docker, in the folder of compose.yaml:'), COMMANDS.docker));
  if (u.install === 'service' || !u.install) how.push(commandBox(u.install ? '' : t('As a Linux service:'), COMMANDS.service));
  const published = u.published_at ? fullDate(Date.parse(u.published_at) / 1000) : '';
  const body = h('div', { class: 'px-4 pb-10 flex flex-col gap-4' },
    h('div', { class: 'text-center pt-2' },
      tile('o-sparkles', 'aurora'),
      h('div', { class: 'text-[1.25rem] font-semibold pt-3', text: t('Ebi Furai {0}', u.latest) }),
      h('div', { class: 'text-[0.875rem] text-muted', text: published ? t('Released {0}. You have {1}.', published, u.current) : t('You have {0}.', u.current) })),
    h('div', { class: 'group px-4 py-3' }, renderNotes(u.notes)),
    h('div', { class: 'group-label', text: t('How to update') }),
    h('p', { class: 'text-[0.9375rem] text-muted px-1', text: u.install === 'docker'
      ? t('On the server, in the folder of compose.yaml:')
      : u.install === 'service' ? t('On the server:') : t('On the server, depending on how you installed it:') }),
    ...how,
    h('p', { class: 'text-[0.875rem] text-muted px-1', text: t('The app restarts in a few seconds, and devices get the new version the next time they open it.') }),
    u.url ? h('a', { class: 'btn btn-plain w-full', href: u.url, target: '_blank', rel: 'noopener noreferrer' },
      icon('o-arrow-top-right-on-square', 'w-5 h-5'), t('Release on GitHub')) : null);
  const header = h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
    h('div', { class: 'w-20' }),
    h('div', { class: 'flex-1 text-center sheet-title', text: t('Update') }),
    h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') }));
  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, header, body), { full: true, wide: true });
}

// openFromNotification: the update notification opens the app at
// "#/update". The release may have been installed meanwhile.
export async function openFromNotification() {
  if (!state.me || !state.me.user.is_admin) return;
  let r;
  try { r = await get('/api/admin/update'); } catch (e) { return; }
  if (r.available && r.update) {
    state.update = r.update;
    if (card) hideCard(card);
    openUpdate(r.update);
  } else {
    toast(t('Ebi Furai is up to date'));
  }
}
