// "Listen": the article read aloud by the device's own voice (Web Speech),
// paragraph by paragraph. A small player floats over the article: back and
// forward by a paragraph, play and pause, the speed. The paragraph being
// read is highlighted and kept in view. At the end the player closes.
//
// Speech is one utterance per sentence group: long utterances stop
// halfway in some browsers. Pause cancels and remembers the place, since
// speechSynthesis.pause() is unreliable on several platforms.

import { t } from '../i18n.js';
import { getPrefs, setPref } from '../prefs.js';
import { h, icon } from '../ui.js';
import * as motion from '../motion.js';

const RATES = [0.8, 1, 1.25, 1.5];
const BLOCKS = 'p, h1, h2, h3, h4, h5, h6, li, blockquote, figcaption, dt, dd, td, .listen-run';
const CONTAINERS = 'div, section, article, main, aside, header, footer';
const BLOCK_TAGS = /^(P|H[1-6]|UL|OL|LI|DL|DT|DD|BLOCKQUOTE|FIGURE|FIGCAPTION|TABLE|PRE|DIV|SECTION|ARTICLE|MAIN|ASIDE|HEADER|FOOTER|HR|IMG|PICTURE|VIDEO|IFRAME)$/;
const MAX_CHUNK = 220;

let player = null; // { el, scroller, items, i, chunk, playing, lang, voice, ... }

export const supported = () => 'speechSynthesis' in window && 'SpeechSynthesisUtterance' in window;

export function listening() {
  return !!player;
}

// ---- the text ----

// A rough guess of the article's language from its most common words, so
// that an English article is not read by an Italian voice.
const STOP = {
  it: ['il', 'di', 'che', 'e', 'la', 'per', 'non', 'un', 'una', 'del', 'della', 'sono', 'con', 'gli', 'nel'],
  en: ['the', 'of', 'and', 'to', 'is', 'that', 'for', 'it', 'with', 'are', 'this', 'was', 'on', 'be', 'you'],
  fr: ['le', 'les', 'des', 'et', 'est', 'une', 'pour', 'dans', 'que', 'qui', 'pas', 'sur', 'du', 'au', 'avec'],
  de: ['der', 'die', 'und', 'das', 'ist', 'nicht', 'mit', 'ein', 'eine', 'zu', 'den', 'von', 'auf', 'sich', 'auch'],
  es: ['el', 'los', 'las', 'que', 'y', 'es', 'por', 'una', 'para', 'con', 'del', 'como', 'pero', 'más', 'se'],
};

export function guessLang(text, fallback) {
  const words = text.toLowerCase().match(/\p{L}+/gu) || [];
  if (words.length < 8) return fallback;
  let best = fallback;
  let top = 0;
  for (const [lang, list] of Object.entries(STOP)) {
    const set = new Set(list);
    let n = 0;
    for (const w of words) if (set.has(w)) n++;
    if (n > top) { top = n; best = lang; }
  }
  return top >= 3 ? best : fallback;
}

// Text that sits loose in the article (not in a paragraph: some feeds send
// plain text with <br>) is wrapped in inline spans, which leave the layout
// as it is, so that it can be read and highlighted too.
function wrapRuns(container) {
  let run = [];
  const flush = () => {
    if (run.some((n) => n.textContent.trim())) {
      const span = document.createElement('span');
      span.className = 'listen-run';
      run[0].before(span);
      span.append(...run);
    }
    run = [];
  };
  for (const n of [...container.childNodes]) {
    if (n.nodeType === 1 && BLOCK_TAGS.test(n.tagName)) flush();
    else if (n.nodeType === 1 && n.tagName === 'BR' && run.length && n.nextSibling && n.nextSibling.nodeName === 'BR') { flush(); }
    else if (n.nodeType === 1 || n.nodeType === 3) run.push(n);
  }
  flush();
}

// The blocks to read, in order: the title, then the text of the article.
// Nested blocks (a paragraph in a list item) are read once, by the inner one.
function blocks(wrap) {
  const out = [];
  const title = wrap.querySelector('.reader-title');
  if (title && title.textContent.trim()) out.push(title);
  const body = wrap.querySelector('.article-body');
  if (!body) return out;
  if (!body.querySelector('.listen-run')) {
    wrapRuns(body);
    body.querySelectorAll(CONTAINERS).forEach(wrapRuns);
  }
  for (const el of body.querySelectorAll(BLOCKS)) {
    if (el.querySelector(BLOCKS)) continue;
    if (el.closest('pre, figure:not(:has(figcaption)), .hidden')) continue;
    if (!el.textContent.trim()) continue;
    out.push(el);
  }
  return out;
}

