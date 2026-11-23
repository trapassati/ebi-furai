// Appearance: per device; the short version is the "Aa" window of the reader.

import { langPref, setLang, t } from '../../i18n.js';
import { FONTS, LINE_HEIGHTS, LIST_SIZES, getPrefs, setPref } from '../../prefs.js';
import { h, icon, openSheet, toggleSwitch } from '../../ui.js';
import * as sounds from '../../sounds.js';
import { segmented } from './parts.js';

// reading: the short version for the "Aa" button in the reader, only what
// changes the article being read.
export function appearanceControls({ reading = false } = {}) {
  const p = getPrefs();
  const sizeLabel = h('span', { class: 'w-16 text-center tabular-nums text-[0.9375rem] font-medium', text: p.size + ' pt' });
  const changeSize = (d) => {
    const v = Math.min(28, Math.max(13, (Number(getPrefs().size) || 18) + d));
    setPref('size', v);
    sizeLabel.textContent = v + ' pt';
  };
  const fonts = h('div', { class: 'group' });
  const paintFonts = () => fonts.querySelectorAll('button').forEach((b) => {
    const on = b.dataset.v === getPrefs().font;
    b.querySelector('.check').classList.toggle('invisible', !on);
  });
  Object.entries(FONTS).forEach(([key, f]) => {
    const label = h('span', { class: 'flex-1', text: f.label });
    label.style.fontFamily = f.stack;
    fonts.append(h('button', {
      class: 'row-btn px-4 py-2.5 text-[1.0625rem]', dataset: { v: key },
      onclick: () => { setPref('font', key); paintFonts(); },
    }, label, icon('o-check', 'check w-5 h-5 text-accent')));
  });
  paintFonts();

  const book = toggleSwitch(p.book !== false);
  book.addEventListener('change', () => setPref('book', book.checked));
  // Side-by-side pages exist only on touch screens (iPhone Duo).
  const touch = window.matchMedia('(pointer: coarse)').matches;
  // The interface size is for screens with a mouse; it stays on this device.
  const desktop = window.matchMedia('(hover: hover) and (pointer: fine)').matches;
  let uiNow = 'auto';
  try { uiNow = localStorage.getItem('uiScale') || 'auto'; } catch { /* ignore */ }
  const uiSize = desktop ? h('div', { class: 'flex flex-col gap-1.5' },
    h('div', { class: 'text-[0.8125rem] text-muted px-1', text: t('Interface size on this computer') }),
    segmented([['auto', t('Automatic|f')], ['1', '100%'], ['1.15', '115%'], ['1.3', '130%']], uiNow, (v) => {
      try { if (v === 'auto') localStorage.removeItem('uiScale'); else localStorage.setItem('uiScale', v); } catch { /* ignore */ }
      if (window.__applyUiScale) window.__applyUiScale();
      window.dispatchEvent(new Event('resize'));
    })) : null;

  // Line spacing of the article and text size of the article list. The
  // nearest choice is shown as selected, whatever the stored value.
  const near = (list, v, def) => String(list.reduce((a, [x]) => (Math.abs(x - v) < Math.abs(a - v) ? x : a), def));
  const labelled = (label, control) => h('div', { class: 'flex flex-col gap-1.5' },
    h('div', { class: 'text-[0.8125rem] text-muted px-1', text: label }), control);
  const lineHeight = labelled(t('Article line spacing'),
    segmented(LINE_HEIGHTS.map(([v, l]) => [String(v), l]), near(LINE_HEIGHTS, Number(p.line_height) || 1.65, 1.65),
      (v) => setPref('line_height', Number(v))));
  const listSize = labelled(t('Article list text'),
    segmented(LIST_SIZES.map(([v, l]) => [String(v), l]), near(LIST_SIZES, Number(p.list_size) || 1, 1),
      (v) => setPref('list_size', Number(v))));

  // Language: the page reloads to apply it everywhere.
  const language = labelled(t('Language'),
    segmented([['auto', t('Automatic|f')], ['it', 'Italiano'], ['en', 'English']], langPref(), (v) => { setLang(v); location.reload(); }));

  // Sound effects, with their volume (shown only when they are on).
  const soundSw = toggleSwitch(p.sounds === true);
  const volNow = String(sounds.VOLUMES.reduce((a, [x]) => (Math.abs(x - p.sound_volume) < Math.abs(a - p.sound_volume) ? x : a), 60));
  const soundVol = h('div', { class: 'sound-volume flex flex-col gap-1.5 pb-3' },
    h('div', { class: 'text-[0.8125rem] text-muted', text: t('Volume') }),
    segmented(sounds.VOLUMES.map(([v, l]) => [String(v), l]), volNow, (v) => {
      setPref('sound_volume', Number(v));
      sounds.play('star');
    }));
  soundVol.classList.toggle('hidden', p.sounds !== true);
  soundSw.addEventListener('change', () => {
    setPref('sounds', soundSw.checked);
    soundVol.classList.toggle('hidden', !soundSw.checked);
    if (soundSw.checked) sounds.play('refresh');
  });
  const soundBox = h('div', { class: 'group px-4' },
    h('div', { class: 'switch-row' },
      h('div', {}, h('div', { class: 'text-[1rem]', text: t('Sound effects') }),
        h('div', { class: 'text-[0.8125rem] text-muted', text: t('Soft sounds for new articles, stars and articles marked as read. On the iPhone the silent switch mutes them.') })),
      soundSw),
    soundVol);

  const theme = segmented([['auto', t('Automatic')], ['light', t('Light')], ['dark', t('Dark')]], p.theme, (v) => setPref('theme', v));
  const size = h('div', { class: 'size-box bg-raise rounded-[calc(20px*var(--ui))] shadow-[var(--shadow-card)] flex items-center justify-between px-2 py-2' },
      h('button', { class: 'btn btn-plain !min-h-11 !px-5 text-[0.9375rem]', 'aria-label': t('Smaller text'), onclick: () => changeSize(-1), text: 'A−' }),
      sizeLabel,
      h('button', { class: 'btn btn-plain !min-h-11 !px-5 text-[1.25rem]', 'aria-label': t('Larger text'), onclick: () => changeSize(1), text: 'A+' }));

  if (reading) return h('div', { class: 'flex flex-col gap-3' }, theme, fonts, size, lineHeight);

  return h('div', { class: 'flex flex-col gap-3' },
    h('div', { class: 'text-[0.8125rem] text-muted px-1', text: t('They only apply to this device.') }),
    language,
    theme,
    fonts,
    size,
    lineHeight,
    listSize,
    soundBox,
    touch ? h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' },
        h('div', {}, h('div', { class: 'text-[1rem]', text: t('Side-by-side pages') }),
          h('div', { class: 'text-[0.8125rem] text-muted', text: t('On an open iPhone Duo, full width: two columns like a book') })),
        book)) : null,
    uiSize);
}

// The "Aa" window of the reader: only what changes the article, with a
// Done button always in reach. The rest is in Settings → Appearance.
export function openAppearance() {
  const ref = {};
  const body = h('div', { class: 'px-4 pt-1 pb-3 flex flex-col gap-3 reading-appearance' },
    h('div', { class: 'flex items-center' },
      h('div', { class: 'w-20' }),
      h('div', { class: 'flex-1 sheet-title', text: t('Appearance') }),
      h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem] justify-end', type: 'button', onclick: () => ref.sheet.close(), text: t('Done') })),
    appearanceControls({ reading: true }));
  ref.sheet = openSheet(body);
}
