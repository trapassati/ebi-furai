import { locale, t } from './i18n.js';
import { isNetworkError } from './api.js';
import * as motion from './motion.js';
import * as sounds from './sounds.js';
import { isDesk, popoverMenu } from './desk.js';

// DOM helpers, icons, toasts, sheets and dialogs.

const SVG_NS = 'http://www.w3.org/2000/svg';

// h('div', {class: 'x', onclick: fn}, child, 'text', ...)
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === undefined || v === null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k === 'text') el.textContent = v;
      else if (k === 'dataset') Object.assign(el.dataset, v);
      else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
      else if (v === true) el.setAttribute(k, '');
      else el.setAttribute(k, v);
    }
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const c of children) {
    if (c === undefined || c === null || c === false) continue;
    if (Array.isArray(c)) append(el, c);
    else el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
}

// icon('o-star', 'w-5 h-5') -> <svg><use href="#o-star"/></svg>
export function icon(name, cls = '') {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('aria-hidden', 'true');
  if (cls) svg.setAttribute('class', cls);
  const use = document.createElementNS(SVG_NS, 'use');
  use.setAttribute('href', '#' + name);
  svg.appendChild(use);
  return svg;
}

export async function loadSprite() {
  try {
    // Same version as the page scripts, so an old copy is never reused.
    const app = document.querySelector('script[src*="/static/js/app.js"]');
    const v = app ? new URL(app.src).searchParams.get('v') || '' : '';
    const res = await fetch('/static/icons/sprite.svg?v=' + encodeURIComponent(v), { cache: 'no-cache' });
    const text = await res.text();
    const holder = document.createElement('div');
    holder.style.position = 'absolute';
    holder.style.width = '0';
    holder.style.height = '0';
    holder.style.overflow = 'hidden';
    holder.innerHTML = text; // static, same-origin asset
    document.body.prepend(holder);
  } catch (e) { /* icons are decorative */ }
}

export function iconButton(name, label, onclick, extra = {}) {
  return h('button', { class: 'icon-btn ' + (extra.class || ''), 'aria-label': label, onclick, ...extra.attrs },
    icon(name), extra.text ? h('span', { class: 'text-[0.9375rem]', text: extra.text }) : null);
}

// The large page title collapses into barTitle when scrolled past (iOS
// large titles). getHeight returns the scroll offset where it switches.
export function collapsingTitle(scroller, barTitle, getOffset) {
  let on = false;
  const check = () => {
    const next = scroller.scrollTop > getOffset();
    if (next !== on) { on = next; barTitle.classList.toggle('on', on); }
  };
  scroller.addEventListener('scroll', check, { passive: true });
  return check;
}

// keepPlace keeps the reading position when the scroller changes width
// (iPhone Duo unfolding or folding, rotation, Split View): the first
// visible item matching selector stays at the same height on screen.
// centerEmpty places an empty-state block (picture, title, hint) in the
// middle of its pane. What is centred is the picture and the title, so the
// titles of two panes side by side ("Hai letto tutto", "Nessun articolo
// aperto") sit on the same line whatever the length of the hints. It stays
// clear of the title above and of a bottom bar (the side bars of the iPhone
// Duo do not count). Call it again when the size changes.
export function centerEmpty(scroller, el) {
  if (!el.isConnected || el.classList.contains('hidden') || scroller.clientHeight === 0) return;
  el.style.paddingTop = '0px';
  const box = scroller.getBoundingClientRect();
  const r = el.getBoundingClientRect();
  const title = el.querySelector('.empty-title');
  const core = title ? title.getBoundingClientRect().bottom - r.top : r.height;
  // A bottom bar only: on the desktop the commands are in the top bar.
  const d = [...scroller.parentElement.querySelectorAll('.dock')].filter((x) => !x.closest('.bar')).map((x) => x.getBoundingClientRect())
    .filter((x) => x.width > x.height && x.height > 0);
  const bottom = (d.length ? Math.min(...d.map((x) => x.top)) : box.bottom) - 16;
  let top = box.top + (box.height - core) / 2;
  if (top + r.height > bottom) top = bottom - r.height;
  if (top < r.top + 16) top = r.top + Math.max(16, (bottom - r.top - r.height) / 2);
  el.style.paddingTop = `${Math.round(top - r.top)}px`;
}

