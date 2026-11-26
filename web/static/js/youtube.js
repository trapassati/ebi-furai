// YouTube videos: recognize them from the article link, and the player
// that loads from youtube-nocookie.com only when the video is tapped (until
// then YouTube is not contacted: the thumbnail comes through the server).

import { t } from './i18n.js';
import { h, icon } from './ui.js';
import * as motion from './motion.js';

const HOSTS = new Set(['youtube.com', 'www.youtube.com', 'm.youtube.com', 'music.youtube.com']);
const ID = /^[0-9A-Za-z_-]{11}$/;

// videoOf('https://www.youtube.com/watch?v=…') -> { id, short } or null.
export function videoOf(link) {
  if (!link) return null;
  let u;
  try { u = new URL(link); } catch (e) { return null; }
  const host = u.hostname.toLowerCase();
  let id = '', short = false;
  if (host === 'youtu.be') id = u.pathname.slice(1);
  else if (HOSTS.has(host)) {
    const [first, second] = u.pathname.split('/').filter(Boolean);
    if (first === 'watch') id = u.searchParams.get('v') || '';
    else if (first === 'shorts' || first === 'live') { id = second || ''; short = first === 'shorts'; }
  }
  return ID.test(id) ? { id, short } : null;
}

// A feed made by YouTube (its options include hiding the Shorts).
export function isYouTubeFeed(feed) {
  return !!feed && /^https:\/\/www\.youtube\.com\/feeds\/videos\.xml\?/.test(feed.url || '');
}

// The big thumbnail of a video in the list, with the play sign.
export function listThumb(a, video) {
  const img = h('img', { src: a.thumbnail, alt: '', loading: 'lazy', decoding: 'async' });
  const box = h('div', { class: 'vthumb' + (video.short ? ' short' : '') },
    img,
    h('span', { class: 'vthumb-play', 'aria-hidden': 'true' }, icon('s-play')),
    video.short ? h('span', { class: 'vthumb-tag', text: 'Shorts' }) : null);
  img.addEventListener('error', () => box.remove(), { once: true });
  return box;
}

// The player at the top of the article: the thumbnail with a big play
// button, replaced by YouTube's player on tap, and "Watch on YouTube".
export function playerBlock(a, video) {
  const frame = h('div', { class: 'yt-frame' + (video.short ? ' short' : '') });
  const play = h('button', { class: 'yt-poster', 'aria-label': t('Play the video') },
    a.thumbnail ? h('img', { src: a.thumbnail, alt: '' }) : null,
    h('span', { class: 'yt-play' }, icon('s-play')));
  const thumb = play.querySelector('img');
  if (thumb) thumb.addEventListener('error', () => thumb.remove(), { once: true });
  play.addEventListener('click', () => {
    const src = `https://www.youtube-nocookie.com/embed/${video.id}?autoplay=1&playsinline=1&rel=0`;
    const iframe = h('iframe', {
      src, title: a.title,
      allow: 'autoplay; encrypted-media; picture-in-picture; fullscreen',
      allowfullscreen: true,
      // The app sends no referrer, but YouTube's player refuses to start
      // without one: it gets the origin only.
      referrerpolicy: 'strict-origin-when-cross-origin',
    });
    frame.prepend(iframe);
    // The poster stays over the player while it loads, then fades away.
    play.disabled = true;
    const gone = () => play.remove();
    iframe.addEventListener('load', () => {
      if (motion.enabled()) window.gsap.to(play, { opacity: 0, scale: 1.03, duration: 0.35, ease: 'power2.out', onComplete: gone });
      else gone();
    }, { once: true });
    setTimeout(gone, 6000);
    if (motion.enabled()) motion.pop(play.querySelector('.yt-play'));
  }, { once: true });
  frame.append(play);
  const open = h('a', { class: 'btn btn-plain yt-open', href: a.url, target: '_blank', rel: 'noopener noreferrer' },
    icon('o-arrow-top-right-on-square', 'w-4 h-4'), t('Watch on YouTube'));
  return h('div', { class: 'yt-block' }, frame, open);
}

// The feed's content starts with the same thumbnail linked to the video
// (for Reeder and the other apps): here the player takes its place.
export function dropLeadingThumb(body, video) {
  const first = body.firstElementChild;
  if (!first || first.textContent.trim() !== '') return;
  const link = first.querySelector('a[href]');
  const v = link && videoOf(link.getAttribute('href'));
  if (v && v.id === video.id && first.querySelector('img')) first.remove();
}
