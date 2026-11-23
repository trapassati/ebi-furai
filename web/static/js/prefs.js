import { t } from './i18n.js';
// Preferences: theme, reading font, size and line spacing, list text size,
// filter and order. They belong to the device (localStorage): changing the
// font on the computer does not change it on the iPhone. A device with none
// yet starts from what the account had saved before (the server copy is no
// longer written; account settings such as the refresh interval live on
// the server on their own).

export const FONTS = {
  outfit: { label: 'Outfit', stack: '"Outfit", -apple-system, system-ui, sans-serif' },
  sans: { label: t('System'), stack: '-apple-system, BlinkMacSystemFont, ui-sans-serif, system-ui, sans-serif' },
  serif: { label: 'Serif', stack: 'ui-serif, "New York", "Iowan Old Style", Georgia, serif' },
  rounded: { label: t('Rounded'), stack: 'ui-rounded, "SF Pro Rounded", -apple-system, system-ui, sans-serif' },
  mono: { label: 'Monospace', stack: 'ui-monospace, "SF Mono", Menlo, monospace' },
};

export const DEFAULTS = {
  theme: 'light', font: 'outfit', size: 18, filter: 'unread', order: 'new',
  offline: true, offline_limit: 200, offline_images: false, book: true, mark_on_scroll: false, thumbs: true,
  list_size: 1, line_height: 1.65, sounds: false, sound_volume: 60, listen_rate: 1,
};

// Choices offered in the settings.
export const LIST_SIZES = [[0.9, t('Small')], [1, t('Normal')], [1.12, t('Large')], [1.25, t('Huge')]];
export const LINE_HEIGHTS = [[1.45, t('Tight')], [1.65, t('Normal')], [1.85, t('Wide')]];

let prefs = { ...DEFAULTS };
const listeners = new Set();

export function getPrefs() { return prefs; }

export function onPrefsChange(fn) { listeners.add(fn); }

export function loadPrefs(serverPrefs) {
  let cached = null;
  try { cached = JSON.parse(localStorage.getItem('prefs') || 'null'); } catch (e) { /* ignore */ }
  prefs = { ...DEFAULTS, ...(cached || serverPrefs || {}) };
  apply();
}

export function setPref(key, value) {
  prefs = { ...prefs, [key]: value };
  apply();
  listeners.forEach((fn) => fn(prefs, key));
}

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');
darkQuery.addEventListener('change', () => apply());

function apply() {
  try { localStorage.setItem('prefs', JSON.stringify(prefs)); } catch (e) { /* ignore */ }
  const theme = prefs.theme === 'auto' ? (darkQuery.matches ? 'dark' : 'light') : prefs.theme;
  const root = document.documentElement;
  root.setAttribute('data-theme', theme);
  const font = FONTS[prefs.font] || FONTS.outfit;
  root.style.setProperty('--reader-font', font.stack);
  const size = Math.min(28, Math.max(13, Number(prefs.size) || DEFAULTS.size));
  root.style.setProperty('--reader-size', `calc(${size}px * var(--ui, 1))`);
  root.style.setProperty('--reader-line', String(Math.min(2.2, Math.max(1.2, Number(prefs.line_height) || DEFAULTS.line_height))));
  root.style.setProperty('--list-scale', String(Math.min(1.5, Math.max(0.8, Number(prefs.list_size) || 1))));
  // The status bar color follows the chosen theme.
  const color = theme === 'dark' ? '#0b1020' : '#f3f5fa';
  document.querySelectorAll('meta[name="theme-color"]').forEach((m) => m.setAttribute('content', color));
}
