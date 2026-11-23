// Account: two-step verification, password, sign-out, OPML export.

import { t } from '../../i18n.js';
import { post, put } from '../../api.js';
import { state } from '../../state.js';
import { confirmDialog, formDialog, h, showError, toast } from '../../ui.js';
import * as offline from '../../offline/index.js';
import * as push from '../../push.js';
import { openTwoFactor, twoFactorLabel } from '../security.js';
import { rowButton } from './parts.js';

export function twoFactorRow() {
  const row = rowButton('o-shield-check', state.me.totp_enabled ? 'mint' : 'plain', twoFactorLabel(), () => openTwoFactor(paint));
  const label = [...row.querySelectorAll('span')].find((s) => s.textContent === twoFactorLabel());
  function paint() {
    if (label) label.textContent = twoFactorLabel();
    const tl = row.querySelector('.tile');
    if (tl) tl.className = tl.className.replace(/tile-\w+/, state.me.totp_enabled ? 'tile-mint' : 'tile-plain');
  }
  return row;
}

export async function changePassword() {
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

export async function logout() {
  const ok = await confirmDialog({ title: t('Sign out?'), message: t('The offline data on this device will be deleted.'), okLabel: t('Sign out'), destructive: true, icon: 'o-arrow-right-start-on-rectangle' });
  if (!ok) return;
  await push.disable();
  try { await post('/api/logout'); } catch (e) { /* ignore */ }
  await offline.wipe();
  location.reload();
}

export async function exportOPML() {
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
