// The ad filter (EasyList), for administrators.

import { locale, t } from '../../i18n.js';
import { get, post, put } from '../../api.js';
import { h, icon, showError, toast, toggleSwitch } from '../../ui.js';

const numFmt = new Intl.NumberFormat(locale);
const dayFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });

export function adControls(isAdmin) {
  const info = h('div', { class: 'text-[0.8125rem] text-muted px-1' });
  const enable = toggleSwitch(true);
  const updateBtn = h('button', { class: 'btn btn-plain w-full' }, icon('o-arrow-path', 'w-5 h-5'), t('Update the lists now'));
  const paint = (st) => {
    enable.setChecked(st.enabled);
    if (!st.enabled) info.textContent = t('Filter off: articles are shown as the site publishes them.');
    else if (!st.rules) info.textContent = t('The EasyList lists haven\'t been downloaded by the server yet.') + (st.error ? t(' Last error: {0}', st.error) : '');
    else {
      let txt = t('On with {0} EasyList and EasyList Italia rules', numFmt.format(st.rules));
      if (st.updated && !st.updated.startsWith('0001')) txt += t(', updated {0}', dayFmt.format(new Date(st.updated)));
      txt += t('. Elements removed since the server started: {0}.', numFmt.format(st.removed));
      info.textContent = txt;
    }
  };
  const load = async () => { try { paint(await get('/api/adfilter')); } catch (e) { info.textContent = ''; } };
  enable.addEventListener('change', async () => {
    try { paint(await put('/api/admin/adfilter', { enabled: enable.checked })); } catch (e) { enable.setChecked(!enable.checked); showError(e); }
  });
  updateBtn.addEventListener('click', async () => {
    updateBtn.disabled = true;
    try { paint(await post('/api/admin/adfilter/update')); toast(t('Lists updated')); } catch (e) { showError(e); }
    updateBtn.disabled = false;
  });
  load();
  const note = h('div', { class: 'text-[0.8125rem] text-muted px-1',
    text: t('Removes banners, sponsored boxes and tracking images from articles and full text. It applies to all users.') });
  if (!isAdmin) return h('div', { class: 'flex flex-col gap-2' }, info, note);
  return h('div', { class: 'flex flex-col gap-3' },
    h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: t('Filter ads') }), enable)),
    info, note, updateBtn);
}

// The row shows whether two-step verification is on, and follows changes.
