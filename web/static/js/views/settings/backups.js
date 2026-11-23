// Backups (administrators): list, copies on WebDAV, restore.

import { locale, t } from '../../i18n.js';
import { api, del, get, post, put } from '../../api.js';
import { confirmDialog, formDialog, h, icon, showError, tile, toast } from '../../ui.js';
import * as offline from '../../offline/index.js';
import * as motion from '../../motion.js';

const sizeFmt = (n) => (n >= 1048576 ? `${(n / 1048576).toLocaleString(locale, { maximumFractionDigits: 1 })} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);
const backupDay = new Intl.DateTimeFormat(locale, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });

export function backupControls() {
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
export async function restoreBackup({ name, when, file }) {
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
