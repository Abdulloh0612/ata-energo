// =============================================================================
// Страница "Поиск инверторов" (только admin и it_head).
//
// Реальный поиск делает ЛОКАЛЬНЫЙ АГЕНТ (agent/ata_agent.py) в сети клиента:
//   Wi-Fi/LAN: mDNS, SSDP, широковещательный запрос Solarman (UDP 48899),
//              порты 502 (Modbus TCP) и 8899 (Solarman V5) + реальные запросы к устройствам;
//   Bluetooth: Bluetooth LE через библиотеку bleak.
// Дополнительно — Bluetooth прямо в браузере (Web Bluetooth, Chrome/Edge).
//
// Порядок: найти -> «Выбрать устройство» -> «Проверить» (чтение показаний) -> «Подключить».
// =============================================================================
import { api } from '../api.js';
import { store } from '../store.js';
import { confirmModal, esc, fmtDate, formModal, openModal, timeAgo, toast } from '../ui.js';

const STATUS_CLASS = { found: 'offline', checking: 'warn', compatible: 'online', needs_setup: 'warn', connected: 'online', error: 'danger' };
const CONN_LABEL = { lan: 'Wi-Fi / LAN', wifi: 'Wi-Fi', bluetooth: 'Bluetooth' };
const KIND_LABEL = { inverter: 'Инвертор', logger: 'Logger', unknown: 'Неизвестное' };
const METHOD_LABEL = { wifi: 'Wi-Fi / LAN', bluetooth: 'Bluetooth', all: 'Все способы' };

// Сервисы, к которым браузеру разрешаем доступ (Web Bluetooth требует перечислить их заранее)
const BROWSER_BLE_SERVICES = ['generic_access', 'device_information', 'battery_service',
  0xffe0, 0xfff0, 0xffff, 0xfee7, '6e400001-b5a3-f393-e0a9-e50e24dcca9e'];

