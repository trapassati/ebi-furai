// Desktop mode: with a mouse or trackpad and room for two or three
// columns, the app behaves like a desktop app instead of a phone app:
// commands sit in a thin bar at the top of each column, menus open where
// they are clicked, buttons are small and say what they do (and their
// shortcut) when hovered. iPhone and iPad never get it: it needs a pointer
// that hovers and is precise.
//
// <html class="desk"> switches the CSS; a "deskchange" event on document
// lets the views move their controls.

import { h, icon, tile } from './ui.js';
import { t } from './i18n.js';
import * as motion from './motion.js';

const fine = matchMedia('(hover: hover) and (pointer: fine)');
let on = false;

export function isDesk() { return on; }
export function finePointer() { return fine.matches; }

// update is called by the layout with the number of columns.
export function update(cols) {
  const next = fine.matches && cols >= 2;
  if (next === on) return;
  on = next;
  document.documentElement.classList.toggle('desk', on);
  hideTip();
  document.dispatchEvent(new CustomEvent('deskchange', { detail: on }));
}

// ---- controls of a pane: bottom dock on touch, top bar on the desktop ----

// dockInBar moves the dock's controls into the bar (before `before`, or at
// the end) on the desktop, and back under the pane on touch screens.
export function dockInBar(pane, dock, slot) {
  const place = () => {
    if (on) slot.append(dock);
    else pane.append(dock);
  };
  place();
  document.addEventListener('deskchange', place);
}

// ---- where the last click was: menus open there ----

let last = { x: 0, y: 0, el: null, at: 0, context: false };
const remember = (e, context) => { last = { x: e.clientX, y: e.clientY, el: e.target, at: Date.now(), context }; };
document.addEventListener('pointerdown', (e) => remember(e, false), true);
document.addEventListener('contextmenu', (e) => remember(e, true), true);
document.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' || e.key === ' ' || e.key === 'ContextMenu') last = { x: 0, y: 0, el: document.activeElement, at: Date.now(), context: false };
}, true);

function anchorRect() {
  if (Date.now() - last.at > 1500 || !last.el || !last.el.isConnected) return null;
  if (last.context && last.x) return { left: last.x, right: last.x, top: last.y, bottom: last.y, point: true };
  const el = last.el.closest('button, [role="button"], .field, a') || last.el;
  const r = el.getBoundingClientRect();
  if (!r.width) return null;
  return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width };
}

// ---- popover menus ----

const openMenus = [];
export function menuOpen() { return openMenus.length > 0; }

