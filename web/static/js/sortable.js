// Sortable list: the rows move by their handle (finger, mouse or pen),
// the others make room with GSAP, and the page scrolls by itself near its
// edges. With the keyboard the handle moves its row with the arrows.
// onDrop(ids) gets the new order of the rows' data-id when it changed.

import * as motion from './motion.js';

const gsap = window.gsap;
const EDGE = 56; // px from the edge of the scroller where it starts scrolling

export function sortable(list, { scroller, rowSelector, handleSelector, onDrop, onMove }) {
  const rows = () => [...list.querySelectorAll(':scope > ' + rowSelector)];
  const order = () => rows().map((r) => r.dataset.id);
  let drag = null;

  // Swaps row with its neighbour (dir 1: the next one, -1: the previous
  // one), which glides to its new place. The neighbour is the one moved in
  // the document: taking out the dragged row would end the drag (its handle
  // holds the pointer) and the keyboard focus.
  const swap = (row, dir) => {
    const other = dir > 0 ? row.nextElementSibling : row.previousElementSibling;
    if (!other || !other.matches(rowSelector)) return false;
    const before = other.offsetTop;
    if (dir > 0) list.insertBefore(other, row); else list.insertBefore(other, row.nextElementSibling);
    const dy = before - other.offsetTop;
    if (dy && motion.enabled()) gsap.fromTo(other, { y: dy }, { y: 0, duration: 0.22, ease: 'power2.out', overwrite: true });
    return true;
  };

  const follow = () => {
    if (!drag) return;
    const { row, grab } = drag;
    // The desktop scales the interface (zoom): the pointer is in screen
    // pixels, offsetTop in layout pixels.
    const want = (drag.y - list.getBoundingClientRect().top) / drag.zoom - grab;
    // The neighbour moves when the dragged row passes half of it.
    const next = row.nextElementSibling;
    const prev = row.previousElementSibling;
    if (next && next.matches(rowSelector) && want + row.offsetHeight > next.offsetTop + next.offsetHeight / 2) swap(row, 1);
    else if (prev && prev.matches(rowSelector) && want < prev.offsetTop + prev.offsetHeight / 2) swap(row, -1);
    const maxY = list.offsetHeight - row.offsetHeight;
    row.style.transform = `translateY(${Math.min(Math.max(want, -row.offsetHeight / 2), maxY + row.offsetHeight / 2) - row.offsetTop}px) scale(1.02)`;
  };

  const tick = () => {
    if (!drag) return;
    const box = scroller.getBoundingClientRect();
    let v = 0;
    if (drag.y < box.top + EDGE) v = -Math.ceil((box.top + EDGE - drag.y) / 6);
    else if (drag.y > box.bottom - EDGE) v = Math.ceil((drag.y - box.bottom + EDGE) / 6);
    if (v) {
      const top = scroller.scrollTop;
      motion.scrollTop(scroller, top + v);
      if (scroller.scrollTop !== top) follow();
    }
    drag.raf = requestAnimationFrame(tick);
  };

  const end = () => {
    if (!drag) return;
    const { row, start, raf, handle, id } = drag;
    cancelAnimationFrame(raf);
    drag = null;
    list.classList.remove('sorting');
    row.style.transform = '';
    row.classList.remove('dragging');
    handle.releasePointerCapture?.(id);
    if (motion.enabled()) gsap.fromTo(row, { y: row.dataset.dropFrom || 0 }, { y: 0, duration: 0.2, ease: 'power2.out', clearProps: 'transform' });
    delete row.dataset.dropFrom;
    const now = order();
    if (now.join() !== start) onDrop(now);
  };

  list.addEventListener('pointerdown', (e) => {
    const handle = e.target.closest(handleSelector);
    if (!handle || drag || (e.pointerType === 'mouse' && e.button !== 0)) return;
    const row = handle.closest(rowSelector);
    if (!row || row.parentElement !== list) return;
    e.preventDefault();
    if (gsap) gsap.killTweensOf(row);
    const r = row.getBoundingClientRect();
    const zoom = r.height / row.offsetHeight || 1;
    drag = { row, handle, id: e.pointerId, y: e.clientY, zoom, grab: (e.clientY - r.top) / zoom, start: order().join(), raf: 0 };
    handle.setPointerCapture?.(e.pointerId);
    list.classList.add('sorting');
    row.classList.add('dragging');
    follow();
    drag.raf = requestAnimationFrame(tick);
  });
  list.addEventListener('pointermove', (e) => {
    if (!drag || e.pointerId !== drag.id) return;
    e.preventDefault();
    drag.y = e.clientY;
    follow();
  });
  list.addEventListener('pointerup', (e) => {
    if (!drag || e.pointerId !== drag.id) return;
    // Where the row was, for the last glide into place.
    const m = /translateY\(([-\d.]+)px\)/.exec(drag.row.style.transform);
    if (m) drag.row.dataset.dropFrom = m[1];
    end();
  });
  list.addEventListener('pointercancel', end);
  list.addEventListener('lostpointercapture', (e) => { if (drag && e.pointerId === drag.id) end(); });

  list.addEventListener('keydown', (e) => {
    const handle = e.target.closest(handleSelector);
    if (!handle || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
    const row = handle.closest(rowSelector);
    e.preventDefault();
    const start = order().join();
    swap(row, e.key === 'ArrowUp' ? -1 : 1);
    const now = order();
    if (now.join() !== start) { onDrop(now); onMove?.(row); }
  });

  return { busy: () => !!drag };
}