// paperArt is the original illustration of the login page: three article
// cards, which float (motion.float). Also the picture of "You've read
// everything". (Classes only: the CSP forbids inline style attributes.)
export function paperArt(extra = '') {
  const paper = (cls, dot) => h('div', { class: 'paper ' + cls },
    h('div', { class: 'flex items-center gap-2' }, h('span', { class: 'dot ' + dot }), h('i', { class: 't' })),
    h('i'), h('i', { class: 'w-4/5' }), h('i', { class: 'w-[55%]' }));
  return h('div', { class: ('welcome-art ' + extra).trim(), 'aria-hidden': 'true' },
    paper('p1', 'bg-[var(--k-purple)]'), paper('p2', 'bg-[var(--k-orange)]'), paper('p3', 'bg-[var(--k-blue)]'));
}

// emptyBlock builds an empty state: the picture sits in a box of fixed
// height, so every empty state has its title at the same place.
export function emptyBlock(picture, title, hint) {
  return [h('div', { class: 'empty-figure' }, picture),
    h('p', { class: 'empty-title text-[1.0625rem] font-medium mb-1', text: title }),
    h('p', { class: 'text-muted', text: hint })];
}

export function keepPlace(scroller, selector) {
  let anchor = null;
  let delta = 0;
  let part = false;
  let raf = 0;
  const visibleTop = () => scroller.getBoundingClientRect().top + (parseFloat(getComputedStyle(scroller).paddingTop) || 0);
  let width = scroller.clientWidth;
  const record = () => {
    raf = 0;
    // While hidden, or before a pending resize is handled, the scroll
    // position says nothing: keep the anchor found on screen.
    if (scroller.clientHeight === 0 || scroller.clientWidth !== width) return;
    anchor = null;
    if (scroller.scrollTop <= 0) return;
    const top = visibleTop();
    for (const el of scroller.querySelectorAll(selector)) {
      const r = el.getBoundingClientRect();
      // The point read is kept as a fraction of the element when it starts
      // above the screen (a long paragraph changes height on reflow).
      if (r.bottom > top) { anchor = el; delta = r.top < top && r.height ? (top - r.top) / r.height : r.top - top; part = r.top < top; return; }
    }
  };
  scroller.addEventListener('scroll', () => { if (!raf) raf = requestAnimationFrame(record); }, { passive: true });
  // A new width (rotation, folding, a column appearing) reflows the text;
  // a pane shown again after display:none starts back at the top. Either
  // way the element that was at the top of the screen goes back there.
  new ResizeObserver(() => {
    const w = scroller.clientWidth;
    if (w === width) return;
    width = w;
    if (!w || !anchor || !anchor.isConnected) return;
    const r = anchor.getBoundingClientRect();
    scroller.scrollTop += part ? r.top + delta * r.height - visibleTop() : r.top - visibleTop() - delta;
  }).observe(scroller);
}

export function clear(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
  return el;
}

// ---- feed icon ----

export function feedIcon(feedId, hasIcon, title, size = '') {
  const cls = size === true ? ' small' : (size ? ' ' + size : '');
  const letter = () => h('span', { class: 'feed-letter' + cls, text: (title || '?').trim().charAt(0) || '?' });
  if (!hasIcon) return letter();
  const img = h('img', { class: 'feed-icon' + cls, src: `/api/feeds/${feedId}/icon`, alt: '', loading: 'lazy' });
  img.addEventListener('error', () => img.replaceWith(letter()), { once: true });
  return img;
}

// ---- dates ----

const dayFmt = new Intl.DateTimeFormat(locale, { weekday: 'short' });
const shortFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short' });
const yearFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric' });
const fullFmt = new Intl.DateTimeFormat(locale, { dateStyle: 'full', timeStyle: 'short' });

export function relTime(ts) {
  const d = new Date(ts * 1000);
  const now = new Date();
  const diff = (now - d) / 1000;
  if (diff < 60) return t('now');
  if (diff < 3600) return `${Math.floor(diff / 60)} min`;
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  if (d >= startToday) return `${Math.floor(diff / 3600)} h`;
  const startYesterday = new Date(startToday - 86400000);
  if (d >= startYesterday) return t('yesterday');
  if (diff < 6 * 86400) return dayFmt.format(d);
  if (d.getFullYear() === now.getFullYear()) return shortFmt.format(d);
  return yearFmt.format(d);
}

