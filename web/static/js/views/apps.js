// Reeder, NetNewsWire and other apps: they sync with Ebi Furai through the
// Google Reader API (the one of FreshRSS), signing in with an app password.

import { del, get, post } from '../api.js';
import { state } from '../state.js';
import { locale, t } from '../i18n.js';
import { confirmDialog, formDialog, h, icon, openSheet, showError, tile, toast } from '../ui.js';
import * as motion from '../motion.js';

const ago = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
function since(ts) {
  const s = Math.round(ts - Date.now() / 1000);
  const abs = Math.abs(s);
  if (abs < 120) return ago.format(0, 'second');
  if (abs < 3600) return ago.format(Math.round(s / 60), 'minute');
  if (abs < 86400) return ago.format(Math.round(s / 3600), 'hour');
  return ago.format(Math.round(s / 86400), 'day');
}

function copy(text) {
  navigator.clipboard.writeText(text).then(() => toast(t('Copied')), () => toast(t('Copy failed'), 'error'));
}

// A value to type in the app, with a copy button.
export function valueRow(label, value, mono = false) {
  return h('div', { class: 'flex items-center gap-3 px-4 py-2.5' },
    h('div', { class: 'flex-1 min-w-0' },
      h('div', { class: 'text-[0.8125rem] text-muted', text: label }),
      h('div', { class: 'text-[1rem] break-all' + (mono ? ' font-mono' : ''), text: value })),
    h('button', { class: 'icon-btn flex-none', type: 'button', 'aria-label': t('Copy {0}', label), title: t('Copy'), onclick: () => copy(value) },
      icon('o-document-duplicate', 'w-5 h-5')));
}

export function serverAddress() {
  return location.origin + '/api/greader.php';
}

// The new password, shown once.
function showSecret(name, secret, extra = null) {
  const ref = {};
  const body = h('div', { class: 'px-4 pt-2 pb-6 flex flex-col gap-4' },
    h('div', { class: 'text-center' },
      icon('o-key', 'w-12 h-12 mx-auto text-ok'),
      h('p', { class: 'text-[1.125rem] font-semibold mt-2', text: t('App password for {0}', name) })),
    h('p', { class: 'text-[0.9375rem] text-muted', text: extra ? extra.message : t('Type it in the app instead of your password. You won\'t see it again: if you lose it, make a new one.') }),
    h('button', { class: 'secret-key app-secret', type: 'button', title: t('Copy'), onclick: () => copy(secret) }, secret),
    extra ? extra.rows(secret) : h('div', { class: 'group' },
      valueRow(t('Server'), serverAddress()),
      valueRow(t('Username'), state.me.user.username)),
    h('button', { class: 'btn btn-primary w-full', text: t('Done'), onclick: () => ref.sheet.close() }));
  ref.sheet = openSheet(body, { wide: true, modal: true });
  return ref.sheet.done;
}

// createAppPassword asks a name and the password, then shows the new
// secret once. Returns true when one was made.
export async function createAppPassword({ name = '', placeholder = t('For example: Reeder on the iPhone'), extra = null } = {}) {
  let made = null;
  const ok = await formDialog({
    title: t('New app password'),
    message: t('A name to recognize it, then your password to confirm.'),
    okLabel: t('Create'),
    fields: [
      { name: 'name', value: name, placeholder, autocapitalize: 'sentences' },
      { name: 'password', type: 'password', placeholder: 'Password', autocomplete: 'current-password' },
    ],
    validate: async (v) => {
      try { made = await post('/api/me/app-passwords', { name: v.name, password: v.password }); } catch (e) { return e.message; }
      return null;
    },
  });
  if (!ok || !made) return false;
  await showSecret(made.item.name, made.secret, extra);
  return true;
}

export function openApps() {
  const ref = {};
  const list = h('div', { class: 'group' });

  const load = async () => {
    let r;
    try { r = await get('/api/me/app-passwords'); } catch (e) { showError(e); return; }
    if (!r.items.length) {
      list.replaceChildren(h('div', { class: 'px-4 py-3 text-muted text-[0.9375rem]', text: t('No app passwords yet.') }));
      return;
    }
    list.replaceChildren(...r.items.map((p) => {
      const used = p.last_used_at ? t('Last used {0}{1}', since(p.last_used_at), p.last_ip ? t(', from {0}', p.last_ip) : '') : t('Never used');
      const row = h('button', { class: 'row-btn px-3 py-2.5 w-full text-left app-pass' },
        tile('o-key', p.last_used_at ? 'mint' : 'plain', 'sm'),
        h('span', { class: 'flex-1 min-w-0' },
          h('span', { class: 'block text-[1rem] truncate', text: p.name }),
          h('span', { class: 'block text-[0.8125rem] text-muted', text: used })),
        icon('o-x-mark', 'w-5 h-5 text-faint flex-none'));
      row.addEventListener('click', async () => {
        const ok = await confirmDialog({
          title: t('Revoke "{0}"?', p.name), message: t('The apps using it are signed out and will need a new app password.'),
          okLabel: t('Revoke'), destructive: true, icon: 'o-key',
        });
        if (!ok) return;
        try {
          await del(`/api/me/app-passwords/${p.id}`);
          await motion.collapse([row]);
          toast(t('App password revoked'));
          load();
        } catch (e) { showError(e); }
      });
      return row;
    }));
  };

  const create = async () => {
    if (await createAppPassword()) load();
  };

  const steps = (app, where) => h('div', { class: 'px-4 py-3' },
    h('div', { class: 'text-[1rem] font-semibold', text: app }),
    h('div', { class: 'text-[0.875rem] text-muted', text: where }));

  load();
  const content = h('div', { class: 'px-4 pb-10 flex flex-col gap-3' },
    h('p', { class: 'text-[0.9375rem] text-muted px-1', text: t('Apps like Reeder and NetNewsWire can show your feeds and keep read and starred articles in sync with Ebi Furai. Add a FreshRSS account in the app with these details:') }),
    h('div', { class: 'group' },
      valueRow(t('Server'), serverAddress()),
      valueRow(t('Username'), state.me.user.username),
      h('div', { class: 'px-4 py-2.5' },
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('Password') }),
        h('div', { class: 'text-[1rem]', text: t('An app password, made below (not your password)') }))),
    h('div', { class: 'group-label', text: t('In the apps') }),
    h('div', { class: 'group' },
      steps('Reeder', t('Add account → FreshRSS, then the server, username and app password.')),
      steps('NetNewsWire', t('Settings → Accounts → + → FreshRSS.')),
      steps(t('Other apps'), t('Choose FreshRSS or "Google Reader API" when adding the account.'))),
    h('div', { class: 'group-label', text: t('App passwords') }),
    h('p', { class: 'text-[0.875rem] text-muted px-1', text: t('One for each app or device: they work with two-step verification too, and revoking one signs out only that app.') }),
    list,
    h('button', { class: 'btn btn-primary w-full', onclick: create }, icon('o-plus', 'w-5 h-5'), t('New app password')));
  const header = h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
    h('div', { class: 'w-20' }),
    h('div', { class: 'flex-1 text-center sheet-title', text: t('Reeder and other apps') }),
    h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') }));
  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, header, content), { full: true, wide: true });
}
