// Motion: GSAP for animations, Lenis for smooth wheel/trackpad scrolling.
// Both are loaded as classic scripts (window.gsap, window.Lenis) from
// /static/vendor. Touch scrolling stays native: on iOS it is already smooth
// and Lenis would fight with pull-to-refresh and the swipe gestures.

const gsap = window.gsap;
const LenisCtor = window.Lenis;
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)');

export function enabled() { return !!gsap && !reduced.matches; }

if (gsap) gsap.defaults({ ease: 'power3.out', duration: 0.4 });

// ---- smooth scrolling ----

const lenisFor = new WeakMap();

export function smoothScroll(scroller) {
  if (!LenisCtor || !scroller.firstElementChild) return null;
  const lenis = new LenisCtor({
    wrapper: scroller,
    content: scroller.firstElementChild,
    eventsTarget: scroller,
    smoothWheel: true,
    syncTouch: false,
    lerp: 0.12,
    autoRaf: !gsap,
  });
  if (gsap) {
    gsap.ticker.add((time) => lenis.raf(time * 1000));
  }
  lenisFor.set(scroller, lenis);
  return lenis;
}

// scrollTo glides to a position (at once with reduced motion).
export function scrollTo(scroller, y) {
  const lenis = lenisFor.get(scroller);
  const max = scroller.scrollHeight - scroller.clientHeight;
  const to = Math.min(Math.max(y, 0), Math.max(max, 0));
  if (lenis && !reduced.matches) lenis.scrollTo(to, { duration: 0.7 });
  else scroller.scrollTo({ top: to, behavior: reduced.matches ? 'auto' : 'smooth' });
}

// scrollTop sets the position, keeping Lenis in sync.
export function scrollTop(scroller, y = 0) {
  const lenis = lenisFor.get(scroller);
  if (lenis) lenis.scrollTo(y, { immediate: true, force: true });
  scroller.scrollTop = y;
  // Tells the app the intended position, also when the pane is hidden.
  scroller.dispatchEvent(new CustomEvent('scrollset', { detail: y }));
}

if (gsap) gsap.ticker.lagSmoothing(0);

// ---- animations ----

// A pane sliding in (single-column layout).
export function enterPane(el, dir = 1) {
  if (!enabled()) return;
  gsap.fromTo(el, { x: 32 * dir, opacity: 0.4 }, { x: 0, opacity: 1, duration: 0.34, clearProps: 'transform,opacity' });
}

// ---- navigation stack (single column), iOS style ----
// The incoming page slides over the current one, which drifts left and
// dims slightly. Back does the reverse, and can follow the finger.

const PARALLAX = 0.3;
const width = () => window.innerWidth;

let revealHook = null;
// setRevealHook: called when a hidden pane becomes visible for a transition
// (the app restores its scroll position).
export function setRevealHook(fn) { revealHook = fn; }

function stage(top, under) {
  gsap && gsap.killTweensOf([top, under]);
  under.classList.add('in-transit');
  top.classList.add('in-transit', 'on-top');
  if (revealHook) { revealHook(under); revealHook(top); }
}

function unstage(top, under) {
  top.classList.remove('in-transit', 'on-top');
  under.classList.remove('in-transit');
  if (gsap) gsap.set([top, under], { clearProps: 'transform,filter' });
  else { top.style.transform = ''; under.style.transform = ''; }
}

// Sets the back-swipe position: p = 0 (page in place) .. 1 (page gone).
function place(top, under, p) {
  const w = width();
  const dim = 1 - 0.08 * (1 - p);
  if (gsap) {
    gsap.set(top, { x: p * w });
    gsap.set(under, { x: -PARALLAX * w * (1 - p), filter: `brightness(${dim})` });
  } else {
    top.style.transform = `translateX(${p * w}px)`;
    under.style.transform = `translateX(${-PARALLAX * w * (1 - p)}px)`;
  }
}

// pushPane: `to` arrives over `from`.
export function pushPane(from, to) {
  if (!enabled() || from === to) return;
  stage(to, from);
  const w = width();
  gsap.timeline({ onComplete: () => unstage(to, from) })
    .fromTo(to, { x: w }, { x: 0, duration: 0.46, ease: 'expo.out' }, 0)
    .fromTo(from, { x: 0, filter: 'brightness(1)' }, { x: -PARALLAX * w, filter: 'brightness(0.92)', duration: 0.46, ease: 'expo.out' }, 0);
}

// popPane: `from` (on top) leaves, uncovering `to`.
export function popPane(from, to) {
  if (!enabled() || from === to) return;
  stage(from, to);
  const w = width();
  gsap.timeline({ onComplete: () => unstage(from, to) })
    .fromTo(from, { x: 0 }, { x: w, duration: 0.42, ease: 'power3.out' }, 0)
    .fromTo(to, { x: -PARALLAX * w, filter: 'brightness(0.92)' }, { x: 0, filter: 'brightness(1)', duration: 0.42, ease: 'power3.out' }, 0);
}

