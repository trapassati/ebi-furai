// Login screen.

import { t } from '../i18n.js';
import { get, post } from '../api.js';
import { h, clear, icon, paperArt } from '../ui.js';
import * as motion from '../motion.js';

export function showLogin(onSuccess) {
  const root = document.getElementById('login');
  clear(root);
  root.className = 'fixed inset-0 flex items-center justify-center welcome px-6 safe-x overflow-y-auto';

  const user = h('input', {
    class: 'field', name: 'username', type: 'text', autocomplete: 'username', autocapitalize: 'off',
    autocorrect: 'off', spellcheck: 'false', placeholder: t('Username'), required: true,
  });
  const pass = h('input', {
    class: 'field', name: 'password', type: 'password', autocomplete: 'current-password', placeholder: 'Password', required: true,
  });
  const error = h('div', { class: 'text-danger text-sm text-center min-h-5 px-2' });
  const submit = h('button', { type: 'submit', class: 'btn btn-primary w-full mt-1', text: t('Sign in') });

  // Original illustration: three article cards, floating.
  const art = paperArt();

  const form = h('form', { class: 'w-full max-w-sm flex flex-col gap-3 py-10' },
    h('h1', { class: 'welcome-title', text: t('Welcome') }),
    h('p', { class: 'text-center text-muted -mt-1', text: t('Sign in to {0} to read your feeds', document.title || 'Ebi Furai') }),
    art,
    h('div', { class: 'field-wrap' }, icon('o-user-circle'), user),
    h('div', { class: 'field-wrap' }, icon('o-key'), pass),
    error, submit);

  // Second step, with two-step verification on: the code of the app (or a
  // recovery code). The ticket from the first step lasts five minutes.
  let ticket = null;
  const code = h('input', {
    class: 'field', name: 'code', type: 'text', inputmode: 'numeric', autocomplete: 'one-time-code',
    autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false', placeholder: t('6-digit code'), maxlength: '11',
  });
  const codeWrap = h('div', { class: 'field-wrap hidden' }, icon('o-shield-check'), code);
  const codeHint = h('p', { class: 'text-center text-muted text-[0.875rem] hidden',
    text: t('Open your authenticator app (or Passwords on iPhone). If you lost your phone, use one of the recovery codes.') });
  const back = h('button', { type: 'button', class: 'btn btn-plain w-full hidden', text: t('Back') });
  form.insertBefore(codeWrap, error);
  form.insertBefore(codeHint, error);
  form.append(back);

  const stepTwo = (on) => {
    ticket = on ? ticket : null;
    [user, pass].forEach((el) => el.closest('.field-wrap').classList.toggle('hidden', on));
    [codeWrap, codeHint, back].forEach((el) => el.classList.toggle('hidden', !on));
    submit.textContent = on ? t('Verify') : t('Sign in');
    if (on) {
      code.value = '';
      motion.swapIn([codeWrap, codeHint], 1);
      setTimeout(() => code.focus(), 50);
    } else {
      pass.value = '';
      pass.focus();
    }
  };
  back.addEventListener('click', () => { error.textContent = ''; stepTwo(false); });

  const finish = async (me) => {
    // Check that the browser really stored the session cookie.
    try {
      await get('/api/me');
    } catch (e2) {
      if (e2.status === 401) {
        error.textContent = location.protocol === 'http:'
          ? t('The browser didn\'t save the sign-in cookie: over http set COOKIE_SECURE=auto (or false) in the .env file.')
          : t('The browser didn\'t save the sign-in cookie: check that cookies are not blocked.');
        return;
      }
      throw e2;
    }
    pass.value = '';
    code.value = '';
    await onSuccess(me);
  };

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    error.textContent = '';
    submit.disabled = true;
    try {
      if (ticket) {
        const me = await post('/api/login/totp', { ticket, code: code.value });
        await finish(me);
        return;
      }
      const r = await post('/api/login', { username: user.value.trim(), password: pass.value });
      if (r.totp_required) {
        ticket = r.ticket;
        stepTwo(true);
        return;
      }
      await finish(r);
    } catch (err) {
      error.textContent = err.message;
      if (err.data && err.data.restart) stepTwo(false);
      else if (ticket) { motion.shake(codeWrap); code.select(); }
    } finally {
      submit.disabled = false;
    }
  });
  root.append(form);
  motion.stagger(form.children, { y: 18 });
  motion.float(art.children);

  // First start: no administrator yet, the setup form takes the place of
  // the login.
  get('/api/setup').then((r) => { if (r.needed) showSetup(root, onSuccess); }).catch(() => {});
}

function showSetup(root, onSuccess) {
  clear(root);
  const field = (ico, attrs) => h('div', { class: 'field-wrap' }, icon(ico), h('input', { class: 'field', required: true, ...attrs }));
  const code = field('o-key', {
    name: 'code', type: 'text', autocomplete: 'off', autocapitalize: 'characters', autocorrect: 'off', spellcheck: 'false',
    placeholder: t('Setup code'), maxlength: '12',
  });
  const user = field('o-user-circle', {
    name: 'username', type: 'text', autocomplete: 'username', autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false',
    placeholder: t('Administrator username'), value: 'admin',
  });
  const pass = field('o-lock-closed', { name: 'password', type: 'password', autocomplete: 'new-password', placeholder: t('Password (min. 8 characters)') });
  const pass2 = field('o-lock-closed', { name: 'password2', type: 'password', autocomplete: 'new-password', placeholder: t('Repeat the password') });
  const error = h('div', { class: 'text-danger text-sm text-center min-h-5 px-2' });
  const submit = h('button', { type: 'submit', class: 'btn btn-primary w-full mt-1', text: t('Create administrator') });
  const value = (wrap) => wrap.querySelector('input').value;
  const form = h('form', { class: 'w-full max-w-sm flex flex-col gap-3 py-10' },
    h('h1', { class: 'welcome-title', text: t('Welcome') }),
    h('p', { class: 'text-center text-muted -mt-1', text: t('Create the administrator account. The setup code is in the server log: docker compose logs ebi-furai') }),
    code, user, pass, pass2, error, submit);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    error.textContent = '';
    if (value(pass) !== value(pass2)) { error.textContent = t('The two passwords don\'t match'); motion.shake(pass2); return; }
    submit.disabled = true;
    try {
      const me = await post('/api/setup', { code: value(code), username: value(user).trim(), password: value(pass) });
      await onSuccess(me);
    } catch (err) {
      error.textContent = err.message;
      if (err.status === 409) setTimeout(() => showLogin(onSuccess), 1500);
    } finally {
      submit.disabled = false;
    }
  });
  root.append(form);
  motion.stagger(form.children, { y: 18 });
  setTimeout(() => code.querySelector('input').focus(), 300);
}

export function hideLogin() {
  const root = document.getElementById('login');
  clear(root);
  root.className = 'hidden';
}
