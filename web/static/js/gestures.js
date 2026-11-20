// Touch gestures written by hand: pull-to-refresh, swipe on list rows and
// swipe from the left edge to go back.

import { h, icon } from './ui.js';
import * as motion from './motion.js';

// Pull-to-refresh on a scroll container. onRefresh must return a promise.
export function pullToRefresh(scroller, onRefresh) {
  const spinner = h('div', { class: 'ptr-spinner' }, icon('o-arrow-path'));
  const holder = h('div', { class: 'ptr' }, spinner);
  scroller.parentElement.insertBefore(holder, scroller);
  holder.style.top = (scroller.offsetTop + (parseFloat(getComputedStyle(scroller).paddingTop) || 0)) + 'px';

  const THRESHOLD = 70;
  let startY = 0;
  let pulling = false;
  let tracking = false;
  let dist = 0;
  let busy = false;

  const render = (d, animate) => {
    spinner.style.transition = animate ? 'transform 200ms, opacity 200ms' : 'none';
    spinner.style.transform = `translateY(${Math.min(d, 90) - 40}px) rotate(${d * 3}deg)`;
    spinner.style.opacity = String(Math.min(d / THRESHOLD, 1));
  };

  scroller.addEventListener('touchstart', (e) => {
    if (busy || e.touches.length !== 1) return;
    holder.style.top = (scroller.offsetTop + (parseFloat(getComputedStyle(scroller).paddingTop) || 0)) + 'px';
    tracking = scroller.scrollTop <= 0;
    pulling = false;
    startY = e.touches[0].clientY;
    dist = 0;
  }, { passive: true });

  scroller.addEventListener('touchmove', (e) => {
    if (!tracking || busy) return;
    const dy = e.touches[0].clientY - startY;
    if (!pulling) {
      if (dy > 6 && scroller.scrollTop <= 0) pulling = true;
      else if (dy < 0) { tracking = false; return; }
      else return;
    }
    if (e.cancelable) e.preventDefault();
    dist = Math.max(0, dy * 0.5);
    render(dist, false);
  }, { passive: false });

  const end = async () => {
    if (!tracking) return;
    tracking = false;
    if (!pulling) return;
    pulling = false;
    if (dist >= THRESHOLD) {
      busy = true;
      render(THRESHOLD, true);
      spinner.classList.add('loading');
      try { await onRefresh(); } catch (e) { /* caller reports errors */ }
      spinner.classList.remove('loading');
      busy = false;
    }
    render(0, true);
  };
  scroller.addEventListener('touchend', end);
  scroller.addEventListener('touchcancel', end);
}

// Horizontal swipe on article rows (event delegation on the list).
// Swipe right -> onRight(row) (star), swipe left -> onLeft(row) (read).
export function rowSwipe(list, { onLeft, onRight }) {
  const TRIGGER = 72;
  let row = null, main = null, bg = null;
  let x0 = 0, y0 = 0, dx = 0;
  let mode = ''; // '', 'h', 'v'

  list.addEventListener('touchstart', (e) => {
    const r = e.target.closest('.arow');
    if (!r || e.touches.length !== 1) { row = null; return; }
    row = r;
    main = r.querySelector('.arow-main');
    bg = r.querySelector('.arow-bg');
    x0 = e.touches[0].clientX;
    y0 = e.touches[0].clientY;
    dx = 0;
    mode = '';
    main.classList.remove('animate');
  }, { passive: true });

  list.addEventListener('touchmove', (e) => {
    if (!row) return;
    const t = e.touches[0];
    const mx = t.clientX - x0;
    const my = t.clientY - y0;
    if (!mode) {
      if (Math.abs(mx) < 8 && Math.abs(my) < 8) return;
      mode = Math.abs(mx) > Math.abs(my) * 1.2 ? 'h' : 'v';
      if (mode === 'h' && x0 < 24) mode = 'v'; // leave the left edge to the back gesture
    }
    if (mode !== 'h') return;
    if (e.cancelable) e.preventDefault();
    dx = mx;
    const limit = 130;
    const shown = Math.abs(dx) > limit ? Math.sign(dx) * (limit + (Math.abs(dx) - limit) * 0.25) : dx;
    main.style.transform = `translateX(${shown}px)`;
    bg.classList.toggle('show-left', dx > 0);
    bg.classList.toggle('show-right', dx < 0);
    bg.style.opacity = String(Math.min(Math.abs(dx) / TRIGGER, 1));
  }, { passive: false });

  const end = () => {
    if (!row) return;
    const r = row;
    const m = main;
    const b = bg;
    row = null;
    if (mode !== 'h') return;
    r.dataset.swiped = '1';
    setTimeout(() => { delete r.dataset.swiped; }, 350);
    m.classList.add('animate');
    m.style.transform = '';
    setTimeout(() => { b.classList.remove('show-left', 'show-right'); b.style.opacity = ''; }, 200);
    if (dx >= TRIGGER) onRight(r);
    else if (dx <= -TRIGGER) onLeft(r);
  };
  list.addEventListener('touchend', end);
  list.addEventListener('touchcancel', end);
}