// Interactive back swipe.
export function dragStart(top, under) { stage(top, under); place(top, under, 0); }
export function dragTo(top, under, p) { place(top, under, Math.min(Math.max(p, 0), 1)); }
export function dragEnd(top, under, p, commit) {
  return new Promise((resolve) => {
    const done = () => { unstage(top, under); resolve(); };
    if (!gsap || reduced.matches) { done(); return; }
    const w = width();
    const left = commit ? 1 - p : p;
    const duration = Math.max(0.16, Math.min(0.36, left * 0.45));
    gsap.timeline({ onComplete: done })
      .to(top, { x: commit ? w : 0, duration, ease: 'power2.out' }, 0)
      .to(under, { x: commit ? 0 : -PARALLAX * w, filter: commit ? 'brightness(1)' : 'brightness(0.92)', duration, ease: 'power2.out' }, 0);
  });
}

// Text that changes in place (list title when switching collection).
export function swapText(els) {
  if (!enabled()) return;
  gsap.fromTo(els, { opacity: 0, y: 6 }, { opacity: 1, y: 0, duration: 0.3, stagger: 0.04, clearProps: 'transform,opacity' });
}

// Elements appearing one after the other.
export function stagger(els, { y = 16, delay = 0, max = 14 } = {}) {
  const list = Array.from(els).slice(0, max);
  if (!enabled() || !list.length) return;
  gsap.fromTo(list, { y, opacity: 0 }, { y: 0, opacity: 1, duration: 0.45, delay, stagger: 0.04, clearProps: 'transform,opacity' });
}

// A card row arriving like a fanned deck, ending straight.
export function deal(els) {
  const list = Array.from(els);
  if (!enabled() || !list.length) return;
  gsap.fromTo(list,
    { y: 24, rotation: (i) => (i === 0 ? -9 : 4), opacity: 0 },
    { y: 0, rotation: 0, opacity: 1, duration: 0.6, stagger: 0.06, ease: 'back.out(1.6)', clearProps: 'transform,opacity' });
}

// Number counting up to its value.
export function countUp(el, to, { suffix = '', from = 0 } = {}) {
  if (!enabled()) { el.textContent = to + suffix; return; }
  const o = { v: from };
  gsap.to(o, { v: to, duration: 0.9, ease: 'power2.out', onUpdate: () => { el.textContent = Math.round(o.v) + suffix; } });
}

// Progress bars growing from zero.
export function bars(els) {
  const list = Array.from(els);
  if (!enabled() || !list.length) return;
  gsap.from(list, { scaleX: 0, transformOrigin: 'left center', duration: 0.7, stagger: 0.03, ease: 'power2.out', clearProps: 'transform' });
}

// Segments of a ring lighting up in order.
export function ring(segments) {
  const list = Array.from(segments);
  if (!enabled() || !list.length) return;
  gsap.from(list, { opacity: 0, scale: 0.6, transformOrigin: '50% 50%', duration: 0.35, stagger: 0.025, ease: 'back.out(2)' });
}

// Rows leaving before a list reloads: the visible ones slide up and fade,
// one after the other. Resolves when done (at once without motion).
export function leave(els, { max = 10 } = {}) {
  const list = Array.from(els).filter((el) => el.getBoundingClientRect().bottom > 0).slice(0, max);
  if (!enabled() || !list.length) return Promise.resolve();
  return new Promise((resolve) => {
    gsap.to(list, { y: -14, opacity: 0, duration: 0.22, stagger: 0.03, ease: 'power2.in', onComplete: resolve });
  });
}

// A width that changed (progress bars): grows or shrinks from the old one.
export function widthFrom(el, fromPct) {
  if (!enabled() || !el) return;
  gsap.from(el, { width: fromPct + '%', duration: 0.6, ease: 'power2.out', clearProps: 'width' });
}

// Height of a block opening (folder) and closing.
export function expand(els) {
  const list = Array.from(els);
  if (!enabled() || !list.length) return;
  gsap.from(list, { height: 0, opacity: 0, paddingTop: 0, paddingBottom: 0, duration: 0.32, stagger: 0.03, ease: 'power2.out', clearProps: 'all' });
}
export function collapse(els) {
  const list = Array.from(els);
  if (!enabled() || !list.length) return Promise.resolve();
  return new Promise((resolve) => {
    gsap.to(list, { height: 0, opacity: 0, paddingTop: 0, paddingBottom: 0, duration: 0.24, ease: 'power2.in', onComplete: resolve });
  });
}

// Content replaced in place (next/previous article, full text): moves in
// from the direction of travel.
export function swapIn(els, dir = 0) {
  const list = Array.from(els).slice(0, 4);
  if (!enabled() || !list.length) return;
  gsap.fromTo(list, { y: dir * 28 || 10, opacity: 0 }, { y: 0, opacity: 1, duration: 0.4, stagger: 0.04, clearProps: 'transform,opacity' });
}