// popoverMenu({title, message, actions}) -> Promise<chosen action or null>.
// Same actions as actionSheet (ui.js), which calls this on the desktop.
export function popoverMenu({ title, message, actions }) {
  return new Promise((resolve) => {
    const anchor = anchorRect();
    const byKeyboard = !anchor || !last.x;
    const items = actions.map((a) => h('button', {
      type: 'button', role: 'menuitem',
      class: 'menu-item' + (a.destructive ? ' danger' : ''),
      disabled: a.disabled,
    },
    a.icon ? tile(a.icon, a.destructive ? 'rose' : (a.tone || 'plain'), 'xs') : null,
    h('span', { class: 'flex-1 truncate', text: a.label }),
    a.checked ? icon('o-check', 'menu-check') : null));
    const menu = h('div', { class: 'menu', role: 'menu', tabindex: '-1' },
      title ? h('div', { class: 'menu-title', text: title }) : null,
      message ? h('div', { class: 'menu-msg', text: message }) : null,
      items);
    const catcher = h('div', { class: 'menu-catcher' });
    document.body.append(catcher, menu);

    // Below the button (or at the pointer), inside the window.
    const mw = menu.offsetWidth;
    const mh = menu.offsetHeight;
    const pad = 8;
    let x, y;
    if (anchor) {
      x = anchor.point ? anchor.left : (anchor.left + mw > innerWidth - pad ? anchor.right - mw : anchor.left);
      y = anchor.bottom + (anchor.point ? 2 : 6);
      if (y + mh > innerHeight - pad) y = Math.max(pad, (anchor.point ? anchor.top : anchor.top - 6) - mh);
    } else {
      x = (innerWidth - mw) / 2;
      y = (innerHeight - mh) / 3;
    }
    x = Math.min(Math.max(pad, x), innerWidth - mw - pad);
    menu.style.left = `${Math.round(x)}px`;
    menu.style.top = `${Math.round(y)}px`;
    const below = !anchor || y > anchor.top;
    menu.style.transformOrigin = `${anchor ? Math.min(Math.max(anchor.left - x, 0), mw) : mw / 2}px ${below ? 0 : mh}px`;

    let done = false;
    const prevFocus = document.activeElement;
    const close = (chosen) => {
      if (done) return;
      done = true;
      const i = openMenus.indexOf(entry);
      if (i >= 0) openMenus.splice(i, 1);
      document.removeEventListener('keydown', onKey, true);
      window.removeEventListener('resize', dismiss);
      window.removeEventListener('blur', dismiss);
      catcher.remove();
      const end = () => menu.remove();
      if (motion.enabled()) window.gsap.to(menu, { opacity: 0, scale: 0.97, duration: 0.12, ease: 'power2.in', onComplete: end });
      else end();
      // Back to where the keyboard was; after a click, nothing gets a ring.
      if (byKeyboard && prevFocus && prevFocus.focus && !chosen) prevFocus.focus({ preventScroll: true });
      else if (menu.contains(document.activeElement)) document.activeElement.blur();
      resolve(chosen);
      if (chosen && chosen.onSelect) chosen.onSelect();
    };
    const dismiss = () => close(null);
    const entry = { close: dismiss };
    openMenus.push(entry);

    items.forEach((b, i) => {
      b.addEventListener('click', () => close(actions[i]));
      b.addEventListener('pointermove', () => { if (document.activeElement !== b && !b.disabled) b.focus({ preventScroll: true }); });
    });
    menu.addEventListener('pointerleave', () => { if (menu.contains(document.activeElement)) menu.focus({ preventScroll: true }); });
    catcher.addEventListener('pointerdown', (e) => { e.preventDefault(); dismiss(); });
    catcher.addEventListener('contextmenu', (e) => { e.preventDefault(); dismiss(); });
    catcher.addEventListener('wheel', dismiss, { passive: true });
    window.addEventListener('resize', dismiss);
    window.addEventListener('blur', dismiss);

    const enabledItems = () => items.filter((b) => !b.disabled);
    const move = (d) => {
      const list = enabledItems();
      if (!list.length) return;
      const i = list.indexOf(document.activeElement);
      list[(i + d + list.length) % list.length].focus({ preventScroll: true });
    };
    const onKey = (e) => {
      e.stopPropagation();
      if (e.key === 'Escape') { e.preventDefault(); dismiss(); } else if (e.key === 'ArrowDown') { e.preventDefault(); move(1); } else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); } else if (e.key === 'Home') { e.preventDefault(); enabledItems()[0]?.focus(); } else if (e.key === 'End') { e.preventDefault(); enabledItems().at(-1)?.focus(); } else if (e.key === 'Tab') { e.preventDefault(); move(e.shiftKey ? -1 : 1); }
    };
    document.addEventListener('keydown', onKey, true);

    menu.focus({ preventScroll: true });
    // Opened from the keyboard: the first item is ready for the arrows.
    if (byKeyboard) enabledItems()[0]?.focus({ preventScroll: true });
    if (motion.enabled()) window.gsap.fromTo(menu, { opacity: 0, scale: 0.96, y: below ? -4 : 4 }, { opacity: 1, scale: 1, y: 0, duration: 0.16, ease: 'power2.out', clearProps: 'transform' });
  });
}

export function closeMenus() {
  if (!openMenus.length) return false;
  openMenus[openMenus.length - 1].close();
  return true;
}

// ---- tooltips: what a button does, and its key ----

let tip = null;
let tipTimer = 0;
let tipFor = null;
let tipHiddenAt = 0;

function hideTip() {
  clearTimeout(tipTimer);
  tipFor = null;
  if (tip && tip.classList.contains('on')) {
    tip.classList.remove('on');
    tipHiddenAt = Date.now();
  }
}

