// =============================================================================
// app.js — точка входа сайта.
// 1) Вход (логин/пароль -> JWT)  2) Меню по роли  3) Переходы между страницами
// 4) WebSocket: живые данные -> store -> страницы
//
// Адреса страниц (после #):
//   #/dashboard      обзор (сотрудники) или "Моя станция" (клиент)
//   #/client/5       станция клиента №5
//   #/clients        клиенты
//   #/modems         модемы инверторов
//   #/devices        оборудование
//   #/discovery      поиск инверторов (admin, it_head)
//   #/employees      сотрудники (admin, it_head)
//   #/alerts         аварии
// =============================================================================
import { api, auth, login, setAuthLostHandler } from './api.js';
import { store } from './store.js';
import { esc, ROLE_LABELS, toast } from './ui.js';
import { live } from './ws.js';
import * as alertsPage from './pages/alerts.js';
import * as clientsPage from './pages/clients.js';
import * as devicesPage from './pages/devices.js';
import * as discoveryPage from './pages/discovery.js';
import * as employeesPage from './pages/employees.js';
import * as modemsPage from './pages/modems.js';
import * as overviewPage from './pages/overview.js';
import * as stationPage from './pages/station.js';

const $ = (id) => document.getElementById(id);

// ---------- Иконки меню ----------
const ICON = {
  home: '<path d="M3 11l9-7 9 7"/><path d="M5 10v10h14V10"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  users: '<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7M18 14c2.2.6 3.5 2.6 3.5 6"/>',
  modem: '<rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 16.5h.01M11 16.5h.01M12 13V9"/><path d="M8.5 7.5a5 5 0 0 1 7 0M6 5a8.5 8.5 0 0 1 12 0"/>',
  radar: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><path d="M12 12l6-6"/><circle cx="12" cy="12" r="1"/>',
  chip: '<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>',
  badge: '<rect x="4" y="3" width="16" height="18" rx="2"/><circle cx="12" cy="10" r="3"/><path d="M8 17c.8-1.8 2.2-2.6 4-2.6s3.2.8 4 2.6"/>',
  bell: '<path d="M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10 21a2 2 0 0 0 4 0"/>',
};

// Пункты меню для каждой роли
function menuFor(me, access) {
  const items = [];
  if (me.can_view_all) {
    items.push(['#/dashboard', 'Обзор', ICON.home]);
    items.push(['#/clients', 'Клиенты', ICON.users]);
    items.push(['#/modems', 'Модемы', ICON.modem]);
    items.push(['#/devices', 'Оборудование', ICON.chip]);
    if (me.can_edit) items.push(['#/discovery', 'Поиск инверторов', ICON.radar]);
    if (me.can_edit) items.push(['#/employees', 'Сотрудники', ICON.badge]);
  } else {
    items.push(['#/dashboard', 'Моя станция', ICON.sun]);
  }
  if (renderCRM && me.can_view_all) {
    items.push(['#/sites', 'Объекты', ICON.sun]);
    if (access.requests?.view) items.push(['#/requests', 'Заявки', ICON.badge]);
    if (access.tasks?.view) items.push(['#/tasks', 'Задачи', ICON.chip]);
    if (me.role === 'admin') {
      items.push(['#/roles', 'Роли и права', ICON.badge]);
      items.push(['#/activity', 'Журнал действий', ICON.radar]);
      items.push(['#/settings', 'Настройки', ICON.chip]);
      items.push(['#/integrations', 'Интеграции', ICON.modem]);
    }
  }
  items.push(['#/alerts', 'Аварии', ICON.bell]);
  return items;
}

// ---------- Маршруты ----------
// staff: только для admin/it_head/dispatcher; manager: только для admin/it_head
const ROUTES = [
  { re: /^\/dashboard$/, title: (me) => (me.can_view_all ? 'Обзор' : 'Моя станция'),
    page: (me) => (me.can_view_all ? overviewPage : stationPage) },
  { re: /^\/client\/(\d+)$/, title: () => 'Станция', page: () => stationPage, staff: true },
  { re: /^\/clients$/, title: () => 'Клиенты', page: () => clientsPage, staff: true },
  { re: /^\/modems$/, title: () => 'Модемы', page: () => modemsPage, staff: true },
  { re: /^\/devices$/, title: () => 'Оборудование', page: () => devicesPage, staff: true },
  { re: /^\/discovery$/, title: () => 'Поиск инверторов', page: () => discoveryPage, manager: true },
  { re: /^\/employees$/, title: () => 'Сотрудники', page: () => employeesPage, manager: true },
  { re: /^\/alerts$/, title: () => 'Аварии', page: () => alertsPage },
];

