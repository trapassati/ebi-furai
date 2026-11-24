// "Impostazioni del feed": name, full text, open on the site and the
// filters of one feed, and whether its site sends instant updates. The switches save at once; the name when the sheet
// closes.

import { t } from '../i18n.js';
import { patch } from '../api.js';
import { feedIcon, h, icon, openSheet, showError, tile, toast, toggleSwitch } from '../ui.js';
import { sourcesChanged } from './sources.js';
import * as motion from '../motion.js';
import { openFilters, filtersFor } from './filters.js';

function switchRow(label, hint, sw) {
  return h('div', { class: 'switch-row' },
    h('div', {}, h('div', { class: 'text-[1rem]', text: label }), h('div', { class: 'text-[0.8125rem] text-muted', text: hint })),
    sw);
}

export function openFeedSettings(feed) {
  const ref = {};
  let changed = false;

  const name = h('input', {
    class: 'field', type: 'text', value: feed.custom_title || '', placeholder: feed.original_title || feed.url,
    autocomplete: 'off', autocapitalize: 'sentences', maxlength: '200', enterkeyhint: 'done',
  });
  name.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); name.blur(); } });

  const option = (key, value) => {
    const sw = toggleSwitch(!!value);
    sw.addEventListener('change', async () => {
      try {
        await patch(`/api/feeds/${feed.id}`, { [key]: sw.checked });
        feed[key] = sw.checked;
        changed = true;
        if (key === 'full_text' && sw.checked) toast(t('The full text will arrive by itself for new articles and for unread ones'));
      } catch (e) { sw.setChecked(!sw.checked); showError(e); }
    });
    return sw;
  };

  const filterLabel = h('span', { class: 'flex-1' });
  const paintFilters = async () => {
    const n = (await filtersFor(feed)).length;
    filterLabel.textContent = n ? t('Keyword filters: {0}', n) : t('Keyword filters');
  };
  paintFilters();

  // Instant updates (WebSub): the site's hub tells the server as soon as it
  // publishes. Shown only where it is on; nothing to set.
  const bolt = feed.instant ? tile('o-bolt', 'sunset', 'sm') : null;
  const instant = feed.instant ? [
    h('div', { class: 'group-label', text: t('Updates') }),
    h('div', { class: 'group flex items-center gap-3 px-3 py-2.5', 'data-instant': '' },
      bolt,
      h('div', { class: 'min-w-0' },
        h('div', { class: 'text-[1rem]', text: t('Instant updates') }),
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('The site tells the server as soon as it publishes: new articles arrive within seconds.') }))),
  ] : [];

  const body = h('div', { class: 'min-h-full' },
    h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
      h('div', { class: 'w-20' }),
      h('div', { class: 'flex-1 text-center sheet-title', text: t('Feed settings') }),
      h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') })),
    h('div', { class: 'px-4 pb-10' },
      h('div', { class: 'flex items-center gap-3 px-1 pt-2' },
        feedIcon(feed.id, feed.has_icon, feed.title, 'big'),
        h('div', { class: 'min-w-0' },
          h('div', { class: 'text-[1.0625rem] font-semibold truncate', text: feed.original_title || feed.title }),
          h('div', { class: 'text-[0.8125rem] text-muted truncate', text: feed.url }))),
      h('div', { class: 'group-label', text: t('Name') }),
      name,
      h('div', { class: 'text-[0.8125rem] text-muted px-1 pt-2', text: t('A short name to show in the list. Leave empty to use the feed\'s title.') }),
      h('div', { class: 'group-label', text: t('Reading') }),
      h('div', { class: 'group px-4' },
        switchRow(t('Always download the full text'),
          t('For feeds that only publish the beginning: the server downloads the article from the site as soon as it arrives, so it\'s ready offline too.'),
          option('full_text', feed.full_text)),
        switchRow(t('Always open on the site'),
          t('Tapping an article opens the site\'s page instead of the reader. Useful for sites that don\'t read well here.'),
          option('open_web', feed.open_web))),
      ...instant,
      h('div', { class: 'group-label', text: t('Filters') }),
      h('div', { class: 'group' },
        h('button', { class: 'row-btn px-3 py-2 text-[1.0625rem]', onclick: () => openFilters({ feed, onChange: paintFilters }) },
          tile('o-funnel', 'indigo', 'sm'), filterLabel, icon('o-chevron-right', 'w-4 h-4 text-faint'))),
      h('div', { class: 'text-[0.8125rem] text-muted px-1 pt-2', text: t('Hide or mark as read the articles about something you don\'t care about.') })));

  if (bolt) setTimeout(() => motion.pop(bolt), 250);

  ref.sheet = openSheet(body, {
    full: true, wide: true,
    onClose: async () => {
      const v = name.value.trim();
      if (v !== (feed.custom_title || '')) {
        try { await patch(`/api/feeds/${feed.id}`, { title: v }); changed = true; } catch (e) { showError(e); }
      }
      if (changed) await sourcesChanged();
    },
  });
}
