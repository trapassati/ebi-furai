// YouTube videos: recognize them from the article link, and the player
// from youtube-nocookie.com at the top of the article.

import { t } from './i18n.js';
import { h, icon } from './ui.js';

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

// The player at the top of the article, YouTube's own (one tap on its
// play button starts the video), and "Watch on YouTube". Offline it is the
// thumbnail only.
const ORIGIN = 'https://www.youtube-nocookie.com';

export function playerBlock(a, video) {
  const frame = h('div', { class: 'yt-frame' + (video.short ? ' short' : '') });
  if (navigator.onLine === false) {
    frame.append(h('div', { class: 'yt-poster' },
      a.thumbnail ? h('img', { src: a.thumbnail, alt: '' }) : null,
      h('span', { class: 'yt-offline', text: t('The video plays when you are online') })));
  } else {
    // enablejsapi: the app can pause it (leaving the article, the app in
    // the background).
    frame.append(h('iframe', {
      src: `${ORIGIN}/embed/${video.id}?playsinline=1&rel=0&enablejsapi=1`, title: a.title,
      allow: 'autoplay; encrypted-media; picture-in-picture; fullscreen',
      allowfullscreen: true,
      // The app sends no referrer, but YouTube's player refuses to start
      // without one: it gets the origin only.
      referrerpolicy: 'strict-origin-when-cross-origin',
    }));
  }
  const open = h('a', { class: 'btn btn-plain yt-open', href: a.url, target: '_blank', rel: 'noopener noreferrer' },
    icon('o-arrow-top-right-on-square', 'w-4 h-4'), t('Watch on YouTube'));
  return h('div', { class: 'yt-block' }, frame, open);
}

// pauseVideos pauses the YouTube players inside root (the position stays).
export function pauseVideos(root) {
  for (const f of root.querySelectorAll('.yt-frame iframe')) {
    try {
      f.contentWindow.postMessage(JSON.stringify({ event: 'command', func: 'pauseVideo', args: [] }), ORIGIN);
    } catch (e) { /* not loaded yet */ }
  }
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
