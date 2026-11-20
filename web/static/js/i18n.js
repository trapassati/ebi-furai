// Interface language: English or Italian. The code is written in English
// and i18n-it.js holds the Italian. "auto" follows the device's languages;
// the choice is per device (localStorage) and applies on reload.
//
// t('Text') translates; t('{0} unread', n) fills the placeholders. A key may
// carry a context after "|" to tell apart words that are the same in
// English but not in Italian: t('Read|one') shows "Read".

import { IT } from './i18n-it.js';

const KEY = 'lang';

export function langPref() {
  try { return localStorage.getItem(KEY) || 'auto'; } catch { return 'auto'; }
}

function detect() {
  const pref = langPref();
  if (pref === 'it' || pref === 'en') return pref;
  for (const l of navigator.languages || [navigator.language || 'en']) {
    const base = String(l).slice(0, 2).toLowerCase();
    if (base === 'it' || base === 'en') return base;
  }
  return 'en';
}

export const lang = detect();

// For Intl: Italian, or the device's own English (en-US writes dates its way).
export const locale = lang === 'it' ? 'it-IT'
  : (String(navigator.language || '').toLowerCase().startsWith('en') ? navigator.language : 'en-GB');

document.documentElement.lang = lang;

export function setLang(v) {
  try {
    if (v === 'auto') localStorage.removeItem(KEY); else localStorage.setItem(KEY, v);
  } catch { /* private mode: stays for this session only */ }
}

export function t(key, ...args) {
  let s = lang === 'it' ? IT[key] : undefined;
  if (s === undefined) {
    const i = key.indexOf('|');
    s = i < 0 ? key : key.slice(0, i);
  }
  return args.length ? s.replace(/\{(\d+)\}/g, (m, n) => (args[n] ?? m)) : s;
}
