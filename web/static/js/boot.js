// Runs before first paint: applies the cached theme and reading prefs so
// the app does not flash the wrong colors. Kept as a classic script (CSP
// forbids inline scripts).
(function () {
  var prefs = {};
  try { prefs = JSON.parse(localStorage.getItem('prefs') || '{}') || {}; } catch (e) { /* ignore */ }
  var theme = prefs.theme || 'light';
  if (theme === 'auto') {
    theme = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  document.documentElement.setAttribute('data-theme', theme);
})();


// Dynamic Type: scale the interface text with the iOS text size setting.
(function () {
  var de = document.documentElement;
  function apply() {
    if (!window.CSS || !CSS.supports('font', '-apple-system-body')) return;
    var p = document.createElement('div');
    p.style.font = '-apple-system-body';
    p.style.position = 'absolute';
    p.style.visibility = 'hidden';
    de.appendChild(p);
    var size = parseFloat(getComputedStyle(p).fontSize) || 17;
    de.removeChild(p);
    var scale = Math.min(1.5, Math.max(0.82, size / 17));
    de.style.setProperty('--type-scale', scale.toFixed(3));
  }
  apply();
  document.addEventListener('visibilitychange', function () { if (document.visibilityState === 'visible') apply(); });
})();

// Interface scale (--ui): on big desktop screens with a mouse everything
// (text, icons, buttons, columns) grows with the width of the window:
// 100% up to 1440 px, about 113% at 1920 and 130% at 2560, at most 135%.
// Touch devices always stay at 100%. A size chosen in the settings
// ("Dimensione interfaccia") wins; it is kept on this device only.
(function () {
  var de = document.documentElement;
  var desktop = window.matchMedia && window.matchMedia('(hover: hover) and (pointer: fine)');
  function apply() {
    var v = 'auto';
    try { v = localStorage.getItem('uiScale') || 'auto'; } catch (e) { /* ignore */ }
    var s = 1;
    if (v !== 'auto') s = Math.min(1.5, Math.max(0.8, parseFloat(v) || 1));
    else if (desktop && desktop.matches && window.innerWidth > 1440) s = Math.min(1.35, 1 + (window.innerWidth - 1440) * 0.3 / 1120);
    s = Math.round(s * 100) / 100;
    if (window.__uiScale === s) return;
    window.__uiScale = s;
    de.style.setProperty('--ui', String(s));
  }
  window.__applyUiScale = apply;
  apply();
  window.addEventListener('resize', apply);
  if (desktop && desktop.addEventListener) desktop.addEventListener('change', apply);
})();
