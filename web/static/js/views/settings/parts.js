// Building blocks of the settings: segmented choices, sections and rows.

import { h, icon, tile } from '../../ui.js';

export function segmented(options, current, onPick) {
  const seg = h('div', { class: 'segmented w-full' });
  const paint = (v) => seg.querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.v === v));
  options.forEach(([v, label]) => seg.append(h('button', {
    type: 'button', class: 'flex-1', dataset: { v }, text: label,
    onclick: () => { paint(v); onPick(v); },
  })));
  paint(current);
  return seg;
}

// A section of the settings. On the desktop each one is a page of the
// settings window, listed on its left with its icon.
export function section(label, iconName, tone, ...children) {
  return h('div', { class: 'settings-section', dataset: { label, icon: iconName, tone } },
    h('div', { class: 'group-label', text: label }), ...children);
}

export function rowButton(iconName, tone, label, onclick, cls = '') {
  return h('button', { class: 'row-btn px-3 py-2 text-[1.0625rem] ' + cls, onclick },
    tile(iconName, tone, 'sm'), h('span', { class: 'flex-1', text: label }), icon('o-chevron-right', 'w-4 h-4 text-faint'));
}

// A row of the list of sections: it opens the page of a section.
// detail is a short value shown before the chevron (a count, a state).
export function pageRow(page, onclick) {
  return h('button', { class: 'row-btn px-3 py-2 text-[1.0625rem] settings-row', dataset: { page: page.id }, onclick },
    tile(page.icon, page.tone, 'sm'), h('span', { class: 'flex-1 truncate', text: page.label }),
    page.detail ? h('span', { class: 'text-[0.9375rem] text-muted tabular-nums', text: page.detail }) : null,
    icon('o-chevron-right', 'w-4 h-4 text-faint'));
}