// Splits a paragraph into pieces of a few sentences.
function chunks(text) {
  const clean = text.replace(/\s+/g, ' ').trim();
  if (clean.length <= MAX_CHUNK) return [clean];
  const sentences = clean.match(/[^.!?…;:]+[.!?…;:]*\s*/g) || [clean];
  const out = [];
  let cur = '';
  for (const s of sentences) {
    if (cur && (cur + s).length > MAX_CHUNK) { out.push(cur.trim()); cur = ''; }
    if (s.length > MAX_CHUNK * 2) {
      for (let i = 0; i < s.length; i += MAX_CHUNK) out.push(s.slice(i, i + MAX_CHUNK));
    } else cur += s;
  }
  if (cur.trim()) out.push(cur.trim());
  return out;
}

// ---- the voice ----

function pickVoice(lang) {
  const voices = speechSynthesis.getVoices().filter((v) => v.lang && v.lang.toLowerCase().startsWith(lang));
  if (!voices.length) return null;
  const score = (v) => (/(premium|enhanced|siri|natural|neural)/i.test(v.name) ? 4 : 0)
    + (v.localService ? 2 : 0) + (v.default ? 1 : 0);
  return voices.sort((a, b) => score(b) - score(a))[0];
}

function rate() {
  const r = Number(getPrefs().listen_rate);
  return RATES.includes(r) ? r : 1;
}

function rateLabel(r) {
  return new Intl.NumberFormat(document.documentElement.lang || undefined).format(r) + '×';
}

// ---- speaking ----

function speakCurrent() {
  const p = player;
  if (!p) return;
  const item = p.items[p.i];
  if (!item) { stopListening(); return; }
  p.pieces = p.pieces || chunks(item.textContent);
  const text = p.pieces[p.chunk];
  if (!text) { next(); return; }
  highlight();
  const u = new SpeechSynthesisUtterance(text);
  u.lang = p.lang;
  if (p.voice) u.voice = p.voice;
  u.rate = rate();
  const my = ++p.token;
  u.onend = () => {
    if (!player || player !== p || my !== p.token || !p.playing) return;
    p.chunk++;
    if (p.chunk < p.pieces.length) speakCurrent();
    else next();
  };
  u.onerror = (e) => {
    if (!player || player !== p || my !== p.token) return;
    if (e.error === 'interrupted' || e.error === 'canceled') return;
    setPlaying(false);
  };
  speechSynthesis.speak(u);
}

function next() {
  const p = player;
  p.i++;
  p.chunk = 0;
  p.pieces = null;
  if (p.i >= p.items.length) { stopListening(); return; }
  speakCurrent();
}

function jump(d) {
  const p = player;
  if (!p) return;
  const to = Math.min(Math.max(p.i + d, 0), p.items.length - 1);
  // Back at the start of a paragraph already begun: restart it first.
  p.i = d < 0 && p.chunk > 0 ? p.i : to;
  p.chunk = 0;
  p.pieces = null;
  p.token++;
  speechSynthesis.cancel();
  if (p.playing) speakCurrent(); else highlight();
  paint();
}

function setPlaying(on) {
  const p = player;
  if (!p) return;
  p.playing = on;
  p.token++;
  speechSynthesis.cancel();
  if (on) speakCurrent();
  paint();
}

function highlight() {
  const p = player;
  p.items.forEach((el, i) => el.classList.toggle('speaking', i === p.i));
  const el = p.items[p.i];
  if (!el || p.book()) return;
  // Keep the paragraph in view: a third from the top when it moves away.
  const sc = p.scroller.getBoundingClientRect();
  const r = el.getBoundingClientRect();
  if (r.top < sc.top + 80 || r.bottom > sc.bottom - 160) {
    motion.scrollTo(p.scroller, p.scroller.scrollTop + r.top - sc.top - sc.height / 3);
  }
}

