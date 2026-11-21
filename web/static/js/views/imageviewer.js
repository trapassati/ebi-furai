// Full-screen image viewer. The app blocks the browser's zoom (pinch and
// double tap), so the images of an article open here with a tap, where
// they can be zoomed: pinch or double tap on touch screens, the wheel with
// Ctrl (or a trackpad pinch) and double click on the desktop. Drag to move
// a zoomed image; swipe down (or Esc, or the X) to close.

import { t } from '../i18n.js';
import { h, icon } from '../ui.js';

const gsap = window.gsap;
const reduced = matchMedia('(prefers-reduced-motion: reduce)');
const MAX = 5;

// An image is worth opening when it is a real picture, not an icon or a
// tracking pixel.
function worthOpening(img) {
  return img.naturalWidth >= 120 && img.naturalHeight >= 80;
}

// Article images open the viewer, unless they are a link to another page
// (then the link wins).
export function enableImageViewer(container) {
  container.addEventListener('click', (e) => {
    const img = e.target.closest('img');
    if (!img || !container.contains(img) || !worthOpening(img)) return;
    const link = img.closest('a');
    if (link && !/\.(jpe?g|png|gif|webp|avif)(\?|#|$)/i.test(link.getAttribute('href') || '')) return;
    e.preventDefault();
    openImage(img);
  });
}

export function openImage(source) {
  const img = h('img', { class: 'iv-img', src: source.currentSrc || source.src, alt: source.alt || '', draggable: 'false' });
  const close = h('button', { class: 'iv-close', 'aria-label': t('Close') }, icon('o-x-mark', 'w-6 h-6'));
  const caption = source.alt ? h('div', { class: 'iv-caption', text: source.alt }) : null;
  const box = h('div', { class: 'iv', role: 'dialog', 'aria-modal': 'true', 'aria-label': source.alt || t('Image') },
    h('div', { class: 'iv-backdrop' }), h('div', { class: 'iv-stage' }, img), close, caption);
  document.body.append(box);
  const prevFocus = document.activeElement;
  close.focus({ preventScroll: true });

  // Zoom state: scale and translation of the image.
  let scale = 1, x = 0, y = 0;
  const apply = (animate) => {
    const v = { x, y, scale };
    if (gsap && animate && !reduced.matches) gsap.to(img, { ...v, duration: 0.3, ease: 'power3.out' });
    else if (gsap) gsap.set(img, v);
    else img.style.transform = `translate(${x}px, ${y}px) scale(${scale})`;
  };
  // Keep a zoomed image on screen.
  const clamp = () => {
    const w = img.offsetWidth * scale, hh = img.offsetHeight * scale;
    const mx = Math.max(0, (w - innerWidth) / 2), my = Math.max(0, (hh - innerHeight) / 2);
    x = Math.min(mx, Math.max(-mx, x));
    y = Math.min(my, Math.max(-my, y));
  };
  // Zoom to s keeping the point (px, py) of the screen still.
  const zoomAt = (s, px, py, animate) => {
    s = Math.min(MAX, Math.max(1, s));
    const cx = px - innerWidth / 2, cy = py - innerHeight / 2;
    x = cx - (cx - x) * (s / scale);
    y = cy - (cy - y) * (s / scale);
    scale = s;
    if (scale === 1) { x = 0; y = 0; }
    clamp();
    apply(animate);
  };

  let closing = false;
  const done = () => {
    if (closing) return;
    closing = true;
    window.removeEventListener('keydown', onKey, true);
    const end = () => { box.remove(); if (prevFocus && prevFocus.focus) prevFocus.focus({ preventScroll: true }); };
    if (gsap && !reduced.matches) {
      gsap.to(box.querySelector('.iv-backdrop'), { opacity: 0, duration: 0.25 });
      gsap.to([close, caption].filter(Boolean), { opacity: 0, duration: 0.15 });
      gsap.to(img, { scale: scale * 0.92, opacity: 0, y: y + 30, duration: 0.25, ease: 'power2.in', onComplete: end });
    } else end();
  };
  // Keys belong to the viewer while it is open: the app's shortcuts
  // (Esc to go back, J/K…) wait until it closes.
  const onKey = (e) => {
    if (e.key === 'Tab') return;
    e.stopImmediatePropagation();
    if (e.key === 'Escape') { e.preventDefault(); done(); }
  };
  window.addEventListener('keydown', onKey, true);
  close.addEventListener('click', done);

  // ---- gestures ----
  const pointers = new Map();
  let start = null;      // gesture start: positions, scale, translation
  let lastTap = 0;
  let swipe = 0;         // vertical drag to close (at scale 1)

  const mid = () => {
    const p = [...pointers.values()];
    return { x: (p[0].x + p[1].x) / 2, y: (p[0].y + p[1].y) / 2, d: Math.hypot(p[0].x - p[1].x, p[0].y - p[1].y) };
  };
  const begin = () => {
    if (pointers.size === 2) { const m = mid(); start = { mx: m.x, my: m.y, d: m.d || 1, scale, tx: x, ty: y, pinch: true }; }
    else {
      const p = [...pointers.values()][0];
      start = { x: p.x, y: p.y, tx: x, ty: y, moved: false, t: Date.now(), onImage: lastTarget === img };
    }
  };
  const stage = box.querySelector('.iv-stage');
  let lastTarget = null, lastType = 'mouse';
  stage.addEventListener('pointerdown', (e) => {
    lastTarget = e.target;
    lastType = e.pointerType;
    stage.setPointerCapture(e.pointerId);
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    begin();
  });
  stage.addEventListener('pointermove', (e) => {
    if (!pointers.has(e.pointerId) || !start) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (start.pinch && pointers.size === 2) {
      const m = mid();
      const s = Math.min(MAX * 1.2, Math.max(0.8, start.scale * (m.d / start.d)));
      // The image point under the fingers at the start stays under them.
      const k = s / start.scale;
      const cx = start.mx - innerWidth / 2, cy = start.my - innerHeight / 2;
      x = (m.x - innerWidth / 2) - k * (cx - start.tx);
      y = (m.y - innerHeight / 2) - k * (cy - start.ty);
      scale = s;
      apply(false);
      return;
    }
    if (pointers.size !== 1 || start.pinch) return;
    const p = [...pointers.values()][0];
    const dx = p.x - start.x, dy = p.y - start.y;
    if (Math.abs(dx) + Math.abs(dy) > 6) start.moved = true;
    if (scale > 1) {
      x = start.tx + dx; y = start.ty + dy;
      apply(false);
    } else {
      swipe = Math.max(0, dy);
      const fade = Math.max(0.2, 1 - swipe / 400);
      if (gsap) {
        gsap.set(img, { y: swipe, scale: 1 - swipe / 2000 });
        gsap.set(box.querySelector('.iv-backdrop'), { opacity: fade });
      }
    }
  });
  const end = (e) => {
    if (!pointers.has(e.pointerId)) return;
    pointers.delete(e.pointerId);
    if (start && start.pinch) {
      if (pointers.size < 2) {
        if (scale <= 1) { scale = 1; x = 0; y = 0; } else if (scale > MAX) { scale = MAX; } clamp(); apply(true);
        start = null;
        if (pointers.size === 1) begin();
      }
      return;
    }
    if (pointers.size) return;
    const s = start;
    start = null;
    if (!s) return;
    if (scale === 1 && swipe) {
      if (swipe > 110) { y = swipe; done(); } else {
        swipe = 0;
        if (gsap) { gsap.to(img, { y: 0, scale: 1, duration: 0.25 }); gsap.to(box.querySelector('.iv-backdrop'), { opacity: 1, duration: 0.25 }); }
      }
      swipe = 0;
      return;
    }
    if (scale > 1) { clamp(); apply(true); }
    if (!s.moved && Date.now() - s.t < 300 && !s.onImage && scale === 1) { done(); return; }
    if (!s.moved && Date.now() - s.t < 300 && lastType === 'touch') {
      // Double tap: zoom in at the point, or back to fit.
      const now = Date.now();
      if (now - lastTap < 320) {
        lastTap = 0;
        if (scale > 1) zoomAt(1, s.x, s.y, true); else zoomAt(2.5, s.x, s.y, true);
      } else lastTap = now;
    }
  };
  stage.addEventListener('pointerup', end);
  stage.addEventListener('pointercancel', end);
  // Desktop: Ctrl + wheel, or a trackpad pinch (which comes as Ctrl + wheel).
  stage.addEventListener('wheel', (e) => {
    e.preventDefault();
    if (e.ctrlKey) zoomAt(scale * Math.exp(-e.deltaY / 200), e.clientX, e.clientY, false);
    else if (scale > 1) { x -= e.deltaX; y -= e.deltaY; clamp(); apply(false); }
  }, { passive: false });
  stage.addEventListener('dblclick', (e) => {
    if (lastType === 'touch') return;
    if (scale > 1) zoomAt(1, e.clientX, e.clientY, true); else zoomAt(2.5, e.clientX, e.clientY, true);
  });

  if (gsap && !reduced.matches) {
    // Grow from where the image was in the article.
    const r = source.getBoundingClientRect();
    gsap.from(box.querySelector('.iv-backdrop'), { opacity: 0, duration: 0.3 });
    img.decode().catch(() => {}).then(() => {
      const f = img.getBoundingClientRect();
      if (!f.width) return;
      gsap.from(img, {
        x: r.left + r.width / 2 - (f.left + f.width / 2), y: r.top + r.height / 2 - (f.top + f.height / 2),
        scale: r.width / f.width, duration: 0.4, ease: 'expo.out', clearProps: 'x,y',
        onComplete: () => gsap.set(img, { x: 0, y: 0, scale: 1 }),
      });
    });
    gsap.from([close, caption].filter(Boolean), { opacity: 0, duration: 0.3, delay: 0.15 });
  }
}
