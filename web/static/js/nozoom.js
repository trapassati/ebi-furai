// The app does not zoom like a web page: pinch and double tap would leave
// the interface half off screen. Article images open in the image viewer
// instead, where they can be zoomed (views/imageviewer.js).
//
// touch-action: manipulation (in the CSS) removes the double-tap zoom.
// iOS ignores user-scalable=no, so the pinch is stopped here: Safari's
// gesture events, and touch moves with more than one finger.

const inViewer = (e) => e.target instanceof Element && e.target.closest('.iv');

for (const type of ['gesturestart', 'gesturechange', 'gestureend']) {
  document.addEventListener(type, (e) => { if (!inViewer(e)) e.preventDefault(); }, { passive: false });
}
document.addEventListener('touchmove', (e) => {
  if (e.touches.length > 1 && !inViewer(e)) e.preventDefault();
}, { passive: false });
// Desktop browsers: Ctrl + wheel and trackpad pinches zoom the page.
document.addEventListener('wheel', (e) => {
  if (e.ctrlKey && !inViewer(e)) e.preventDefault();
}, { passive: false });