function paint() {
  const p = player;
  if (!p) return;
  p.playBtn.replaceChildren(icon(p.playing ? 's-pause' : 's-play', 'w-6 h-6'));
  p.playBtn.setAttribute('aria-label', p.playing ? t('Pause') : t('Play'));
  p.prevBtn.disabled = p.i === 0 && p.chunk === 0;
  p.nextBtn.disabled = p.i >= p.items.length - 1;
  p.rateBtn.textContent = rateLabel(rate());
  p.where.textContent = t('{0} of {1}', p.i + 1, p.items.length);
}

// ---- the player ----

// startListening(pane, scroller, wrap, { book }) reads the open article.
export function startListening(pane, scroller, wrap, { book = () => false } = {}) {
  if (!supported()) return;
  if (player) { setPlaying(!player.playing); return; }
  const items = blocks(wrap);
  if (!items.length) return;
  const sample = items.slice(0, 12).map((e) => e.textContent).join(' ');
  const fallback = (document.documentElement.lang || 'it').slice(0, 2);
  const lang = guessLang(sample, fallback);

  const p = { pane, scroller, items, i: 0, chunk: 0, pieces: null, playing: true, token: 0, lang, voice: null, book };
  // Start from the paragraph at the top of the screen, if the reader has
  // already scrolled down.
  if (!book()) {
    const top = scroller.getBoundingClientRect().top + 60;
    const first = items.findIndex((el) => el.getBoundingClientRect().bottom > top);
    if (first > 0) p.i = first;
  }
  p.voice = pickVoice(lang);
  if (!p.voice && speechSynthesis.getVoices().length === 0) {
    // Voices load late in some browsers: pick one as soon as they arrive.
    speechSynthesis.addEventListener('voiceschanged', () => { if (player === p) p.voice = pickVoice(lang); }, { once: true });
  }

  p.prevBtn = h('button', { class: 'listen-btn', type: 'button', 'aria-label': t('Previous paragraph'), title: t('Previous paragraph'), onclick: () => jump(-1) }, icon('o-backward', 'w-5 h-5'));
  p.playBtn = h('button', { class: 'listen-btn listen-play', type: 'button', onclick: () => setPlaying(!p.playing) });
  p.nextBtn = h('button', { class: 'listen-btn', type: 'button', 'aria-label': t('Next paragraph'), title: t('Next paragraph'), onclick: () => jump(1) }, icon('o-forward', 'w-5 h-5'));
  p.rateBtn = h('button', {
    class: 'listen-rate', type: 'button', 'aria-label': t('Speed'), title: t('Speed'),
    onclick: () => {
      const r = RATES[(RATES.indexOf(rate()) + 1) % RATES.length];
      setPref('listen_rate', r);
      // The new speed starts from the sentence being read.
      if (p.playing) { p.token++; speechSynthesis.cancel(); speakCurrent(); }
      paint();
    },
  });
  p.where = h('div', { class: 'listen-where', 'aria-live': 'polite' });
  const close = h('button', { class: 'listen-btn', type: 'button', 'aria-label': t('Stop listening'), title: t('Stop listening'), onclick: () => stopListening() }, icon('o-x-mark', 'w-5 h-5'));
  p.el = h('div', { class: 'listen-bar glass', role: 'region', 'aria-label': t('Listen') },
    p.rateBtn, p.prevBtn, p.playBtn, p.nextBtn, p.where, close);
  pane.append(p.el);
  pane.classList.add('is-listening');
  motion.toastIn(p.el);
  player = p;
  paint();
  speechSynthesis.cancel();
  speakCurrent();
}

export function stopListening() {
  const p = player;
  if (!p) return;
  player = null;
  p.token++;
  try { speechSynthesis.cancel(); } catch (e) { /* ignore */ }
  p.items.forEach((el) => el.classList.remove('speaking'));
  p.pane.classList.remove('is-listening');
  motion.toastOut(p.el).then(() => p.el.remove());
}

// Back from the background: iOS stops the voice without telling. The
// player then shows the pause state, to go on from the same paragraph.
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState !== 'visible' || !player || !player.playing) return;
  if (!speechSynthesis.speaking) {
    player.playing = false;
    player.chunk = 0;
    player.pieces = null;
    paint();
  }
});
window.addEventListener('pagehide', () => stopListening());
