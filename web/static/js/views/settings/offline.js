// Offline reading: what is kept on this device.

import { locale, t } from '../../i18n.js';
import { getPrefs, setPref } from '../../prefs.js';
import { confirmDialog, h, icon, picker, toast, toggleSwitch } from '../../ui.js';
import * as offline from '../../offline/index.js';

function formatSize(bytes) {
  if (!bytes) return '';
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} kB`;
  return `${(bytes / 1024 / 1024).toFixed(1).replace('.', ',')} MB`;
}

const timeFmt = new Intl.DateTimeFormat(locale, { weekday: 'short', hour: '2-digit', minute: '2-digit' });

export function offlineControls() {
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