export async function render(view) {
  view.innerHTML = `
    <div class="card">
      <div class="card-head"><h2>Где искать</h2><span class="spacer"></span>
        <button class="btn btn-sm" id="ds-agents-btn">Агенты</button></div>
      <div id="ds-agent-box"></div>
      <div class="modes" id="ds-modes">
        <label class="mode"><input type="radio" name="mode" value="wifi" checked>
          <b>Wi-Fi / LAN</b><span>mDNS, SSDP, логгеры Solarman, Modbus TCP</span></label>
        <label class="mode"><input type="radio" name="mode" value="bluetooth">
          <b>Bluetooth</b><span>Bluetooth LE устройства рядом с агентом</span></label>
        <label class="mode"><input type="radio" name="mode" value="all">
          <b>Все способы</b><span>Wi-Fi / LAN и Bluetooth вместе</span></label>
      </div>
      <div class="toolbar">
        <input id="ds-subnet" placeholder="Сеть (необязательно), например 192.168.1.0/24">
        <button class="btn btn-primary" id="ds-start">Начать поиск</button>
        <button class="btn" id="ds-browser-ble" title="Chrome / Edge, нужен Bluetooth на этом устройстве">Bluetooth в этом браузере</button>
      </div>
      <div id="ds-progress" class="scan-log hidden"></div>
    </div>

    <div class="card">
      <div class="card-head"><h2>Найденные устройства</h2><span class="muted" id="ds-count"></span>
        <span class="spacer"></span>
        <label class="check-inline"><input type="checkbox" id="ds-only"> только инверторы и Logger</label>
        <button class="btn btn-sm" id="ds-clear">Очистить список</button></div>
      <div class="table-wrap"><table class="compact">
        <thead><tr><th></th><th>Название</th><th>Производитель</th><th>Модель</th><th>IP-адрес</th><th>Bluetooth ID</th>
          <th>Подключение</th><th>Сигнал</th><th>Тип</th><th>Протоколы</th><th>Статус</th></tr></thead>
        <tbody id="ds-body"><tr><td colspan="11" class="loading">Загрузка…</td></tr></tbody>
      </table></div>
    </div>`;

  const $ = (s) => view.querySelector(s);
  let agents = [];
  let devices = [];
  let pollTimer = null;
  let destroyed = false;

  // ---------- Агенты ----------
  async function loadAgents() {
    agents = await api('/api/agents/');
    const box = $('#ds-agent-box');
    if (!agents.length) {
      box.innerHTML = `<div class="banner warn">Нет ни одного агента. Поиск выполняет локальный агент — программа на компьютере
        в той же сети, что и инвертор. Нажмите «Агенты» → «Добавить агента» и запустите его по инструкции.</div>`;
      return;
    }
    const selected = $('#ds-agent')?.value;
    box.innerHTML = `<div class="toolbar"><select id="ds-agent">${agents.map((a) =>
      `<option value="${a.id}">${esc(a.name)} — ${a.online ? 'в сети' : 'не в сети'}</option>`).join('')}</select>
      <span id="ds-agent-info" class="muted"></span></div>`;
    const sel = $('#ds-agent');
    sel.value = selected && agents.some((a) => String(a.id) === selected) ? selected : (agents.find((a) => a.online) || agents[0]).id;
    sel.onchange = drawAgentInfo;
    drawAgentInfo();
  }
  function currentAgent() { return agents.find((a) => String(a.id) === $('#ds-agent')?.value); }
  function drawAgentInfo() {
    const a = currentAgent();
    if (!a) return;
    const bt = a.capabilities?.bluetooth;
    $('#ds-agent-info').innerHTML = a.online
      ? `${statusDot(true)} ${esc(a.hostname || '')} · сеть ${esc(a.local_ip || '?')} · Bluetooth: ${bt?.available ? 'есть' : 'нет'}`
        + `${a.capabilities?.mdns === false ? ' · mDNS: нет zeroconf' : ''}${a.capabilities?.solarman === false ? ' · Solarman: нет pysolarmanv5' : ''}`
      : `${statusDot(false)} агент не на связи ${a.last_seen ? '(был ' + timeAgo(a.last_seen) + ')' : '(ещё ни разу не подключался)'}`;
  }
  const statusDot = (on) => `<span class="badge ${on ? 'online' : 'danger'}">${on ? 'в сети' : 'нет связи'}</span>`;

  // ---------- Список устройств ----------
  async function loadDevices() {
    devices = await api('/api/discovery/devices/');
    drawDevices();
  }
  function drawDevices() {
    const only = $('#ds-only').checked;
    const list = devices.filter((d) => !only || d.device_kind !== 'unknown');
    $('#ds-count').textContent = devices.length ? `· ${list.length} из ${devices.length}` : '';
    const body = $('#ds-body');
    if (!list.length) {
      body.innerHTML = `<tr><td colspan="11" class="empty">${devices.length ? 'Нет инверторов и Logger среди найденного'
        : 'Пока ничего не найдено. Выберите способ и нажмите «Начать поиск».'}</td></tr>`;
      return;
    }
    body.innerHTML = list.map((d) => `
      <tr>
        <td><button class="btn btn-sm btn-primary" data-pick="${d.id}">Выбрать устройство</button></td>
        <td><b>${esc(d.name || '—')}</b>${d.source === 'browser' ? '<br><small class="muted">найдено браузером</small>' : ''}</td>
        <td>${esc(d.manufacturer || '—')}</td>
        <td>${esc(d.model || '—')}</td>
        <td class="nowrap">${esc(d.ip || '—')}</td>
        <td class="nowrap muted">${esc(d.bt_address || '—')}</td>
        <td class="nowrap">${CONN_LABEL[d.connection_type] || esc(d.connection_type)}</td>
        <td class="nowrap">${d.rssi != null ? d.rssi + ' дБм' : '—'}</td>
        <td><span class="kind kind-${d.device_kind}">${KIND_LABEL[d.device_kind]}</span></td>
        <td><small>${(d.protocols || []).map(esc).join('<br>') || '—'}</small></td>
        <td><span class="badge ${STATUS_CLASS[d.status] || ''} wrap">${esc(d.status_display)}</span></td>
      </tr>`).join('');
  }
  $('#ds-only').onchange = drawDevices;
  $('#ds-body').onclick = (e) => {
    const b = e.target.closest('[data-pick]');
    if (b) openDevice(devices.find((d) => d.id === Number(b.dataset.pick)));
  };
  $('#ds-clear').onclick = async () => {
    if (!(await confirmModal('Очистить список найденных устройств? Подключённые останутся.', 'Очистить'))) return;
    await api('/api/discovery/devices/clear/', { method: 'POST' });
    loadDevices();
  };

  // ---------- Поиск через агента ----------
  function showLog(text, cls = '') {
    const box = $('#ds-progress');
    box.classList.remove('hidden');
    box.className = 'scan-log ' + cls;
    box.textContent = text;
    box.scrollTop = box.scrollHeight;
  }

  $('#ds-start').onclick = async () => {
    const agent = currentAgent();
    if (!agent) { toast('Сначала добавьте и запустите агента', 'error'); return; }
    if (!agent.online) { toast('Агент не на связи. Запустите его на компьютере в сети клиента.', 'error'); return; }
    const method = view.querySelector('input[name=mode]:checked').value;
    const btn = $('#ds-start');
    btn.disabled = true;
    btn.textContent = 'Идёт поиск…';
    try {
      const task = await api('/api/discovery/scan/', { method: 'POST', body: { agent: agent.id, method, subnet: $('#ds-subnet').value } });
      showLog(`Задача отправлена агенту «${agent.name}» (${METHOD_LABEL[method]}). Ждём…`);
      await waitTask(task.id, (t) => {
        showLog(t.log || 'Агент получил задачу…', t.status === 'error' ? 'error' : '');
      });
    } catch (err) {
      showLog(err.message, 'error');
    } finally {
      btn.disabled = false;
      btn.textContent = 'Начать поиск';
      loadDevices();
    }
  };

  // Ждём окончания задачи агента, вызывая onUpdate при каждом опросе
  function waitTask(id, onUpdate) {
    return new Promise((resolve, reject) => {
      const started = Date.now();
      const tick = async () => {
        if (destroyed) { resolve(null); return; }
        try {
          const t = await api(`/api/discovery/tasks/${id}/`);
          onUpdate(t);
          if (t.found?.length) { mergeFound(t.found); drawDevices(); }
          if (t.status === 'done' || t.status === 'error') { resolve(t); return; }
          if (t.status === 'pending' && Date.now() - started > 30000) {
            reject(new Error('Агент не забрал задачу за 30 секунд. Проверьте, что он запущен и на связи.'));
            return;
          }
          if (Date.now() - started > 5 * 60000) { reject(new Error('Задача выполняется слишком долго')); return; }
        } catch (err) { reject(err); return; }
        pollTimer = setTimeout(tick, 1500);
      };
      tick();
    });
  }
  function mergeFound(found) {
    for (const d of found) {
      const i = devices.findIndex((x) => x.id === d.id);
      if (i >= 0) devices[i] = d; else devices.unshift(d);
    }
  }

  // ---------- Bluetooth в браузере (Web Bluetooth) ----------
  $('#ds-browser-ble').onclick = async () => {
    if (!navigator.bluetooth) {
      toast('Этот браузер не поддерживает Web Bluetooth. Нужен Chrome или Edge (на компьютере или Android), '
        + 'сайт по https:// или localhost. Или используйте поиск Bluetooth через агента.', 'error');
      return;
    }
    let device;
    try {
      // Браузер сам показывает окно выбора устройства и спрашивает разрешение
      device = await navigator.bluetooth.requestDevice({ acceptAllDevices: true, optionalServices: BROWSER_BLE_SERVICES });
    } catch (err) {
      if (err.name !== 'NotFoundError') toast('Bluetooth: ' + err.message, 'error');
      return;
    }
    showLog(`Выбрано: ${device.name || '(без имени)'} (${device.id}). Подключаюсь…`);
    const payload = { id: device.id, name: device.name || '', services: [], connected: false };
    try {
      const server = await device.gatt.connect();
      payload.connected = true;
      const services = await server.getPrimaryServices();
      payload.services = services.map((s) => s.uuid);
      device.gatt.disconnect();
      showLog(`Подключено. Доступных сервисов: ${payload.services.length}\n${payload.services.join('\n')}`);
    } catch (err) {
      payload.error = err.message;
      showLog(`Устройство выбрано, но подключиться не удалось: ${err.message}`, 'error');
    }
    const saved = await api('/api/discovery/browser-bluetooth/', { method: 'POST', body: payload });
    mergeFound([saved]);
    drawDevices();
  };

  // ---------- Окно устройства: проверка и подключение ----------
  function openDevice(d) {
    const m = openModal('<div id="dv-root"></div>');
    m.el.classList.add('modal-lg');
    const root = m.el.querySelector('#dv-root');
    let checking = false;

    const draw = () => {
      const r = d.check_result || {};
      const isBle = d.connection_type === 'bluetooth';
      const det = d.details || {};
      root.innerHTML = `
        <h3>${esc(d.name || d.key)}</h3>
        <div class="kv">
          <div><small>Производитель</small><b>${esc(d.manufacturer || '—')}</b></div>
          <div><small>Модель</small><b>${esc(d.model || '—')}</b></div>
          <div><small>Тип</small><b>${KIND_LABEL[d.device_kind]}</b></div>
          <div><small>Серийный номер</small><b>${esc(d.serial || '—')}</b></div>
          <div><small>IP / MAC</small><b>${esc(d.ip || '—')}${d.mac ? ' · ' + esc(d.mac) : ''}</b></div>
          <div><small>Bluetooth ID / сигнал</small><b>${esc(d.bt_address || '—')}${d.rssi != null ? ' · ' + d.rssi + ' дБм' : ''}</b></div>
          <div><small>Подключение</small><b>${CONN_LABEL[d.connection_type] || ''}</b></div>
          <div><small>Найдено</small><b>${esc(d.agent_name || 'браузером')} · ${fmtDate(d.last_seen)}</b></div>
        </div>
        <div class="muted small">Протоколы: ${(d.protocols || []).map(esc).join(', ') || '—'}
          ${det.mac_vendor ? '<br>Модуль: ' + esc(det.mac_vendor) : ''}${det.inverter ? '<br>Инвертор за логгером: ' + esc(det.inverter) : ''}
          ${det.services?.length ? '<br>Bluetooth-сервисы: ' + det.services.map(esc).join(', ') : ''}</div>

        <h4>1. Проверка совместимости</h4>
        ${d.source === 'browser' ? '<div class="banner warn">Найдено браузером: браузер может только подключиться и показать сервисы. Чтение показаний по Bluetooth для этого устройства не настроено. Устройство обнаружено, требуется настройка протокола.</div>' : `
        ${isBle ? '' : `<details ${r.ok === false ? 'open' : ''}><summary>Настройки протокола (если авто не сработало)</summary>
          <div class="grid-form">
            <label>Протокол<select name="protocol"><option value="auto">Авто</option><option value="solarman_v5">Solarman V5 (логгер, 8899)</option>
              <option value="modbus_tcp">Modbus TCP (502)</option><option value="sunspec">SunSpec (Modbus TCP)</option></select></label>
            <label>Порт<input name="port" placeholder="8899 или 502"></label>
            <label>SN логгера (цифры под QR-кодом)<input name="logger_serial" value="${esc(det.logger_serial || d.serial || '')}"></label>
            <label>Modbus адрес (unit id)<input name="unit_id" value="1"></label>
            <label>Карта регистров<select name="profile"><option value="auto">Авто</option><option value="deye_1ph">Deye / Sunsynk 1-фазный</option>
              <option value="deye_3ph">Deye 3-фазный</option><option value="sunspec">SunSpec</option></select></label>
            <label class="check"><input type="checkbox" name="invert_grid"> инвертировать знак сети</label>
          </div></details>`}
        <button class="btn btn-primary" id="dv-check" ${checking ? 'disabled' : ''}>${checking ? 'Проверка…' : isBle ? 'Проверить подключение' : 'Проверить'}</button>`}
        <div id="dv-log" class="scan-log ${checking ? '' : 'hidden'}"></div>
        ${r.attempts ? `<div class="attempts">${r.attempts.map((a) => `<div class="${a.ok ? 'ok' : 'bad'}">${a.ok ? '✓' : '✗'} <b>${esc(a.protocol)}</b>: ${esc(a.detail)}</div>`).join('')}</div>` : ''}
        ${r.ok === false ? `<div class="banner warn">${esc(r.message || 'Устройство обнаружено, требуется настройка протокола')}</div>` : ''}
        ${r.ble_services ? `<div class="muted small">GATT-сервисы: ${r.ble_services.map((s) => esc(s.uuid + (s.name ? ' ' + s.name : ''))).join(', ')}</div>` : ''}
        ${r.ok ? readingsHtml(r) : ''}

        <h4>2. Подключение к объекту клиента</h4>
        ${d.status === 'connected' ? `<div class="banner ok">Подключено: модем ${esc(d.modem_serial || '')}. <a href="#/client/${d.modem_client}">Открыть мониторинг →</a></div>` : ''}
        ${r.ok ? '<button class="btn btn-primary" id="dv-connect">' + (d.status === 'connected' ? 'Переподключить / изменить' : 'Подключить') + '</button>'
          : '<div class="muted small">Подключить можно после успешной проверки (когда реальные показания прочитаны).</div>'}
        <div class="modal-actions"><button class="btn" id="dv-close">Закрыть</button></div>`;

      root.querySelector('#dv-close').onclick = m.close;
      root.querySelector('#dv-check')?.addEventListener('click', runCheck);
      root.querySelector('#dv-connect')?.addEventListener('click', connect);
    };

    async function runCheck() {
      const body = {};
      root.querySelectorAll('details input, details select').forEach((el) => {
        if (el.type === 'checkbox') body[el.name] = el.checked;
        else if (el.value && el.value !== 'auto') body[el.name] = el.value;
      });
      if (body.protocol === undefined && root.querySelector('[name=protocol]')) body.protocol = 'auto';
      checking = true;
      draw();
      const logBox = root.querySelector('#dv-log');
      try {
        const task = await api(`/api/discovery/devices/${d.id}/probe/`, { method: 'POST', body });
        logBox.textContent = 'Задача отправлена агенту…';
        await waitTask(task.id, (t) => { const lb = root.querySelector('#dv-log'); if (lb) lb.textContent = t.log || 'Агент проверяет…'; });
      } catch (err) {
        toast(err.message, 'error');
      }
      checking = false;
      await loadDevices();
      d = devices.find((x) => x.id === d.id) || d;
      if (document.body.contains(root)) draw();
    }

    async function connect() {
      const clients = await api('/api/clients/');
      if (!clients.length) { toast('Сначала добавьте клиента', 'error'); return; }
      const r = d.check_result;
      const sites = [...new Set(clients.flatMap((c) => [c.address, ...c.modems.map((x) => x.site)]).filter(Boolean))];
      const form = formModal({
        title: 'Подключить к объекту клиента',
        submitText: 'Подключить',
        values: { client: clients[0].id, site: clients[0].address, name: [d.manufacturer, d.model].filter(Boolean).join(' '),
          poll_interval: 10, has_generator: Math.abs(r.readings?.generator?.power || 0) > 20 },
        fields: [
          { name: 'client', label: 'Клиент', type: 'select', required: true, options: clients.map((c) => [c.id, c.name]) },
          { name: 'site', label: 'Объект (дом, участок, адрес)', required: true, help: 'Можно выбрать из списка или ввести новый' },
          { name: 'name', label: 'Название / модель инвертора', half: true },
          { name: 'poll_interval', label: 'Опрос каждые, сек', half: true },
          { name: 'has_generator', label: 'У объекта есть генератор', type: 'checkbox' },
        ],
        onSubmit: async (data) => {
          data.client = Number(data.client);
          data.poll_interval = Number(data.poll_interval) || 10;
          const res = await api(`/api/discovery/devices/${d.id}/connect/`, { method: 'POST', body: data });
          const [devs, modems] = await Promise.all([api('/api/devices/'), api('/api/modems/')]);
          store.setDevices(devs);
          store.setModems(modems);
          toast(`Подключено как модем ${res.serial}. Агент начнёт присылать показания.`, 'ok');
          d = res.device;
          await loadDevices();
          if (document.body.contains(root)) draw();
        },
      });
      // подсказки объектов и автоподстановка адреса клиента
      const siteInput = form.el.querySelector('[name=site]');
      siteInput.setAttribute('list', 'site-list');
      siteInput.insertAdjacentHTML('afterend', `<datalist id="site-list">${sites.map((s) => `<option value="${esc(s)}">`).join('')}</datalist>`);
      form.el.querySelector('[name=client]').onchange = (e) => {
        const c = clients.find((x) => x.id === Number(e.target.value));
        if (c?.address) siteInput.value = c.address;
      };
    }

    draw();
  }

  function readingsHtml(r) {
    const x = r.readings || {};
    const row = (label, v, unit) => (v == null ? '' : `<tr><td>${label}</td><td class="num">${Math.round(v * 100) / 100} ${unit}</td></tr>`);
    const pv = (x.pv || []);
    return `<div class="banner ok">✓ ${esc(r.message)} Протокол: <b>${esc(r.protocol_label || r.protocol)}</b>,
        карта регистров: <b>${esc(r.profile_name || r.profile)}</b>${r.serial ? ', SN ' + esc(r.serial) : ''}</div>
      <div class="table-wrap"><table class="readings"><tbody>
        ${pv.map((p, i) => row(`Панели ${i + 1}, мощность`, p.power, 'Вт') + row(`Панели ${i + 1}, напряжение`, p.voltage, 'В')).join('')}
        ${row('Инвертор, мощность', x.inverter?.power, 'Вт')}${row('Инвертор, температура', x.inverter?.temperature, '°C')}
        ${row('Батарея, заряд', x.battery?.soc, '%')}${row('Батарея, напряжение', x.battery?.voltage, 'В')}
        ${row('Батарея, мощность (+ заряд / − разряд)', x.battery?.power, 'Вт')}
        ${row('Сеть, напряжение', x.grid?.voltage, 'В')}${row('Сеть, мощность (+ покупка / − продажа)', x.grid?.power, 'Вт')}
        ${row('Нагрузка', x.load?.power, 'Вт')}${row('Генератор', x.generator?.power, 'Вт')}
      </tbody></table></div>
      ${r.raw ? `<div class="muted small">Сырые регистры: ${Object.entries(r.raw).map(([k, v]) => esc(k + ' = ' + v)).join(' · ')}</div>` : ''}`;
  }

  // ---------- Управление агентами ----------
  $('#ds-agents-btn').onclick = async () => {
    const clients = await api('/api/clients/');
    const m = openModal('<div id="ag-root"></div>');
    m.el.classList.add('modal-lg');
    const root = m.el.querySelector('#ag-root');
    const draw = (tokenInfo) => {
      root.innerHTML = `<h3>Локальные агенты</h3>
        <p class="muted small">Агент — программа на компьютере, мини-ПК или Raspberry Pi в сети клиента (там же, где инвертор).
          Она ищет устройства и отправляет показания на этот сервер по HTTPS. Один агент на объект.</p>
        ${tokenInfo ? `<div class="banner ok">Агент создан. Токен показывается <b>один раз</b> — скопируйте команду:
          <pre class="cmd">${esc(tokenInfo.command)}</pre>
          На Windows можно запустить <b>start_agent.bat</b> и вставить токен, когда он спросит.</div>` : ''}
        <div class="table-wrap"><table><thead><tr><th>Название</th><th>Где</th><th>Компьютер</th><th>Сеть</th><th>Статус</th><th></th></tr></thead><tbody>
        ${agents.map((a) => `<tr><td><b>${esc(a.name)}</b><br><small class="muted">токен …${esc(a.token_hint)}</small></td>
          <td>${esc(a.client_name || '—')}</td><td>${esc(a.hostname || '—')}<br><small class="muted">${esc(a.platform || '')}</small></td>
          <td>${esc(a.local_ip || '—')}</td><td>${statusDot(a.online)}<br><small class="muted">${a.last_seen ? timeAgo(a.last_seen) : 'не подключался'}</small></td>
          <td class="actions"><button class="btn btn-sm" data-tok="${a.id}">Новый токен</button>
            <button class="btn btn-sm btn-danger" data-del="${a.id}">Удалить</button></td></tr>`).join('')
          || '<tr><td colspan="6" class="empty">Агентов нет</td></tr>'}
        </tbody></table></div>
        <div class="modal-actions"><button class="btn" data-close>Закрыть</button><button class="btn btn-primary" data-add>+ Добавить агента</button></div>`;
      root.querySelector('[data-close]').onclick = m.close;
      root.querySelector('[data-add]').onclick = () => formModal({
        title: 'Новый агент', submitText: 'Создать',
        fields: [{ name: 'name', label: 'Название (например: Дом Асанова, Офис)', required: true },
          { name: 'client', label: 'Где установлен', type: 'select', options: [['', '— не указано —'], ...clients.map((c) => [c.id, c.name])] }],
        onSubmit: async (data) => {
          const res = await api('/api/agents/', { method: 'POST', body: { name: data.name, client: data.client ? Number(data.client) : null } });
          await loadAgents();
          draw(res);
        },
      });
      root.querySelectorAll('[data-tok]').forEach((b) => (b.onclick = async () => {
        if (!(await confirmModal('Выдать новый токен? Старый перестанет работать.', 'Выдать'))) return;
        draw(await api(`/api/agents/${b.dataset.tok}/new_token/`, { method: 'POST' }));
      }));
      root.querySelectorAll('[data-del]').forEach((b) => (b.onclick = async () => {
        if (!(await confirmModal('Удалить агента? Подключённые через него устройства перестанут получать данные.'))) return;
        await api(`/api/agents/${b.dataset.del}/`, { method: 'DELETE' });
        await loadAgents();
        draw();
      }));
    };
    draw();
  };

  await Promise.all([loadAgents(), loadDevices()]);
  // статус агентов обновляем раз в 5 сек
  const agentTimer = setInterval(() => { loadAgents().catch(() => {}); }, 5000);
  return () => { destroyed = true; clearTimeout(pollTimer); clearInterval(agentTimer); };
}
