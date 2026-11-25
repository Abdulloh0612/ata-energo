// Hybrid Doctor: service worker. Данные всегда берутся с сервера (ничего устаревшего),
// офлайн показывается понятная страница вместо ошибки браузера.
const CACHE = 'ata-hd-shell-v1';
const SHELL = ['/hybrid-doctor/static/style.css?v=1.0', '/hybrid-doctor/static/app.js?v=1.0', '/hybrid-doctor/static/icons/icon-192.png', '/hybrid-doctor/static/offline.html'];
self.addEventListener('install', e => {
  e.waitUntil(caches.open(CACHE).then(c => c.addAll(SHELL)).then(() => self.skipWaiting()));
});
self.addEventListener('activate', e => {
  e.waitUntil(caches.keys().then(ks => Promise.all(ks.filter(k => k.startsWith('ata-hd-shell-') && k !== CACHE).map(k => caches.delete(k))))
    .then(() => self.clients.claim()));
});
self.addEventListener('fetch', e => {
  const r = e.request;
  if (r.method !== 'GET') return;
  const url = new URL(r.url);
  if (url.origin !== location.origin) return;
  if (url.pathname.startsWith('/hybrid-doctor/static/')) {
    e.respondWith(caches.match(r).then(hit => hit || fetch(r)));
    return;
  }
  if (r.mode === 'navigate') {
    e.respondWith(fetch(r).catch(() => caches.match('/hybrid-doctor/static/offline.html')));
  }
});
