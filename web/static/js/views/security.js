// Account security: connected devices (sessions) and two-step verification
// (TOTP codes from an authenticator app, the iOS Passwords app included).

import { locale, t } from '../i18n.js';
import { del, get, post } from '../api.js';
import { state } from '../state.js';
import { actionSheet, confirmDialog, formDialog, h, icon, openSheet, showError, tile, toast } from '../ui.js';
import * as motion from '../motion.js';
import { encode } from '../../vendor/uqr-0.1.3.js';

// ---- connected devices ----

// A readable name from the browser's user agent: "iPhone, Safari".
function deviceName(ua) {
  const u = ua || '';
  let device = t('Device');
  let kind = 'desktop';
  if (/iPad/.test(u) || (/Macintosh/.test(u) && /Mobile\//.test(u))) { device = 'iPad'; kind = 'tablet'; }
  else if (/iPhone/.test(u)) { device = 'iPhone'; kind = 'phone'; }
  else if (/Android/.test(u)) { device = 'Android'; kind = /Mobile/.test(u) ? 'phone' : 'tablet'; }
  else if (/Macintosh|Mac OS X/.test(u)) device = 'Mac';
  else if (/Windows/.test(u)) device = 'Windows';
  else if (/Linux|X11/.test(u)) device = 'Linux';
  let browser = '';
  if (/Edg\//.test(u)) browser = 'Edge';
  else if (/Firefox\/|FxiOS/.test(u)) browser = 'Firefox';
  else if (/Chrome\/|CriOS/.test(u)) browser = 'Chrome';
  else if (/Safari\//.test(u)) browser = 'Safari';
  else if (/AppleWebKit/.test(u) && (kind === 'phone' || kind === 'tablet')) browser = t('installed app');
  return { name: browser ? `${device}, ${browser}` : device, kind };
}

const ago = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
function since(ts) {
  const s = Math.round(ts - Date.now() / 1000);
  const abs = Math.abs(s);
  if (abs < 120) return 'adesso';
  if (abs < 3600) return ago.format(Math.round(s / 60), 'minute');
  if (abs < 86400) return ago.format(Math.round(s / 3600), 'hour');
  return ago.format(Math.round(s / 86400), 'day');
}

function sheetHeader(title, ref) {
  return h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
    h('div', { class: 'w-20' }),
    h('div', { class: 'flex-1 text-center sheet-title', text: title }),
    h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') }));
}

export function openSessions() {
  const ref = {};
  const list = h('div', { class: 'group' }, h('div', { class: 'px-4 py-3 text-muted', text: t('Loading…') }));
  const others = h('button', { class: 'btn btn-plain w-full text-danger' }, icon('o-arrow-right-start-on-rectangle', 'w-5 h-5'), t('Sign out all the others'));

  const load = async () => {
    let r;
    try { r = await get('/api/me/sessions'); } catch (e) { showError(e); return; }
    others.hidden = r.sessions.length < 2;
    list.replaceChildren(...r.sessions.map((s) => {
      const d = deviceName(s.user_agent);
      const where = s.ip ? t(', from {0}', s.ip) : '';
      const row = h(s.current ? 'div' : 'button', { class: 'row-btn px-3 py-2.5 w-full text-left' },
        tile(d.kind === 'phone' ? 'o-device-phone-mobile' : d.kind === 'tablet' ? 'o-device-tablet' : 'o-computer-desktop',
          s.current ? 'mint' : 'indigo', 'sm'),
        h('span', { class: 'flex-1 min-w-0' },
          h('span', { class: 'block text-[1rem] truncate', text: d.name }),
          h('span', { class: 'block text-[0.8125rem] text-muted', text: s.current ? t('This device') : t('Last used {0}{1}', since(s.last_seen), where) })),
        s.current ? null : icon('o-x-mark', 'w-5 h-5 text-faint flex-none'));
      if (!s.current) {
        row.addEventListener('click', async () => {
          const ok = await confirmDialog({
            title: t('Sign out the device?'), message: t('{0} will have to sign in again with the password.', d.name),
            okLabel: t('Sign out|device'), destructive: true, icon: 'o-arrow-right-start-on-rectangle',
          });
          if (!ok) return;
          try {
            await del(`/api/me/sessions/${s.id}`);
            await motion.collapse([row]);
            toast(t('Device signed out'));
            load();
          } catch (e) { showError(e); }
        });
      }
      return row;
    }));
  };
  others.addEventListener('click', async () => {
    const ok = await confirmDialog({
      title: t('Sign out the other devices?'), message: t('You\'ll stay signed in only here; the others will need the password again.'),
      okLabel: t('Sign out|device'), destructive: true, icon: 'o-arrow-right-start-on-rectangle',
    });
    if (!ok) return;
    try { await post('/api/me/sessions/logout-others'); toast(t('Other devices signed out')); load(); } catch (e) { showError(e); }
  });
  load();
  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, sheetHeader(t('Connected devices'), ref),
    h('div', { class: 'px-4 pb-10 flex flex-col gap-3' },
      h('p', { class: 'text-[0.9375rem] text-muted px-1', text: t('Where your account is open. If you don\'t recognize a device, sign it out and change the password.') }),
      list, others)), { full: true, wide: true });
}

// ---- two-step verification ----

function qrSVG(text) {
  const qr = encode(text, { ecc: 'M', border: 2 });
  const n = qr.size;
  let d = '';
  qr.data.forEach((row, y) => row.forEach((on, x) => { if (on) d += `M${x} ${y}h1v1h-1z`; }));
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${n} ${n}`);
  svg.setAttribute('class', 'qr');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', t('QR code for the authenticator app'));
  const bg = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
  bg.setAttribute('width', n); bg.setAttribute('height', n); bg.setAttribute('fill', '#fff');
  const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  p.setAttribute('d', d); p.setAttribute('fill', '#000');
  svg.append(bg, p);
  return svg;
}

async function askPassword(title, message, url, extra = {}) {
  let result = null;
  const ok = await formDialog({
    title, message, okLabel: t('Continue'),
    fields: [{ name: 'password', type: 'password', placeholder: 'Password', autocomplete: 'current-password' }],
    validate: async (v) => {
      try { result = await post(url, { password: v.password, ...extra }); } catch (e) { return e.message; }
      return null;
    },
  });
  return ok ? result : null;
}

function copy(text) {
  navigator.clipboard.writeText(text).then(() => toast(t('Copied')), () => toast(t('Copy failed'), 'error'));
}

// The recovery codes, shown once: copy or save them.
function recoveryView(codes, done) {
  const text = t('Ebi Furai recovery codes ({0})\nEach code works only once.\n\n{1}\n', state.me.user.username, codes.join('\n'));
  const save = h('a', { class: 'btn btn-plain flex-1', download: t('ebi-furai-recovery-codes.txt') }, icon('o-arrow-down-tray', 'w-5 h-5'), t('Save'));
  save.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
  return h('div', { class: 'flex flex-col gap-4' },
    h('div', { class: 'text-center' },
      icon('o-shield-check', 'w-12 h-12 mx-auto text-ok'),
      h('p', { class: 'text-[1.125rem] font-semibold mt-2', text: t('Save the recovery codes') })),
    h('p', { class: 'text-[0.9375rem] text-muted', text: t('If you lose your phone, each of these codes lets you in once instead of the app\'s code. Keep them somewhere safe, for example in your password manager\'s notes or printed: you won\'t see them again.') }),
    h('div', { class: 'recovery-codes' }, ...codes.map((c) => h('code', { text: c }))),
    h('div', { class: 'flex gap-3' },
      h('button', { class: 'btn btn-plain flex-1', onclick: () => copy(text) }, icon('o-document-duplicate', 'w-5 h-5'), t('Copy')),
      save),
    h('button', { class: 'btn btn-primary w-full', text: t('I\'ve saved them'), onclick: done }));
}

export async function openTwoFactor(onChange) {
  const me = state.me;
  if (me.totp_enabled) {
    const choice = await actionSheet({
      title: t('Two-step verification'),
      message: t('On. Recovery codes left: {0}.', me.recovery_left),
      actions: [
        { label: t('New recovery codes'), icon: 'o-key', key: 'codes' },
        { label: t('Turn off verification'), icon: 'o-x-mark', destructive: true, key: 'off' },
      ],
    });
    if (!choice) return;
    if (choice.key === 'codes') {
      const r = await askPassword(t('New recovery codes'), t('The previous codes stop working.'), '/api/me/totp/recovery');
      if (!r) return;
      const ref = {};
      ref.sheet = openSheet(h('div', { class: 'px-4 pt-2 pb-6' }, recoveryView(r.recovery_codes, () => ref.sheet.close())), { wide: true, modal: true });
      me.recovery_left = r.recovery_codes.length;
      onChange();
    } else {
      const r = await askPassword(t('Turn off verification?'), t('The password alone will be enough to sign in again.'), '/api/me/totp/disable');
      if (!r) return;
      me.totp_enabled = false;
      me.recovery_left = 0;
      toast(t('Two-step verification turned off'));
      onChange();
    }
    return;
  }

  const setup = await askPassword(t('Two-step verification'), t('For safety, confirm your password.'), '/api/me/totp/setup');
  if (!setup) return;
  const ref = {};
  const code = h('input', {
    class: 'field text-center tracking-[0.3em] text-[1.25rem]', inputmode: 'numeric', autocomplete: 'one-time-code',
    maxlength: '7', placeholder: '000000',
  });
  const error = h('div', { class: 'text-danger text-[0.875rem] text-center min-h-5' });
  const enable = h('button', { class: 'btn btn-primary w-full', text: t('Turn on') });
  const body = h('div', { class: 'flex flex-col gap-4' },
    h('p', { class: 'text-[0.9375rem] text-muted', text: t('Besides the password, signing in will need a 6-digit code that changes every 30 seconds, made by an app on your phone: iPhone Passwords, Google Authenticator, 1Password…') }),
    h('div', { class: 'text-[1rem] font-semibold', text: t('1. Add Ebi Furai to the app') }),
    h('div', { class: 'qr-box' }, qrSVG(setup.uri)),
    h('a', { class: 'btn btn-plain w-full', href: setup.uri }, icon('o-key', 'w-5 h-5'), t('Add to this device\'s app')),
    h('div', { class: 'text-[0.8125rem] text-muted text-center', text: t('Or type the key by hand:') }),
    h('button', { class: 'secret-key', title: t('Copy the key'), onclick: () => copy(setup.secret.replace(/ /g, '')) }, setup.secret),
    h('div', { class: 'text-[1rem] font-semibold pt-2', text: t('2. Type the code the app shows') }),
    h('div', { class: 'field-wrap' }, icon('o-shield-check'), code),
    error, enable);
  const box = h('div', { class: 'px-4 pb-8' }, body);
  const submit = async () => {
    error.textContent = '';
    enable.disabled = true;
    try {
      const r = await post('/api/me/totp/enable', { code: code.value });
      me.totp_enabled = true;
      me.recovery_left = r.recovery_codes.length;
      onChange();
      box.replaceChildren(recoveryView(r.recovery_codes, () => ref.sheet.close()));
      motion.swapIn(box.children, 1);
    } catch (e) {
      error.textContent = e.message;
      motion.shake(code.parentElement);
      code.select();
    } finally {
      enable.disabled = false;
    }
  };
  enable.addEventListener('click', submit);
  code.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit(); });
  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, sheetHeader(t('Two-step verification'), ref), box), { full: true, wide: true });
}

export function twoFactorLabel() {
  const me = state.me;
  return me.totp_enabled ? t('Two-step verification: on') : t('Two-step verification');
}
