// =============================================================================
// api.js — работа с REST API сервера.
// Хранит JWT-токены в localStorage и сам обновляет токен, если он истёк.
//
// Пример:  const clients = await api('/api/clients/');
//          await api('/api/devices/', { method: 'POST', body: {...} });
// =============================================================================

const STORAGE_KEY = 'ata_auth';

// Хранилище токенов (access — короткий, refresh — длинный)
export const auth = {
  load() {
    try { return JSON.parse(localStorage.getItem(STORAGE_KEY)) || {}; } catch { return {}; }
  },
  get access() { return this.load().access; },
  get refresh() { return this.load().refresh; },
  save(patch) { localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...this.load(), ...patch })); },
  clear() { localStorage.removeItem(STORAGE_KEY); },
};

// Ошибка API: message — текст для человека, data — ответ сервера (ошибки по полям)
export class ApiError extends Error {
  constructor(message, status, data) {
    super(message);
    this.status = status;
    this.data = data;
  }
}

// Превращает ответ сервера с ошибкой в понятный текст
function errorText(data, status) {
  if (!data) return `Ошибка сервера (${status})`;
  if (typeof data === 'string') return data;
  if (data.detail) return data.detail;
  return Object.entries(data).map(([k, v]) => `${k}: ${[].concat(v).join(' ')}`).join('\n');
}

// Что делать, если вход "слетел" (токен не обновился) — назначает app.js
let authLostHandler = () => {};
export function setAuthLostHandler(fn) { authLostHandler = fn; }
function authLost() { auth.clear(); authLostHandler(); }

// Обновляет access-токен по refresh-токену (одновременно — только один запрос)
let refreshing = null;
export function refreshAccess() {
  if (!auth.refresh) return Promise.reject(new ApiError('Нет токена', 401));
  if (!refreshing) {
    refreshing = fetch('/api/auth/refresh/', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh: auth.refresh }),
    })
      .then(async (r) => {
        if (!r.ok) throw new ApiError('Сессия истекла, войдите снова', 401);
        const data = await r.json();
        auth.save({ access: data.access });
        return data.access;
      })
      .finally(() => { refreshing = null; });
  }
  return refreshing;
}

// Главная функция запросов к API
export async function api(path, { method = 'GET', body, params } = {}, retry = true) {
  let url = path;
  if (params) {
    const q = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => { if (v !== '' && v != null) q.append(k, v); });
    if (q.toString()) url += '?' + q;
  }
  const headers = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (auth.access) headers.Authorization = 'Bearer ' + auth.access;

  const res = await fetch(url, { method, headers, body: body !== undefined ? JSON.stringify(body) : undefined });

  // Токен истёк -> обновляем и повторяем запрос один раз
  if (res.status === 401 && retry && auth.refresh) {
    try { await refreshAccess(); } catch (e) { authLost(); throw e; }
    return api(path, { method, body, params }, false);
  }
  if (res.status === 401) { authLost(); }
  if (res.status === 204) return null;

  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(errorText(data, res.status), res.status, data);
  return data;
}

// Вход: сохраняет токены и возвращает данные пользователя
export async function login(username, password) {
  const res = await fetch('/api/auth/login/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError('Неверный логин или пароль', res.status, data);
  auth.save({ access: data.access, refresh: data.refresh });
  return data.user;
}
