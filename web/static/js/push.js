// Web Push subscription for this device, and the app icon badge.

import { t } from './i18n.js';
import { api, get, post } from './api.js';

export function isStandalone() {
  return window.matchMedia('(display-mode: standalone)').matches || navigator.standalone === true;
}

export function isIOS() {
  return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
}

export function supported() {
  return 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window && window.isSecureContext;
}

// Why push cannot be enabled here, or '' when it can.
export function unavailableReason() {
  if (!window.isSecureContext) return t('Notifications need HTTPS: open the app from its public address.');
  if (isIOS() && !isStandalone()) return t('On iPhone and iPad notifications only work in the installed app: Share → Add to Home Screen, then open it from the icon.');
  if (!supported()) return t('This browser doesn\'t support push notifications.');
  if (Notification.permission === 'denied') return t('Notifications are blocked. Turn them back on in the system settings for this app.');
  return '';
}

function keyBytes(b64) {
  const s = b64.replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(s + '='.repeat((4 - (s.length % 4)) % 4));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

function deviceName() {
  const ua = navigator.userAgent;
  let name = 'Browser';
  if (/iPad/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)) name = 'iPad';
  else if (/iPhone/.test(ua)) name = 'iPhone';
  else if (/Android/.test(ua)) name = 'Android';
  else if (/Macintosh/.test(ua)) name = 'Mac';
  else if (/Windows/.test(ua)) name = 'Windows';
  else if (/Linux/.test(ua)) name = 'Linux';
  if (isStandalone()) name += ', app';
  else if (/Firefox\//.test(ua)) name += ', Firefox';
  else if (/Edg\//.test(ua)) name += ', Edge';
  else if (/Chrome\//.test(ua)) name += ', Chrome';
  else if (/Safari\//.test(ua)) name += ', Safari';
  return name;
}

export async function currentSubscription() {
  if (!supported()) return null;
  const reg = await navigator.serviceWorker.ready;
  return reg.pushManager.getSubscription();
}

async function register(sub) {
  const json = sub.toJSON();
  await post('/api/push/subscriptions', { endpoint: json.endpoint, keys: json.keys, device: deviceName() });
}

// enable must be called directly from a tap: iOS only shows the permission
// prompt during a user gesture, so nothing is awaited before it.
export async function enable(publicKey) {
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') throw new Error(t('Notification permission not granted'));
  const reg = await navigator.serviceWorker.ready;
  let sub = await reg.pushManager.getSubscription();
  if (!sub) {
    sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) });
  }
  await register(sub);
  return sub;
}

export async function disable() {
  const sub = await currentSubscription().catch(() => null);
  if (!sub) return;
  await post('/api/push/unsubscribe', { endpoint: sub.endpoint }).catch(() => {});
  await sub.unsubscribe().catch(() => {});
}

// Called at every start: keeps the server copy of this device's
// subscription in sync (iOS can rotate or drop subscriptions).
export async function refresh() {
  if (!supported() || Notification.permission !== 'granted') return;
  try {
    const sub = await currentSubscription();
    if (sub) {
      await register(sub);
      return;
    }
    const info = await get('/api/push');
    const reg = await navigator.serviceWorker.ready;
    const fresh = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(info.public_key) });
    await register(fresh);
  } catch (e) { /* best effort */ }
}

export async function info() {
  const sub = await currentSubscription().catch(() => null);
  const q = sub ? '?endpoint=' + encodeURIComponent(sub.endpoint) : '';
  const data = await api('GET', '/api/push' + q);
  return { ...data, subscribed: !!sub };
}

// ---- badge ----

let lastBadge = -1;

export function setBadge(count) {
  if (!('setAppBadge' in navigator) || count === lastBadge) return;
  lastBadge = count;
  const p = count > 0 ? navigator.setAppBadge(count) : navigator.clearAppBadge();
  p.catch(() => {});
}
