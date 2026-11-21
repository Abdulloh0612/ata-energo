// =============================================================================
// Страница "Обзор" для admin / it_head / dispatcher:
// общие цифры по всем станциям + карточки всех клиентов (обновляются вживую).
// =============================================================================
import { api } from '../api.js';
import { summarize } from '../flow.js';
import { store } from '../store.js';
import { esc, fmtPower, rafThrottle } from '../ui.js';

export async function render(view) {
  view.innerHTML = `
    <div class="kpis">
      <div class="kpi" style="--c:var(--accent)"><small>Клиенты</small><b data-k="clients">—</b><span data-k="clientsSub"></span></div>
      <div class="kpi" style="--c:var(--ok)"><small>Устройства в сети</small><b data-k="online">—</b><span data-k="onlineSub"></span></div>
      <div class="kpi" style="--c:var(--pv)"><small>Солнце сейчас</small><b data-k="pv">—</b><span>все станции</span></div>
      <div class="kpi" style="--c:var(--load)"><small>Потребление</small><b data-k="load">—</b><span>все станции</span></div>
      <div class="kpi" style="--c:var(--danger)"><small>Новые аварии</small><b data-k="alerts">—</b><span><a href="#/alerts">открыть</a></span></div>
    </div>
    <div class="toolbar">
      <input type="search" id="ov-search" placeholder="Поиск клиента…">
      <select id="ov-filter">
        <option value="">Все станции</option>
        <option value="problem">Только с проблемами</option>
      </select>
    </div>
    <div class="client-grid" id="ov-grid"><div class="loading">Загрузка…</div></div>`;

  const kpi = (k) => view.querySelector(`[data-k="${k}"]`);
  const grid = view.querySelector('#ov-grid');
  const clients = await api('/api/clients/');

  if (!clients.length) {
    grid.innerHTML = '<div class="empty">Клиентов пока нет</div>';
  } else {
    grid.innerHTML = clients.map((c) => `
      <a class="ccard" href="#/client/${c.id}" data-id="${c.id}" data-name="${esc((c.name + ' ' + c.address + ' ' + c.phone).toLowerCase())}">
        <div class="ccard-top">
          <div><b>${esc(c.name)}</b><small>${esc(c.address || 'адрес не указан')}</small></div>
          <span class="badge" data-f="status">—</span>
        </div>
        <div class="mini">
          <div class="c-pv">Солнце<b data-f="pv">—</b></div>
          <div class="c-load">Нагрузка<b data-f="load">—</b></div>
          <div class="c-bat">Батарея<b data-f="bat">—</b></div>
          <div class="c-grid">Сеть<b data-f="grid">—</b></div>
        </div>
        <div class="bar"><i data-f="batbar"></i></div>
      </a>`).join('');
  }

  // Ссылки на элементы карточек (чтобы не искать их при каждом обновлении)
  const cards = new Map();
  grid.querySelectorAll('.ccard').forEach((el) => {
    const f = {};
    el.querySelectorAll('[data-f]').forEach((x) => (f[x.dataset.f] = x));
    cards.set(Number(el.dataset.id), { el, f, problem: false });
  });

  const set = (el, text) => { if (el.textContent !== text) el.textContent = text; };

  // Обновить одну карточку клиента
  function updateCard(id) {
    const card = cards.get(id);
    if (!card) return;
    const s = summarize(store.devicesOfClient(id));
    const { f } = card;
    set(f.pv, fmtPower(s.pv.power));
    set(f.load, fmtPower(s.load.power));
    set(f.bat, s.bat.percent != null ? Math.round(s.bat.percent) + '%' : '—');
    const gridDown = s.grid.online && (s.grid.error || !s.grid.voltage);
    set(f.grid, !s.grid.online ? '—' : gridDown ? 'нет сети' : fmtPower(s.grid.power, true));
    f.batbar.style.width = (s.bat.percent ?? 0) + '%';
    const problem = s.total > 0 && (s.online < s.total || gridDown || !!s.bat.error || !!s.inv.error);
    card.problem = problem;
    card.el.classList.toggle('problem', problem);
    f.status.className = 'badge ' + (s.total === 0 ? 'offline' : s.online === s.total ? (problem ? 'warn' : 'online') : s.online ? 'warn' : 'danger');
    set(f.status, `${s.online}/${s.total} в сети`);
  }

  // Общие цифры сверху
  function updateKpis() {
    let pv = 0, load = 0, online = 0;
    for (const d of store.devices.values()) {
      if (d.status !== 'online') continue;
      online++;
      const p = Number(d.last_data?.power) || 0;
      if (d.device_type === 'panel') pv += p;
      if (d.device_type === 'load') load += p;
    }
    set(kpi('clients'), String(clients.length));
    set(kpi('clientsSub'), `${[...cards.values()].filter((c) => c.problem).length} с проблемами`);
    set(kpi('online'), `${online} / ${store.devices.size}`);
    set(kpi('onlineSub'), `${store.devices.size - online} без связи`);
    set(kpi('pv'), fmtPower(pv));
    set(kpi('load'), fmtPower(load));
    set(kpi('alerts'), String(store.unreadAlerts));
  }

  cards.forEach((_, id) => updateCard(id));
  updateKpis();

  // Поиск и фильтр
  const search = view.querySelector('#ov-search');
  const filter = view.querySelector('#ov-filter');
  const applyFilter = () => {
    const q = search.value.trim().toLowerCase();
    cards.forEach((c) => {
      const show = (!q || c.el.dataset.name.includes(q)) && (filter.value !== 'problem' || c.problem);
      c.el.style.display = show ? '' : 'none';
    });
  };
  search.oninput = applyFilter;
  filter.onchange = applyFilter;

  // Живые обновления: копим изменённых клиентов и перерисовываем раз в кадр
  const dirty = new Set();
  const flush = rafThrottle(() => {
    dirty.forEach(updateCard);
    dirty.clear();
    updateKpis();
    if (filter.value) applyFilter();
  });
  const unsubscribe = store.subscribe((e) => {
    if (e.type === 'device') dirty.add(e.device.client);
    flush();
  });
  return unsubscribe;
}