/** @type {(view: HTMLElement) => Promise<void | (() => void)>} */
let renderOverview = overviewPage.render;
/** @type {((view: HTMLElement, path: string) => Promise<void | (() => void)>) | undefined} */
let renderCRM;
let cleanup = null;   // функция "уборки" текущей страницы (отписки, таймеры)
let routeToken = 0;

async function route() {
  const me = store.me;
  if (!me) return;
  const [path, qs] = (location.hash.slice(1) || '/dashboard').split('?');
  const crmRoute = renderCRM && /^\/(?:requests(?:\/(?:\d+|new))?|tasks(?:\/(?:\d+|new))?|sites(?:\/[sm]\d+)?|integrations(?:\/(?:\d+|new))?|activity|roles|settings|profile|equipment\/\d+)$/.test(path);
  const r = ROUTES.find((x) => x.re.test(path));
  if (!crmRoute && (!r || (r.staff && !me.can_view_all) || (r.manager && !me.can_edit))) {
    location.hash = '#/dashboard';
    return;
  }
  const match = r ? path.match(r.re) : [];

  if (cleanup) { try { cleanup(); } catch { /* ничего */ } cleanup = null; }
  const token = ++routeToken;
  const view = $('view');
  view.innerHTML = crmRoute
    ? '<div class="crm-skeleton" role="status" aria-label="Загрузка"><i></i><i></i><i></i></div>'
    : '<div class="loading">Загрузка…</div>';
  const crmTitle = { sites: 'Объекты', integrations: 'Интеграции', requests: 'Заявки', tasks: 'Задачи', activity: 'Журнал действий', roles: 'Роли и права', settings: 'Настройки', profile: 'Профиль', equipment: 'Оборудование' };
  setTitle(crmRoute ? crmTitle[path.split('/')[1]] : r.title(me));
  document.querySelectorAll('#nav a').forEach((a) => a.classList.toggle('active', a.getAttribute('href') === '#' + path
    || (path.startsWith('/client/') && a.getAttribute('href') === '#/dashboard')));
  $('app').classList.remove('menu-open');
  window.scrollTo(0, 0);

  try {
    const page = r?.page(me);
    const renderPage = page === overviewPage ? renderOverview : page?.render;
    const pageRoot = document.createElement('div');
    pageRoot.className = 'page-content';
    if (crmRoute) view.append(pageRoot); else view.replaceChildren(pageRoot);
    if (crmRoute && !me.can_view_all && path !== '/profile') {
      view.querySelector('.crm-skeleton')?.remove();
      pageRoot.innerHTML = '<p class="card empty">Нет доступа</p>';
      return;
    }
    const result = crmRoute ? await renderCRM(pageRoot, path) : await renderPage(pageRoot, {
      clientId: match[1] ? Number(match[1]) : me.client_id,
      query: new URLSearchParams(qs || ''),
      setTitle,
      refreshBell,
    });
    if (token !== routeToken) { result?.(); return; } // пока грузили — ушли на другую страницу
    view.querySelector('.crm-skeleton')?.remove();
    cleanup = result;
  } catch (err) {
    if (token !== routeToken) return;
    view.innerHTML = `<div class="card empty">${err.status === 403 ? "Нет доступа" : "Не удалось загрузить: " + esc(err.message)}</div>`;
  }
}

function setTitle(text) {
  $('page-title').textContent = text;
  document.title = `${text} — Ата Энерго`;
}

// ---------- Колокольчик (непрочитанные аварии) ----------
function drawBell() {
  const n = store.unreadAlerts;
  $('bell-count').textContent = n > 99 ? '99+' : n;
  $('bell-count').classList.toggle('hidden', !n);
  const navCount = document.querySelector('#nav a[href="#/alerts"] .count');
  if (navCount) { navCount.textContent = n; navCount.classList.toggle('hidden', !n); }
}
async function refreshBell() {
  try { store.unreadAlerts = (await api('/api/alerts/', { params: { unread: 1 } })).length; } catch { /* ничего */ }
  drawBell();
  store.notify({ type: 'alertCount' });
}