// Swipe from the left edge to go back (single-column layout). The page
// follows the finger and the previous one shows underneath; on release it
// completes if dragged past a third of the width or flicked.
export function edgeBack(pane, enabled, getUnder, onBack) {
  let x0 = 0, y0 = 0, active = false, started = false, dx = 0, under = null;
  let lastX = 0, lastT = 0, vel = 0;
  pane.addEventListener('touchstart', (e) => {
    active = enabled() && e.touches.length === 1 && e.touches[0].clientX < 28;
    started = false;
    if (!active) return;
    x0 = lastX = e.touches[0].clientX;
    y0 = e.touches[0].clientY;
    lastT = e.timeStamp;
    dx = 0; vel = 0;
  }, { passive: true });
  pane.addEventListener('touchmove', (e) => {
    if (!active) return;
    const t = e.touches[0];
    dx = t.clientX - x0;
    if (!started) {
      if (Math.abs(t.clientY - y0) > 12 && Math.abs(t.clientY - y0) > dx) { active = false; return; }
      if (dx < 8) return;
      started = true;
      under = getUnder();
      motion.dragStart(pane, under);
    }
    if (e.cancelable) e.preventDefault();
    const dt = e.timeStamp - lastT;
    if (dt > 0) vel = 0.8 * ((t.clientX - lastX) / dt) + 0.2 * vel;
    lastX = t.clientX; lastT = e.timeStamp;
    motion.dragTo(pane, under, dx / window.innerWidth);
  }, { passive: false });
  const end = async () => {
    if (!active) return;
    active = false;
    if (!started) return;
    started = false;
    const p = Math.max(0, dx) / window.innerWidth;
    const commit = p > 0.33 || (vel > 0.45 && dx > 30);
    if (commit) onBack();
    await motion.dragEnd(pane, under, p, commit);
  };
  pane.addEventListener('touchend', end);
  pane.addEventListener('touchcancel', () => { if (started) { dx = 0; vel = 0; } end(); });
}

// Long press (for context menus on touch).
export function longPress(el, cb, ms = 500) {
  let timer = null, x = 0, y = 0, fired = false;
  el.addEventListener('touchstart', (e) => {
    x = e.touches[0].clientX; y = e.touches[0].clientY;
    fired = false;
    timer = setTimeout(() => {
      timer = null;
      fired = true;
      el.dataset.longpress = '1';
      setTimeout(() => { delete el.dataset.longpress; }, 600);
      cb(e);
    }, ms);
  }, { passive: true });
  el.addEventListener('touchmove', (e) => {
    if (timer && (Math.abs(e.touches[0].clientX - x) > 8 || Math.abs(e.touches[0].clientY - y) > 8)) {
      clearTimeout(timer); timer = null;
    }
  }, { passive: true });
  const cancel = () => { if (timer) { clearTimeout(timer); timer = null; } };
  // After a long press, the finger lifts on the menu that just opened:
  // cancel the synthetic click so it does not pick the first item.
  el.addEventListener('touchend', (e) => {
    cancel();
    if (fired) { fired = false; if (e.cancelable) e.preventDefault(); }
  });
  el.addEventListener('touchcancel', cancel);
  el.addEventListener('contextmenu', (e) => { e.preventDefault(); if (!el.dataset.longpress) cb(e); });
}
