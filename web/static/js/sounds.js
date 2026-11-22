// Sound effects: short tones made on the fly with Web Audio, no files.
// They are off by default and belong to the device (prefs). On the iPhone
// the audio session is "ambient": the silent switch mutes them and music
// playing in another app goes on.
//
// play() is called only for actions the person takes: never for articles
// marked as read while scrolling or when an article is opened.

import { getPrefs } from './prefs.js';
import { t } from './i18n.js';

export const VOLUMES = [[30, t('Low|volume')], [60, t('Medium|volume')], [90, t('High|volume')]];

let ctx = null;
let master = null;
let noiseBuf = null;
const lastPlayed = {};

function enabled() {
  return getPrefs().sounds === true;
}

function volume() {
  const v = Number(getPrefs().sound_volume);
  return Math.min(100, Math.max(0, Number.isFinite(v) ? v : 60));
}

function audio() {
  if (!ctx) {
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return null;
    try { if (navigator.audioSession) navigator.audioSession.type = 'ambient'; } catch (e) { /* not supported */ }
    ctx = new AC();
    master = ctx.createGain();
    master.connect(ctx.destination);
    noiseBuf = ctx.createBuffer(1, ctx.sampleRate, ctx.sampleRate);
    const d = noiseBuf.getChannelData(0);
    for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
  }
  if (ctx.state !== 'running') ctx.resume().catch(() => {});
  master.gain.value = (volume() / 100) ** 2 * 0.9;
  return ctx;
}

// Browsers start audio only from a tap or a key: the first one after the
// app opens (or comes back from the background) wakes the audio up, so a
// sound that follows a wait (new articles after a refresh) can play too.
function wake() {
  if (enabled() && (!ctx || ctx.state !== 'running')) audio();
}
window.addEventListener('pointerdown', wake, { capture: true, passive: true });
window.addEventListener('keydown', wake, { capture: true, passive: true });

// A tone with a quick attack and an exponential decay.
function tone({ f, f2, at = 0, dur = 0.15, type = 'sine', g = 0.3, attack = 0.004 }) {
  const t = ctx.currentTime + 0.01 + at;
  const o = ctx.createOscillator();
  const v = ctx.createGain();
  o.type = type;
  o.frequency.setValueAtTime(f, t);
  if (f2) o.frequency.exponentialRampToValueAtTime(f2, t + dur);
  v.gain.setValueAtTime(0.0001, t);
  v.gain.exponentialRampToValueAtTime(g, t + attack);
  v.gain.exponentialRampToValueAtTime(0.0001, t + dur);
  o.connect(v).connect(master);
  o.start(t);
  o.stop(t + dur + 0.05);
}

// Filtered noise, the filter sweeping from f to f2.
function noise({ at = 0, dur = 0.1, type = 'bandpass', f = 2000, f2, q = 1, g = 0.3, attack = 0.003 }) {
  const t = ctx.currentTime + 0.01 + at;
  const s = ctx.createBufferSource();
  const flt = ctx.createBiquadFilter();
  const v = ctx.createGain();
  s.buffer = noiseBuf;
  flt.type = type;
  flt.Q.value = q;
  flt.frequency.setValueAtTime(f, t);
  if (f2) flt.frequency.exponentialRampToValueAtTime(f2, t + dur);
  v.gain.setValueAtTime(0.0001, t);
  v.gain.exponentialRampToValueAtTime(g, t + attack);
  v.gain.exponentialRampToValueAtTime(0.0001, t + dur);
  s.connect(flt).connect(v).connect(master);
  s.start(t);
  s.stop(t + dur + 0.05);
}

const SOUNDS = {
  // New articles after a refresh: a soft bell arpeggio (E5, G5, C6).
  refresh() {
    [659.25, 783.99, 1046.5].forEach((f, i) => tone({ f, at: i * 0.07, dur: 0.38, type: 'triangle', g: 0.2, attack: 0.006 }));
  },
  // Starred: a small crystal bell.
  star() {
    [[2093, 0.18, 0.6], [2093 * 2.76, 0.05, 0.3], [2093 * 5.4, 0.02, 0.15]].forEach(([f, g, dur]) => tone({ f, g, dur, attack: 0.002 }));
  },
  // Marked as read: a short falling drop.
  read() {
    tone({ f: 1200, f2: 900, dur: 0.06, g: 0.2, attack: 0.002 });
  },
  // Everything marked as read: a rising swoosh.
  allread() {
    noise({ dur: 0.34, f: 400, f2: 4200, q: 1.4, g: 0.22, attack: 0.08 });
  },
  // Something failed: two soft low thumps.
  error() {
    tone({ f: 140, f2: 60, dur: 0.11, g: 0.5, attack: 0.002 });
    tone({ f: 140, f2: 60, at: 0.13, dur: 0.11, g: 0.5, attack: 0.002 });
  },
};

export const NAMES = Object.keys(SOUNDS);

// play makes the named sound, if sounds are on. force plays it anyway
// (the preview in the settings).
export function play(name, { force = false } = {}) {
  if (!force && !enabled()) return;
  const fn = SOUNDS[name];
  if (!fn) return;
  // The same sound twice in a few milliseconds (several rows at once) is one.
  const now = performance.now();
  if (now - (lastPlayed[name] || 0) < 80) return;
  lastPlayed[name] = now;
  try {
    if (!audio()) return;
    fn();
  } catch (e) { /* audio unavailable: stay silent */ }
}