// ---------- Запуск приложения после входа ----------
async function startApp() {
  const me = store.me;

  const access = renderCRM && me.can_view_all ? await api('/api/crm/access') : {};
  if (store.me !== me) return;
  store.crmAccess = access;
  $('nav').innerHTML = menuFor(me, access).map(([href, label, icon]) =>
    `<a href="${href}" title="${label}"><svg viewBox="0 0 24 24" aria-hidden="true">${icon}</svg><span class="nav-label">${label}</span>${href === '#/alerts' ? '<span class="count hidden"></span>' : ''}</a>`).join('');
  const name = `${me.first_name} ${me.last_name}`.trim() || me.username;
  $('user-box').innerHTML = `<span class="avatar">${esc(name[0].toUpperCase())}</span>
    <div><b>${renderCRM ? `<a href="#/profile">${esc(name)}</a>` : esc(name)}</b><small>${esc(ROLE_LABELS[me.role] || me.role)}</small></div>`;

  // Устройства: сотрудники видят все, клиент — только свои
  const [devices, modems] = await Promise.all([
    api(me.can_view_all ? '/api/devices/' : '/api/my/devices/'),
    api('/api/modems/'),
  ]);
  store.setDevices(devices);
  store.setModems(modems);
  await refreshBell();
  if (store.me !== me) return;
  $('app').dataset.audience = me.can_view_all ? 'staff' : 'client';
  $('login-screen').classList.add('hidden');
  $('app').classList.remove('hidden');
  live.start();
  if (!location.hash) history.replaceState(null, '', '#/dashboard');
  void route();
}

async function boot() {
  if (!auth.access) { showLogin(); return; }
  try {
    store.me = await api('/api/me/');
    await startApp();
  } catch {
    showLogin();
  }
}

function showLogin() {
  ++routeToken;
  live.stop();
  if (cleanup) { cleanup(); cleanup = null; }
  store.me = null;
  $('app').classList.add('hidden');
  $('login-screen').classList.remove('hidden');
  $('login-form').password.value = '';
}

function logout() {
  auth.clear();
  location.hash = '';
  showLogin();
}

// ---------- WebSocket -> store ----------
live.on('telemetry', (msg) => store.applyTelemetry(msg));
live.on('status', (msg) => store.applyStatus(msg));
live.on('modem', (msg) => store.applyModem(msg));
live.on('alert', (msg) => {
  store.unreadAlerts += 1;
  drawBell();
  toast(msg.alert.text, 'error');
  store.notify({ type: 'alert', alert: msg.alert });
});
live.on('state', (state) => {
  const el = $('ws-status');
  el.className = 'live ' + (state === 'online' ? 'on' : state === 'offline' ? 'off' : '');
  el.querySelector('span').textContent = state === 'online' ? 'Онлайн' : state === 'offline' ? 'Нет связи' : 'Подключение…';
});
live.on('authLost', logout);
setAuthLostHandler(() => { if (store.me) { toast('Сессия истекла, войдите снова', 'error'); showLogin(); } });

// ---------- Кнопки и формы ----------
/** @param {typeof renderOverview} [overview]
 * @param {typeof renderCRM} [crm] */
export function initialize(overview = overviewPage.render, crm) {
  renderCRM = crm;
  document.documentElement.classList.toggle('crm-enabled', Boolean(crm));
  renderOverview = overview;
  $('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const form = e.target;
    const btn = form.querySelector('[type=submit]');
    $('login-error').textContent = '';
    btn.disabled = true;
    try {
      store.me = await login(form.username.value.trim(), form.password.value);
      await startApp();
    } catch (err) {
      auth.clear();
      showLogin();
      $('login-error').textContent = err.message;
    } finally {
      btn.disabled = false;
    }
  });
  document.querySelectorAll('[data-demo]').forEach((b) => b.addEventListener('click', () => {
    const [u, p] = b.dataset.demo.split(':');
    $('login-form').username.value = u;
    $('login-form').password.value = p;
    $('login-form').requestSubmit();
  }));
  $('logout-btn').onclick = logout;
  const collapse = $('sidebar-collapse');
  if (collapse) collapse.onclick = () => {
    const collapsed = $('app').classList.toggle('sidebar-collapsed');
    const label = collapsed ? 'Развернуть меню' : 'Свернуть меню';
    collapse.setAttribute('aria-expanded', String(!collapsed));
    collapse.setAttribute('aria-label', label);
    collapse.title = label;
  };
  $('burger').onclick = () => $('app').classList.toggle('menu-open');
  $('overlay').onclick = () => $('app').classList.remove('menu-open');
  const themeSelect = $('theme-select');
  const applyTheme = (theme) => {
    document.documentElement.dataset.theme = theme;
    const background = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim();
    document.querySelector('meta[name="theme-color"]').content = background;
  };
  themeSelect.value = document.documentElement.dataset.theme;
  themeSelect.value ||= 'light';
  applyTheme(themeSelect.value);
  themeSelect.onchange = () => {
    applyTheme(themeSelect.value);
    try { localStorage.setItem('ata_theme', themeSelect.value); } catch { /* ничего */ }
  };
  window.addEventListener('hashchange', route);

  boot();
}
