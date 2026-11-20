// Thin wrapper around fetch for the JSON API: CSRF header, errors, 401,
// timeouts and the hooks used by the offline layer.

import { lang, t } from './i18n.js';

let csrfToken = '';
let onUnauthorized = () => {};
let hooks = null;

export function setCsrf(token) { csrfToken = token || ''; }
export function setUnauthorizedHandler(fn) { onUnauthorized = fn; }

// hooks: {
//   intercept(method, path, body) -> Promise | null   handle before the network
//   fallback(method, path, body) -> Promise | null    answer when the network fails
//   success(method, path, body, data)                 observe successful calls
//   status(online: boolean)                            network reachability
// }
export function setOfflineHooks(h) { hooks = h; }

export class ApiError extends Error {
  constructor(status, message, data) {
    super(message);
    this.status = status;
    this.data = data;
  }
}

// True when the server could not be reached: no connection, timeout, or an
// error page from the tunnel/proxy instead of a JSON answer from the app.
export function isNetworkError(e) {
  if (!(e instanceof ApiError)) return false;
  if (e.status === 0) return true;
  return e.status >= 502 && e.status <= 530 && !(e.data && e.data.error);
}

async function request(method, path, body, opts) {
  const headers = {};
  let payload;
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }
  if (method !== 'GET') headers['X-CSRF-Token'] = csrfToken;
  headers['X-Lang'] = lang; // messages from the server in the same language

  const ctrl = new AbortController();
  const timeout = opts.timeout ?? (method === 'GET' ? 15000 : 60000);
  const timer = setTimeout(() => ctrl.abort(new DOMException('timeout', 'TimeoutError')), timeout);
  if (opts.signal) opts.signal.addEventListener('abort', () => ctrl.abort(opts.signal.reason), { once: true });

  let res;
  try {
    res = await fetch(path, { method, headers, body: payload, credentials: 'same-origin', signal: ctrl.signal });
  } catch (e) {
    if (opts.signal && opts.signal.aborted) throw e;
    throw new ApiError(0, t('Connection unavailable'));
  } finally {
    clearTimeout(timer);
  }
  let data = null;
  if ((res.headers.get('content-type') || '').includes('json')) {
    data = await res.json().catch(() => null);
  }
  if (!res.ok) {
    if (res.status === 401 && path !== '/api/login' && path !== '/api/me') onUnauthorized();
    throw new ApiError(res.status, (data && data.error) || t('Error {0}', res.status), data);
  }
  return data;
}

// api(method, path, body, {signal, timeout, raw}) - raw skips the offline hooks.
export async function api(method, path, body, opts = {}) {
  const useHooks = hooks && !opts.raw;
  if (useHooks && hooks.intercept) {
    const r = hooks.intercept(method, path, body);
    if (r) return r;
  }
  let data;
  try {
    data = await request(method, path, body, opts);
  } catch (e) {
    if (isNetworkError(e)) {
      if (hooks) hooks.status(false);
      if (useHooks && hooks.fallback) {
        const r = hooks.fallback(method, path, body);
        if (r) return r;
      }
    }
    throw e;
  }
  if (hooks) {
    hooks.status(true);
    if (!opts.raw && hooks.success) hooks.success(method, path, body, data);
  }
  return data;
}

export const get = (path, opts) => api('GET', path, undefined, opts);
export const post = (path, body, opts) => api('POST', path, body === undefined ? {} : body, opts);
export const put = (path, body) => api('PUT', path, body);
export const patch = (path, body) => api('PATCH', path, body);
export const del = (path) => api('DELETE', path);
