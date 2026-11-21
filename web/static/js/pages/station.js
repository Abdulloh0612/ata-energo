// =============================================================================
// Страница станции одного клиента:
// схема потоков энергии, показатели, таблица устройств, график истории, аварии.
// Для роли user это главная страница ("Моя станция").
// =============================================================================
import { api } from '../api.js';
import { LineChart } from '../chart.js';
import { createFlow, summarize } from '../flow.js';
import { store } from '../store.js';
import {
  esc, fmtDate, fmtNum, fmtPower, rafThrottle, statusBadge, timeAgo, TYPE_ORDER, typeLabel,
} from '../ui.js';

// Какие показатели можно смотреть на графике
const METRICS = {
  power: { label: 'Мощность', unit: 'Вт', color: '#f5b301' },
  voltage: { label: 'Напряжение', unit: 'В', color: '#3b82f6' },
  current: { label: 'Ток', unit: 'А', color: '#06b6d4' },
  battery_percent: { label: 'Заряд', unit: '%', color: '#22c55e' },
  temperature: { label: 'Температура', unit: '°C', color: '#f97316' },
};
const PERIODS = { '1h': 3600e3, '6h': 6 * 3600e3, '24h': 24 * 3600e3, '7d': 7 * 24 * 3600e3 };

export async function render(view, { clientId, setTitle }) {
  const me = store.me;
  // Данные клиента: сотрудникам — из API, клиенту — из /api/me/
  const client = me.can_view_all ? await api(`/api/clients/${clientId}/`) : me.client;
  if (!client) {
    view.innerHTML = '<div class="card empty">К вашему аккаунту не привязана станция. Обратитесь в Ата Энерго.</div>';
    return () => {};
  }
  clientId = client.id;
  setTitle(me.can_view_all ? client.name : 'Моя станция');

  view.innerHTML = `
    <div class="station-head">
      ${me.can_view_all ? '<a class="btn btn-sm" href="#/dashboard">← Все клиенты</a>' : ''}
      <h2>${esc(client.name)}</h2>
      <span class="muted">${esc(client.address || '')}${client.phone ? ' · ' + esc(client.phone) : ''}</span>
      <span class="badge" id="st-status">—</span>
    </div>
    <div id="st-modem" class="modem-bar"></div>

    <div class="grid-main">
      <div class="card flow-card" id="st-flow"></div>
      <div class="card">
        <div class="card-head"><h2>Сейчас</h2><span class="spacer"></span><small class="muted" id="st-updated"></small></div>
        <div class="stats">
          <div class="stat" style="--c:var(--pv)"><i></i><div><small>Солнечные панели</small><b data-s="pv">—</b></div><em data-s="pvSub"></em></div>
          <div class="stat" style="--c:var(--load)"><i></i><div><small>Потребление</small><b data-s="load">—</b></div><em></em></div>
          <div class="stat" style="--c:var(--bat)"><i></i><div><small>Батарея</small><b data-s="bat">—</b></div><em data-s="batSub"></em></div>
          <div class="stat" style="--c:var(--grid)"><i></i><div><small>Сеть</small><b data-s="grid">—</b></div><em data-s="gridSub"></em></div>
          <div class="stat" style="--c:var(--gen)" data-s="genRow"><i></i><div><small>Генератор</small><b data-s="gen">—</b></div><em></em></div>
          <div class="stat" style="--c:var(--inv)"><i></i><div><small>Инвертор</small><b data-s="inv">—</b></div><em data-s="invSub"></em></div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-head"><h2>Оборудование</h2></div>
      <div class="table-wrap"><table>
        <thead><tr><th>Устройство</th><th>ID</th><th>Статус</th><th>Мощность</th><th>Напряжение</th><th>Ток</th><th>Заряд</th><th>Темп.</th><th>Данные</th></tr></thead>
        <tbody id="st-devices"></tbody>
      </table></div>
    </div>

    <div class="card">
      <div class="card-head">
        <h2>История</h2><span class="spacer"></span>
        <select id="ch-device" style="width:auto"></select>
        <select id="ch-metric" style="width:auto"></select>
        <div class="seg" id="ch-period">
          <button data-p="1h">1 ч</button><button data-p="6h">6 ч</button>
          <button data-p="24h" class="active">24 ч</button><button data-p="7d">7 дн</button>
        </div>
      </div>
      <div class="chart-box"><canvas id="ch-canvas"></canvas></div>
    </div>

    <div class="card">
      <div class="card-head"><h2>Аварии</h2><span class="spacer"></span><a class="btn btn-sm" href="#/alerts">Все аварии</a></div>
      <div class="alerts" id="st-alerts"><div class="loading">Загрузка…</div></div>
    </div>`;

  const $ = (s) => view.querySelector(s);
  const set = (el, text) => { if (el && el.textContent !== text) el.textContent = text; };
  const stat = (k) => view.querySelector(`[data-s="${k}"]`);

  // ---------- Схема + показатели ----------
  const flow = createFlow($('#st-flow'));
  // Если у клиента несколько объектов (модемов) — показываем по одному, иначе цифры сложатся
  const clientModems = store.modemsOfClient(clientId);
  let selectedModem = clientModems.length > 1 ? clientModems[0].id : null;
  const devices = () => store.devicesOfClient(clientId)
    .filter((d) => !selectedModem || d.modem === selectedModem).sort(
    (a, b) => TYPE_ORDER.indexOf(a.device_type) - TYPE_ORDER.indexOf(b.device_type) || a.device_id.localeCompare(b.device_id));

  function updateSummary() {
    const list = devices();
    const s = summarize(list);
    flow.update(s);
    const nc = (info, text) => (info.exists && !info.online ? 'нет связи' : text);
    set(stat('pv'), nc(s.pv, fmtPower(s.pv.power)));
    set(stat('pvSub'), s.pv.exists ? `${list.filter((d) => d.device_type === 'panel').length} шт.` : 'нет');
    set(stat('load'), nc(s.load, fmtPower(s.load.power)));
    set(stat('bat'), s.bat.percent != null ? Math.round(s.bat.percent) + '%' : '—');
    set(stat('batSub'), !s.bat.online ? '' : s.bat.power > 20 ? 'заряд ' + fmtPower(s.bat.power) : s.bat.power < -20 ? 'разряд ' + fmtPower(-s.bat.power) : 'покой');
    const gridDown = s.grid.online && (s.grid.error || !s.grid.voltage);
    set(stat('grid'), !s.grid.online ? 'нет связи' : gridDown ? 'Отключена' : fmtPower(Math.abs(s.grid.power)));
    set(stat('gridSub'), gridDown ? '' : s.grid.power > 20 ? 'покупка' : s.grid.power < -20 ? 'продажа' : '');
    stat('genRow').style.display = s.gen.exists ? '' : 'none';
    set(stat('gen'), !s.gen.online ? 'нет связи' : s.gen.power > 20 ? fmtPower(s.gen.power) : 'выключен');
    set(stat('inv'), nc(s.inv, fmtPower(s.inv.power)));
    set(stat('invSub'), s.inv.temperature != null ? fmtNum(s.inv.temperature, '°C') : '');
    const badge = $('#st-status');
    badge.className = 'badge ' + (s.total && s.online === s.total ? 'online' : s.online ? 'warn' : 'danger');
    set(badge, `${s.online}/${s.total} в сети`);
    const last = list.map((d) => d.last_seen).filter(Boolean).sort().pop();
    set($('#st-updated'), last ? 'обновлено ' + timeAgo(last) : '');
  }

  // ---------- Таблица устройств ----------
  const rows = new Map();
  function buildTable() {
    const list = devices();
    const tbody = $('#st-devices');
    if (!list.length) {
      tbody.innerHTML = '<tr><td colspan="9" class="empty">Оборудование не добавлено</td></tr>';
      return;
    }
    tbody.innerHTML = list.map((d) => `
      <tr class="clickable" data-id="${d.id}">
        <td class="nowrap"><span class="type-dot t-${d.device_type}"></span>${esc(d.name || typeLabel(d.device_type))}</td>
        <td class="muted">${esc(d.device_id)}</td>
        <td data-c="status"></td><td class="num nowrap" data-c="power"></td><td class="num nowrap" data-c="voltage"></td>
        <td class="num nowrap" data-c="current"></td><td class="num" data-c="pct"></td><td class="num nowrap" data-c="temp"></td>
        <td class="muted nowrap" data-c="seen"></td>
      </tr>`).join('');
    rows.clear();
    tbody.querySelectorAll('tr[data-id]').forEach((tr) => {
      const c = {};
      tr.querySelectorAll('[data-c]').forEach((td) => (c[td.dataset.c] = td));
      rows.set(Number(tr.dataset.id), { tr, c });
      tr.onclick = () => { deviceSelect.value = tr.dataset.id; onDeviceChange(); };
    });
    list.forEach(updateRow);
  }
  function updateRow(d) {
    const r = rows.get(d.id);
    if (!r) return;
    const x = d.last_data || {};
    const html = statusBadge(d.status);
    if (r.c.status.innerHTML !== html) r.c.status.innerHTML = html;
    set(r.c.power, fmtPower(x.power));
    set(r.c.voltage, fmtNum(x.voltage, 'В'));
    set(r.c.current, fmtNum(x.current, 'А', 2));
    set(r.c.pct, x.battery_percent != null ? fmtNum(x.battery_percent, '%') : '');
    set(r.c.temp, fmtNum(x.temperature, '°C'));
    set(r.c.seen, x.error ? '⚠ ' + x.error : timeAgo(d.last_seen));
  }

  // ---------- График ----------
  const chart = new LineChart($('#ch-canvas'));
  const deviceSelect = $('#ch-device');
  const metricSelect = $('#ch-metric');
  let period = '24h';
  let loadToken = 0;

  function fillDeviceSelect() {
    const list = devices();
    const preferred = list.find((d) => d.device_type === 'inverter') || list[0];
    deviceSelect.innerHTML = list.map((d) => `<option value="${d.id}">${esc(d.name || typeLabel(d.device_type))} (${esc(d.device_id)})</option>`).join('');
    if (preferred) deviceSelect.value = preferred.id;
  }
  function fillMetricSelect() {
    const d = store.devices.get(Number(deviceSelect.value));
    const keys = Object.keys(METRICS).filter((k) => k !== 'battery_percent' || d?.device_type === 'battery');
    const current = metricSelect.value;
    metricSelect.innerHTML = keys.map((k) => `<option value="${k}">${METRICS[k].label}</option>`).join('');
    metricSelect.value = keys.includes(current) ? current : (d?.device_type === 'battery' ? 'battery_percent' : 'power');
  }
  async function loadHistory() {
    const id = Number(deviceSelect.value);
    if (!id) { chart.setData([]); return; }
    const token = ++loadToken;
    const from = new Date(Date.now() - PERIODS[period]).toISOString();
    const res = await api(`/api/devices/${id}/history/`, { params: { from, limit: 600 } });
    if (token !== loadToken) return; // пользователь уже выбрал другое
    const m = METRICS[metricSelect.value];
    chart.setData(res.points.map((p) => ({ t: new Date(p.timestamp), v: p[metricSelect.value] })), { color: m.color, unit: m.unit });
  }
  function onDeviceChange() {
    rows.forEach((r, id) => r.tr.classList.toggle('selected', id === Number(deviceSelect.value)));
    fillMetricSelect();
    loadHistory();
  }
  deviceSelect.onchange = onDeviceChange;
  metricSelect.onchange = loadHistory;
  $('#ch-period').onclick = (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    period = b.dataset.p;
    $('#ch-period').querySelectorAll('button').forEach((x) => x.classList.toggle('active', x === b));
    loadHistory();
  };

  // ---------- Аварии ----------
  const alertBox = $('#st-alerts');
  const alertHtml = (a) => `<div class="alert kind-${a.kind} ${a.is_read ? 'read' : ''}">
      <div>${esc(a.text)}<br><small>${fmtDate(a.created_at)}</small></div></div>`;
  async function loadAlerts() {
    const list = await api('/api/alerts/', { params: { client: clientId } });
    alertBox.innerHTML = list.length ? list.slice(0, 8).map(alertHtml).join('') : '<div class="empty">Аварий нет 👍</div>';
  }

  // ---------- Модем(ы) клиента ----------
  const modemBox = $('#st-modem');
  function updateModems() {
    const modems = store.modemsOfClient(clientId);
    if (!modems.length) {
      modemBox.innerHTML = `<div class="banner warn">Модем не указан. ${me.can_edit
        ? 'Добавьте SN модема в карточке клиента или на странице «Модемы».' : 'Обратитесь в Ата Энерго.'}</div>`;
      return;
    }
    const many = modems.length > 1;
    modemBox.innerHTML = (many ? '<div class="muted small">Объекты клиента — выберите, какой показать:</div>' : '') + modems.map((m) => {
      // запасная проверка: если данных нет дольше 35 сек — считаем, что связи нет
      const fresh = m.last_seen && Date.now() - new Date(m.last_seen).getTime() < 35000;
      const on = m.status === 'online' && fresh;
      return `<div class="modem-chip ${on ? 'on' : 'off'} ${many ? 'pick' : ''} ${m.id === selectedModem ? 'active' : ''}" data-modem="${m.id}">
          ${m.site ? `<b>${esc(m.site)}</b> <span class="muted">·</span> ` : ''}<b>Модем ${esc(m.serial)}</b>${m.name ? ` <span class="muted">· ${esc(m.name)}</span>` : ''}
          <span class="badge ${on ? 'online' : 'danger'}">${on ? 'на связи' : 'нет связи'}</span>
          <span class="muted">${m.last_seen ? 'данные ' + timeAgo(m.last_seen) : 'данных ещё не было'}</span>
        </div>
        ${on ? '' : `<div class="banner danger">⚠ Нет связи с модемом ${esc(m.serial)}${m.last_seen
          ? ' с ' + fmtDate(m.last_seen) : ''}. Проверьте питание модема и интернет (Wi-Fi / SIM).</div>`}`;
    }).join('');
  }

  modemBox.onclick = (e) => {
    const chip = e.target.closest('[data-modem]');
    if (!chip || store.modemsOfClient(clientId).length < 2) return;
    selectedModem = Number(chip.dataset.modem);
    updateModems();
    buildTable();
    updateSummary();
    fillDeviceSelect();
    onDeviceChange();
  };

  // ---------- Первый рендер ----------
  updateModems();
  buildTable();
  updateSummary();
  fillDeviceSelect();
  onDeviceChange();
  loadAlerts().catch(() => {});

  // ---------- Живые обновления ----------
  const dirty = new Set();
  let needSummary = false;
  let alive = true; // страница ещё открыта (кадр мог быть запланирован до ухода со страницы)
  const flush = rafThrottle(() => {
    if (!alive) return;
    dirty.forEach((id) => { const d = store.devices.get(id); if (d) updateRow(d); });
    dirty.clear();
    if (needSummary) { needSummary = false; updateSummary(); }
  });
  const unsubscribe = store.subscribe((e) => {
    if (e.type === 'device' && e.device.client === clientId && (!selectedModem || e.device.modem === selectedModem)) {
      dirty.add(e.device.id);
      needSummary = true;
      // новая точка на график, если это выбранное устройство
      if (e.device.id === Number(deviceSelect.value) && e.device.last_data?.timestamp) {
        chart.push({ t: new Date(e.device.last_data.timestamp), v: e.device.last_data[metricSelect.value] }, PERIODS[period]);
      }
      flush();
    }
    if (e.type === 'modem' && e.modem.client === clientId) updateModems();
    if (e.type === 'alert' && e.alert.device.client_id === clientId) {
      if (alertBox.querySelector('.empty')) alertBox.innerHTML = '';
      alertBox.insertAdjacentHTML('afterbegin', alertHtml(e.alert));
      while (alertBox.children.length > 8) alertBox.lastElementChild.remove();
    }
  });
  // раз в 5 сек обновляем подписи "N сек назад"
  const timer = setInterval(() => { devices().forEach(updateRow); updateSummary(); updateModems(); }, 5000);

  return () => { alive = false; unsubscribe(); clearInterval(timer); chart.destroy(); };
}