export function fullDate(ts) { return fullFmt.format(new Date(ts * 1000)); }

// ---- toasts ----

// toast shows a short message; opts.action ({label, onClick}) adds a
// button (for example "Annulla"), opts.duration how long it stays.
export function toast(message, type = '', opts = {}) {
  if (type === 'error' && !opts.silent) sounds.play('error');
  const box = document.getElementById('toasts');
  const el = h('div', { class: 'toast ' + type, role: 'status' }, h('span', { text: message }));
  let gone = false;
  const close = () => {
    if (gone) return;
    gone = true;
    motion.toastOut(el).then(() => el.remove());
  };
  if (opts.action) {
    el.classList.add('has-action');
    el.append(h('button', {
      class: 'toast-action', type: 'button', text: opts.action.label,
      onclick: () => { close(); opts.action.onClick(); },
    }));
  }
  box.append(el);
  motion.toastIn(el);
  setTimeout(close, opts.duration || (type === 'error' ? 4500 : 2600));
}

let lastOfflineToast = 0;

export function showError(e) {
  if (e && e.name === 'AbortError') return;
  if (e && e.status === 401) return; // the login screen is shown instead
  if (isNetworkError(e)) {
    // One gentle notice instead of a red error for every failed call.
    if (Date.now() - lastOfflineToast > 8000) toast(e.message && e.status === 0 && e.message !== t('Connection unavailable') ? e.message : t('You\'re offline: this needs a connection'));
    lastOfflineToast = Date.now();
    return;
  }
  toast((e && e.message) || t('Error'), 'error');
}

// ---- sheets ----

// Open sheets, topmost last: Esc closes the top one (keyboard shortcuts).
const openSheets = [];
export function sheetOpen() { return openSheets.length > 0; }
export function closeTopSheet() {
  const top = openSheets[openSheets.length - 1];
  if (!top || top.modal) return !!top;
  top.close();
  return true;
}

// openSheet(contentEl, {wide, full}) -> {close, el}; resolves onClose.
export function openSheet(content, opts = {}) {
  const overlay = document.getElementById('overlay');
  const backdrop = h('div', { class: 'backdrop' });
  const sheet = h('div', { class: 'sheet' + (opts.wide ? ' wide' : '') + (opts.full ? ' full' : '') + (opts.cls ? ' ' + opts.cls : ''), role: 'dialog', 'aria-modal': 'true' },
    opts.full ? null : h('div', { class: 'sheet-grip' }), content);
  overlay.append(backdrop, sheet);
  // Desktop: a window opened from another one (About from the settings)
  // takes its place and its size, as a page of the same window, and gives
  // it back when closed. The shade behind is not doubled.
  const big = (el) => el.classList.contains('wide') || el.classList.contains('full');
  const below = openSheets.length ? openSheets[openSheets.length - 1] : null;
  const covers = isDesk() && below && below.el && big(sheet) && big(below.el);
  if (covers) {
    const r = below.el.getBoundingClientRect();
    sheet.style.width = `${r.width}px`;
    sheet.style.height = `${r.height}px`;
    sheet.style.maxHeight = 'none';
    backdrop.classList.add('still');
    below.el.classList.add('covered');
  }
  let closed = false;
  let resolveClosed;
  const done = new Promise((r) => { resolveClosed = r; });
  const close = () => {
    if (closed) return;
    closed = true;
    const i = openSheets.indexOf(entry);
    if (i >= 0) openSheets.splice(i, 1);
    sheet.style.pointerEvents = 'none';
    backdrop.style.pointerEvents = 'none';
    if (covers) below.el.classList.remove('covered');
    if (sheet.contains(document.activeElement) && below && below.el.isConnected) below.el.focus({ preventScroll: true });
    else if (sheet.contains(document.activeElement)) document.activeElement.blur();
    motion.sheetOut(backdrop, sheet).then(() => { backdrop.remove(); sheet.remove(); resolveClosed(); });
    if (opts.onClose) opts.onClose();
  };
  const entry = { close, modal: !!opts.modal, el: sheet };
  openSheets.push(entry);
  if (!opts.modal) backdrop.addEventListener('click', close);
  motion.sheetIn(backdrop, sheet);
  // Focus goes to the window itself, which draws no ring: left alone, some
  // browsers put it on the first button ("Done", "Back") and outline it
  // until a click elsewhere. Tab still starts from the top of the window.
  sheet.tabIndex = -1;
  const blurOpener = document.activeElement;
  if (blurOpener && blurOpener !== document.body && !sheet.contains(blurOpener) && blurOpener.blur) blurOpener.blur();
  sheet.focus({ preventScroll: true });
  return { close, el: sheet, done };
}

