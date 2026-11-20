// Minimal promise wrapper around IndexedDB. One database per user, so a
// shared device never mixes the data of two accounts.

const VERSION = 1;
let db = null;
let dbName = '';

function req(r) {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

function done(tx) {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onabort = tx.onerror = () => reject(tx.error);
  });
}

export function nameFor(userId) { return `reader-u${userId}`; }

export async function open(userId) {
  const name = nameFor(userId);
  if (db && dbName === name) return db;
  close();
  const r = indexedDB.open(name, VERSION);
  r.onupgradeneeded = () => {
    const d = r.result;
    if (!d.objectStoreNames.contains('articles')) {
      const s = d.createObjectStore('articles', { keyPath: 'id' });
      s.createIndex('published_at', 'published_at');
    }
    if (!d.objectStoreNames.contains('queue')) d.createObjectStore('queue', { keyPath: 'seq', autoIncrement: true });
    if (!d.objectStoreNames.contains('meta')) d.createObjectStore('meta');
  };
  db = await req(r);
  dbName = name;
  db.onversionchange = () => close();
  return db;
}

export function isOpen() { return !!db; }

export function close() {
  if (db) db.close();
  db = null;
  dbName = '';
}

export async function destroy(userId) {
  if (dbName === nameFor(userId)) close();
  await req(indexedDB.deleteDatabase(nameFor(userId))).catch(() => {});
}

function store(name, mode = 'readonly') {
  if (!db) throw new Error('offline database not open');
  const tx = db.transaction(name, mode);
  return { tx, s: tx.objectStore(name) };
}

// ---- generic helpers ----

export async function get(name, key) {
  const { s } = store(name);
  return req(s.get(key));
}

export async function getAll(name) {
  const { s } = store(name);
  return req(s.getAll());
}

export async function getAllKeys(name) {
  const { s } = store(name);
  return req(s.getAllKeys());
}

export async function count(name) {
  const { s } = store(name);
  return req(s.count());
}

export async function put(name, value, key) {
  const { tx, s } = store(name, 'readwrite');
  s.put(value, key);
  return done(tx);
}

export async function putMany(name, values) {
  if (!values.length) return;
  const { tx, s } = store(name, 'readwrite');
  values.forEach((v) => s.put(v));
  return done(tx);
}

export async function del(name, key) {
  const { tx, s } = store(name, 'readwrite');
  s.delete(key);
  return done(tx);
}

export async function delMany(name, keys) {
  if (!keys.length) return;
  const { tx, s } = store(name, 'readwrite');
  keys.forEach((k) => s.delete(k));
  return done(tx);
}

export async function clear(name) {
  const { tx, s } = store(name, 'readwrite');
  s.clear();
  return done(tx);
}

// update(name, key, fn): read-modify-write in one transaction.
export async function update(name, key, fn) {
  const { tx, s } = store(name, 'readwrite');
  const cur = await req(s.get(key));
  if (cur !== undefined) {
    const next = fn(cur);
    if (next) s.put(next);
  }
  return done(tx);
}