// A row whose state changed (read, starred): eases from its old look.
export function rowChanged(row, { fromOpacity, dot, star } = {}) {
  if (!enabled() || !row) return;
  if (fromOpacity != null) gsap.from(row, { opacity: fromOpacity, duration: 0.35, clearProps: 'opacity' });
  if (dot) gsap.from(dot, { scale: 0, duration: 0.35, ease: 'back.out(3)', clearProps: 'transform' });
  if (star) pop(star);
}

// Confirmation sheet: the ring grows in, the icon springs into place, then the
// text and the buttons follow.
export function confirmIn(ring, rest) {
  if (!enabled()) return;
  const tl = gsap.timeline({ delay: 0.08 });
  tl.fromTo(ring, { scale: 0.6, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.45, ease: 'back.out(2.2)', clearProps: 'transform,opacity' });
  const svg = ring.querySelector('svg');
  if (svg) tl.fromTo(svg, { scale: 0.4, rotation: -20 }, { scale: 1, rotation: 0, duration: 0.5, ease: 'back.out(3)', clearProps: 'transform' }, 0.12);
  tl.fromTo(Array.from(rest), { y: 10, opacity: 0 }, { y: 0, opacity: 1, duration: 0.35, stagger: 0.05, clearProps: 'transform,opacity' }, 0.15);
}

// Images leaving or entering a row sideways (list images on/off).
export function collapseWidth(els) {
  const list = Array.from(els);
  if (!enabled() || !list.length) return Promise.resolve();
  return new Promise((resolve) => {
    gsap.to(list, { width: 0, opacity: 0, scale: 0.8, marginLeft: 0, duration: 0.3, ease: 'power2.in', onComplete: resolve });
  });
}
export function expandWidth(els) {
  const list = Array.from(els);
  if (!enabled() || !list.length) return;
  gsap.from(list, { width: 0, opacity: 0, scale: 0.8, duration: 0.4, ease: 'power3.out', clearProps: 'all' });
}

// A short horizontal shake (an error message, a refused value).
export function shake(el) {
  if (!enabled() || !el) return;
  gsap.fromTo(el, { x: 0 }, { keyframes: { x: [-8, 7, -5, 3, 0] }, duration: 0.4, ease: 'power1.out', clearProps: 'transform' });
}

// A small "pop" on a toggled control.
export function pop(el) {
  if (!enabled() || !el) return;
  gsap.fromTo(el, { scale: 0.7 }, { scale: 1, duration: 0.45, ease: 'back.out(3)', clearProps: 'transform' });
}

// Sheets: in from the bottom on phones, scale-in centered on wide screens.
const wide = () => window.matchMedia('(min-width: 640px)').matches;

export function sheetIn(backdrop, sheet) {
  if (!enabled()) {
    backdrop.style.opacity = '1';
    return;
  }
  gsap.fromTo(backdrop, { opacity: 0 }, { opacity: 1, duration: 0.25, ease: 'none' });
  if (wide()) gsap.fromTo(sheet, { opacity: 0, scale: 0.94, y: 12 }, { opacity: 1, scale: 1, y: 0, duration: 0.32, clearProps: 'transform' });
  else gsap.fromTo(sheet, { yPercent: 110 }, { yPercent: 0, duration: 0.42, ease: 'expo.out', clearProps: 'transform' });
}

export function sheetOut(backdrop, sheet) {
  if (!enabled()) return Promise.resolve();
  const tl = gsap.timeline();
  tl.to(backdrop, { opacity: 0, duration: 0.22, ease: 'none' }, 0);
  if (wide()) tl.to(sheet, { opacity: 0, scale: 0.96, duration: 0.2, ease: 'power2.in' }, 0);
  else tl.to(sheet, { yPercent: 110, duration: 0.28, ease: 'power3.in' }, 0);
  return tl.then();
}

export function toastIn(el) {
  if (!enabled()) return;
  gsap.fromTo(el, { y: 16, opacity: 0, scale: 0.96 }, { y: 0, opacity: 1, scale: 1, duration: 0.35, ease: 'back.out(1.8)' });
}

export function toastOut(el) {
  if (!enabled()) return Promise.resolve();
  return gsap.to(el, { y: 8, opacity: 0, duration: 0.2, ease: 'power2.in' }).then();
}

// The welcome illustration floating gently.
export function float(els) {
  if (!enabled()) return;
  Array.from(els).forEach((el, i) => {
    gsap.to(el, { y: i % 2 ? 6 : -6, rotation: i % 2 ? '+=2' : '-=2', duration: 2.4 + i * 0.3, repeat: -1, yoyo: true, ease: 'sine.inOut' });
  });
}