// tile('o-star', 'sunset', 'sm') -> colored circle with a white icon
export function tile(name, tone = 'sky', size = '') {
  return h('span', { class: `tile tile-${tone} ${size}` }, icon(name));
}

// actionSheet({title, message, actions: [{label, icon, tone, destructive, checked, disabled, onSelect}]})
export function actionSheet({ title, message, actions }) {
  if (isDesk()) return popoverMenu({ title, message, actions });
  return new Promise((resolve) => {
    let chosen = null;
    const list = h('div', { class: 'group' },
      actions.map((a) => h('button', {
        class: 'row-btn px-3 py-2 text-[1.0625rem] ' + (a.destructive ? 'text-danger' : 'text-fg') + (a.disabled ? ' opacity-40' : ''),
        disabled: a.disabled,
        onclick: () => { chosen = a; s.close(); },
      },
      a.icon ? tile(a.icon, a.destructive ? 'rose' : (a.tone || 'plain'), 'sm') : null,
      h('span', { class: 'flex-1 truncate', text: a.label }),
      a.checked ? icon('o-check', 'w-5 h-5 text-accent flex-none') : null)));
    const body = h('div', { class: 'px-4 pb-2' },
      title ? h('div', { class: 'sheet-title pt-1 px-2 truncate', text: title }) : null,
      message ? h('div', { class: 'px-2 pt-1 text-center text-[0.8125rem] text-muted whitespace-pre-line break-words', text: message }) : null,
      h('div', { class: 'pt-4' }, list),
      h('button', { class: 'btn btn-plain w-full mt-3', onclick: () => s.close(), text: t('Cancel') }));
    const s = openSheet(body);
    s.done.then(() => {
      resolve(chosen);
      if (chosen && chosen.onSelect) chosen.onSelect();
    });
  });
}

// picker({options: [{value, label}], value, title}) -> custom select that
// opens an action sheet. el.value holds the current value.
export function picker({ options, value, title = t('Choose') }) {
  const label = h('span', { class: 'picker-value' });
  const el = h('button', { type: 'button', class: 'field picker', 'aria-haspopup': 'listbox' }, label, icon('o-chevron-up-down'));
  const set = (v) => {
    const o = options.find((x) => String(x.value) === String(v)) || options[0];
    el.value = o ? o.value : '';
    label.textContent = o ? o.label : '';
  };
  set(value);
  el.addEventListener('click', async () => {
    const choice = await actionSheet({
      title,
      actions: options.map((o) => ({ label: o.label, icon: o.icon, tone: o.tone, checked: String(o.value) === String(el.value), value: o.value })),
    });
    if (choice) {
      set(choice.value);
      el.dispatchEvent(new Event('change'));
    }
  });
  return el;
}

// toggleSwitch(checked) -> button[role=switch]; el.checked holds the state.
export function toggleSwitch(checked = false) {
  const el = h('button', { type: 'button', class: 'switch', role: 'switch' });
  const set = (v) => { el.checked = !!v; el.setAttribute('aria-checked', String(!!v)); };
  set(checked);
  el.setChecked = set; // updates the look too (aria-checked drives the CSS)
  el.addEventListener('click', () => { set(!el.checked); el.dispatchEvent(new Event('change')); });
  return el;
}

