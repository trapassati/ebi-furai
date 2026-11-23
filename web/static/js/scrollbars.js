// Scrollbars appear only while something scrolls, then fade out, as on
// macOS: the element that scrolls gets .sb-on for a moment (the CSS keeps
// them hidden otherwise). Only with a mouse or a trackpad: touch screens
// do this by themselves.

const LINGER = 900;
const timers = new WeakMap();

function scrolled(e) {
  const el = e.target === document ? document.scrollingElement : e.target;
  if (!el || !el.classList) return;
  el.classList.add('sb-on');
  clearTimeout(timers.get(el));
  timers.set(el, setTimeout(() => el.classList.remove('sb-on'), LINGER));
}

if (window.matchMedia('(hover: hover) and (pointer: fine)').matches) {
  document.addEventListener('scroll', scrolled, { capture: true, passive: true });
}