function showTip(el) {
  const label = el.getAttribute('aria-label') || el.dataset.tip;
  if (!label) return;
  if (!tip) {
    tip = h('div', { class: 'tooltip', role: 'tooltip' });
    document.body.append(tip);
  }
  tip.replaceChildren(h('span', { text: label }), el.dataset.key ? h('kbd', { text: el.dataset.key }) : null);
  const r = el.getBoundingClientRect();
  tip.classList.add('on');
  const w = tip.offsetWidth;
  const ht = tip.offsetHeight;
  let x = r.left + r.width / 2 - w / 2;
  x = Math.min(Math.max(6, x), innerWidth - w - 6);
  let y = r.bottom + 6;
  if (y + ht > innerHeight - 6) y = r.top - ht - 6;
  tip.style.left = `${Math.round(x)}px`;
  tip.style.top = `${Math.round(y)}px`;
}

const TIP_TARGETS = '.gbtn, .seg-btn, .icon-btn[aria-label], [data-tip]';
document.addEventListener('pointerover', (e) => {
  if (!on || e.pointerType === 'touch') return;
  const el = e.target.closest(TIP_TARGETS);
  if (el === tipFor) return;
  hideTip();
  if (!el || el.closest('.menu')) return;
  tipFor = el;
  // Moving from one button to the next shows the next tip at once.
  tipTimer = setTimeout(() => showTip(el), Date.now() - tipHiddenAt < 400 ? 60 : 550);
});
document.addEventListener('pointerdown', () => { hideTip(); tipHiddenAt = 0; }, true);
document.addEventListener('wheel', hideTip, { passive: true, capture: true });
document.addEventListener('keydown', hideTip, true);

fine.addEventListener('change', () => document.dispatchEvent(new Event('desk-recheck')));

// ---- columns that can be made wider or narrower ----

const WIDTHS_KEY = 'colWidths';
const LIMITS = { sources: [200, 420], list: [300, 680] };

function loadWidths() {
  try { return JSON.parse(localStorage.getItem(WIDTHS_KEY) || '{}') || {}; } catch (e) { return {}; }
}

function applyWidths(w) {
  const ui = window.__uiScale || 1;
  for (const k of ['sources', 'list']) {
    if (w[k]) document.body.style.setProperty(`--w-${k}`, `${Math.round(w[k] * ui)}px`);
    else document.body.style.removeProperty(`--w-${k}`);
  }
}

// initResizers adds a handle on the right edge of the first two columns:
// drag it to resize (in interface points, so it follows the interface
// size), double click to go back to the standard width.
export function initResizers(panes) {
  const widths = loadWidths();
  applyWidths(widths);
  window.addEventListener('resize', () => applyWidths(widths));
  for (const key of ['sources', 'list']) {
    const pane = panes[key];
    const handle = h('div', { class: 'col-resizer', role: 'separator', 'aria-orientation': 'vertical', 'aria-label': t('Drag to resize the column') });
    pane.append(handle);
    handle.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      handle.setPointerCapture(e.pointerId);
      const ui = window.__uiScale || 1;
      const start = e.clientX;
      const from = pane.getBoundingClientRect().width / ui;
      document.body.classList.add('resizing');
      const move = (ev) => {
        const [lo, hi] = LIMITS[key];
        widths[key] = Math.round(Math.min(hi, Math.max(lo, from + (ev.clientX - start) / ui)));
        applyWidths(widths);
      };
      const up = () => {
        handle.removeEventListener('pointermove', move);
        handle.removeEventListener('pointerup', up);
        handle.removeEventListener('pointercancel', up);
        document.body.classList.remove('resizing');
        try { localStorage.setItem(WIDTHS_KEY, JSON.stringify(widths)); } catch (err) { /* private mode */ }
        window.dispatchEvent(new Event('resize'));
      };
      handle.addEventListener('pointermove', move);
      handle.addEventListener('pointerup', up);
      handle.addEventListener('pointercancel', up);
    });
    handle.addEventListener('dblclick', () => {
      delete widths[key];
      applyWidths(widths);
      try { localStorage.setItem(WIDTHS_KEY, JSON.stringify(widths)); } catch (err) { /* private mode */ }
      window.dispatchEvent(new Event('resize'));
    });
  }
}