// formDialog({title, message, fields: [{name, label, type, value, placeholder, autocomplete, options}], okLabel, destructive})
// type can be text/password/url/..., 'select' (custom picker) or 'switch'.
// Resolves with the values object, or null if cancelled.
export function formDialog({ title, message, fields = [], okLabel = 'OK', destructive = false, validate }) {
  return new Promise((resolve) => {
    let result = null;
    const inputs = {};
    const errorBox = h('div', { class: 'text-danger text-sm px-1 hidden' });
    const form = h('form', { class: 'px-5 pt-1 pb-3 flex flex-col gap-3', novalidate: true });
    if (title) form.append(h('div', { class: 'sheet-title pt-1', text: title }));
    if (message) form.append(h('div', { class: 'text-center text-[0.875rem] text-muted -mt-1 mb-1', text: message }));
    for (const f of fields) {
      let input;
      if (f.type === 'select') {
        input = picker({ options: f.options, value: f.value, title: f.label });
      } else if (f.type === 'switch') {
        input = toggleSwitch(f.value);
        form.append(h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: f.label }), input));
        inputs[f.name] = input;
        continue;
      } else {
        input = h('input', {
          class: 'field', name: f.name, type: f.type || 'text', value: f.value ?? '', placeholder: f.placeholder || '',
          autocomplete: f.autocomplete || 'off', autocapitalize: f.autocapitalize || 'off', autocorrect: 'off', spellcheck: 'false',
          inputmode: f.inputmode,
        });
      }
      inputs[f.name] = input;
      form.append(f.label ? h('label', { class: 'flex flex-col gap-1.5' }, h('span', { class: 'field-label', text: f.label }), input) : input);
    }
    form.append(errorBox);
    const okBtn = h('button', { type: 'submit', class: 'btn flex-1 ' + (destructive ? 'btn-danger' : 'btn-primary'), text: okLabel });
    form.append(h('div', { class: 'flex gap-3 pt-2' },
      h('button', { type: 'button', class: 'btn btn-plain flex-1', text: t('Cancel'), onclick: () => s.close() }), okBtn));
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      const values = {};
      for (const [k, el] of Object.entries(inputs)) values[k] = el.getAttribute('role') === 'switch' ? el.checked : el.value;
      if (validate) {
        okBtn.disabled = true;
        let err;
        try { err = await validate(values); } catch (e) { err = e.message || t('Error'); }
        okBtn.disabled = false;
        if (err) {
          errorBox.textContent = err;
          errorBox.classList.remove('hidden');
          motion.shake(errorBox);
          return;
        }
      }
      result = values;
      s.close();
    });
    const s = openSheet(form);
    s.done.then(() => resolve(result));
    const first = fields.find((f) => f.type !== 'select' && f.type !== 'switch' && f.autofocus !== false);
    if (first && !matchMedia('(pointer: coarse)').matches) setTimeout(() => inputs[first.name].focus(), 250);
  });
}

// confirmDialog: a large icon in a ring, title, message and two full-width
// buttons stacked (the action first, Annulla below). Resolves true/false.
export function confirmDialog({ title, message, okLabel = t('Confirm'), destructive = false, icon: iconName }) {
  return new Promise((resolve) => {
    let ok = false;
    const glyph = iconName || (destructive ? 'o-trash' : 'o-check');
    const ring = h('div', { class: 'confirm-ring' + (destructive ? ' danger' : '') }, icon(glyph));
    const okBtn = h('button', { type: 'button', class: 'btn btn-lg w-full ' + (destructive ? 'btn-danger' : 'btn-primary'), text: okLabel });
    const cancel = h('button', { type: 'button', class: 'btn btn-lg btn-plain w-full', text: t('Cancel') });
    const body = h('div', { class: 'confirm' },
      ring,
      title ? h('div', { class: 'confirm-title', text: title }) : null,
      message ? h('div', { class: 'confirm-msg', text: message }) : null,
      h('div', { class: 'confirm-actions' }, okBtn, cancel));
    const s = openSheet(body);
    okBtn.addEventListener('click', () => { ok = true; s.close(); });
    cancel.addEventListener('click', () => s.close());
    s.done.then(() => resolve(ok));
    motion.confirmIn(ring, body.querySelectorAll('.confirm-title, .confirm-msg, .confirm-actions > *'));
    if (!matchMedia('(pointer: coarse)').matches) setTimeout(() => okBtn.focus(), 200);
  });
}

export async function promptDialog({ title, message, value = '', placeholder = '', okLabel = t('Save'), type = 'text' }) {
  const r = await formDialog({ title, message, okLabel, fields: [{ name: 'v', value, placeholder, type, autocapitalize: 'sentences' }] });
  return r ? r.v : null;
}

// debounce(fn, ms)
export function debounce(fn, ms) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

// Reading time in minutes for a number of words (0 under a minute): a calm
// 230 words a minute, as the server counts them.
export function readingMinutes(words) {
  if (!words || words < 172) return 0;
  return Math.max(1, Math.round(words / 230));
}
