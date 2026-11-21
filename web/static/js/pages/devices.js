// =============================================================================
// Страница "Оборудование": все устройства (панели, инверторы, батареи,
// генераторы, сеть, нагрузка). admin / it_head могут менять, dispatcher — смотреть.
// =============================================================================
import { api } from '../api.js';
import { store } from '../store.js';
import {
  confirmModal, esc, fmtPower, formModal, rafThrottle, statusBadge, timeAgo, toast, TYPE_LABELS, typeLabel,
} from '../ui.js';

export async function render(view, { query }) {
  const canEdit = store.me.can_edit;
  view.innerHTML = `
    <div class="toolbar">
      <select id="dv-client"><option value="">Все клиенты</option></select>
      <select id="dv-type"><option value="">Все типы</option>
        ${Object.entries(TYPE_LABELS).map(([k, v]) => `<option value="${k}">${v}</option>`).join('')}</select>
      <select id="dv-status"><option value="">Любой статус</option><option value="online">В сети</option><option value="offline">Нет связи</option></select>
      <input type="search" id="dv-search" placeholder="ID или серийный номер…">
      <span class="spacer"></span>
      ${canEdit ? '<button class="btn btn-primary" id="dv-add">+ Добавить устройство</button>' : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>Тип</th><th>Название</th><th>ID (модем)</th><th>Серийный №</th><th>Клиент</th><th>Статус</th><th>Мощность</th><th>Данные</th><th></th></tr></thead>
        <tbody id="dv-body"></tbody>
      </table></div>
    </div>`;

  const $ = (s) => view.querySelector(s);
  const clients = await api('/api/clients/');
  $('#dv-client').insertAdjacentHTML('beforeend', clients.map((c) => `<option value="${c.id}">${esc(c.name)}</option>`).join(''));
  if (query.get('client')) $('#dv-client').value = query.get('client');

  const body = $('#dv-body');
  const rows = new Map();

  // Фильтруем устройства прямо в браузере (они уже загружены в store)
  function filtered() {
    const cid = Number($('#dv-client').value) || null;
    const type = $('#dv-type').value;
    const status = $('#dv-status').value;
    const q = $('#dv-search').value.trim().toLowerCase();
    return [...store.devices.values()]
      .filter((d) => (!cid || d.client === cid) && (!type || d.device_type === type) && (!status || d.status === status)
        && (!q || d.device_id.toLowerCase().includes(q) || (d.serial_number || '').toLowerCase().includes(q)))
      .sort((a, b) => (a.client_name || '').localeCompare(b.client_name || '') || a.device_id.localeCompare(b.device_id));
  }

  function draw() {
    const list = filtered();
    rows.clear();
    if (!list.length) { body.innerHTML = '<tr><td colspan="9" class="empty">Устройств нет</td></tr>'; return; }
    body.innerHTML = list.map((d) => `
      <tr data-id="${d.id}">
        <td class="nowrap"><span class="type-dot t-${d.device_type}"></span>${typeLabel(d.device_type)}</td>
        <td>${esc(d.name || '—')}</td>
        <td><b>${esc(d.device_id)}</b></td>
        <td class="muted">${esc(d.serial_number || '—')}</td>
        <td><a href="#/client/${d.client}">${esc(d.client_name || clients.find((c) => c.id === d.client)?.name || '—')}</a></td>
        <td data-c="status">${statusBadge(d.status)}</td>
        <td class="num nowrap" data-c="power">${fmtPower(d.last_data?.power)}</td>
        <td class="muted nowrap" data-c="seen">${timeAgo(d.last_seen)}</td>
        <td class="actions">${canEdit ? `<button class="btn btn-sm" data-edit="${d.id}">Изменить</button>
          <button class="btn btn-sm btn-danger" data-del="${d.id}">Удалить</button>` : ''}</td>
      </tr>`).join('');
    body.querySelectorAll('tr[data-id]').forEach((tr) => {
      const c = {};
      tr.querySelectorAll('[data-c]').forEach((td) => (c[td.dataset.c] = td));
      rows.set(Number(tr.dataset.id), c);
    });
  }

  function updateRow(d) {
    const c = rows.get(d.id);
    if (!c) return;
    const html = statusBadge(d.status);
    if (c.status.innerHTML !== html) c.status.innerHTML = html;
    c.power.textContent = fmtPower(d.last_data?.power);
    c.seen.textContent = timeAgo(d.last_seen);
  }

  function openForm(device) {
    formModal({
      title: device ? 'Изменить устройство' : 'Новое устройство',
      values: device || { client: $('#dv-client').value || clients[0]?.id, device_type: 'inverter' },
      fields: [
        { name: 'client', label: 'Клиент', type: 'select', required: true, options: clients.map((c) => [c.id, c.name]) },
        { name: 'device_type', label: 'Тип', type: 'select', required: true, options: Object.entries(TYPE_LABELS), half: true },
        { name: 'name', label: 'Название', half: true, placeholder: 'Например: Инвертор Deye 8 кВт' },
        { name: 'device_id', label: 'ID устройства (как шлёт модем)', required: true, half: true, placeholder: 'INV003' },
        { name: 'serial_number', label: 'Серийный номер', required: true, half: true },
      ],
      onSubmit: async (data) => {
        data.client = Number(data.client);
        const saved = device
          ? await api(`/api/devices/${device.id}/`, { method: 'PATCH', body: data })
          : await api('/api/devices/', { method: 'POST', body: data });
        if (device) store.removeDevice(device.id);
        store.addDevice(saved);
        toast('Сохранено', 'ok');
        draw();
      },
    });
  }

  $('#dv-add')?.addEventListener('click', () => {
    if (!clients.length) { toast('Сначала добавьте клиента', 'error'); return; }
    openForm(null);
  });
  body.onclick = async (e) => {
    const edit = e.target.closest('[data-edit]');
    const del = e.target.closest('[data-del]');
    if (edit) openForm(store.devices.get(Number(edit.dataset.edit)));
    if (del) {
      const d = store.devices.get(Number(del.dataset.del));
      if (!(await confirmModal(`Удалить устройство ${d.device_id} и всю его историю?`))) return;
      try {
        await api(`/api/devices/${d.id}/`, { method: 'DELETE' });
        store.removeDevice(d.id);
        toast('Удалено', 'ok');
        draw();
      } catch (err) { toast(err.message, 'error'); }
    }
  };
  ['#dv-client', '#dv-type', '#dv-status'].forEach((s) => ($(s).onchange = draw));
  $('#dv-search').oninput = draw;
  draw();

  // Живые обновления строк
  const dirty = new Set();
  const flush = rafThrottle(() => { dirty.forEach((id) => { const d = store.devices.get(id); if (d) updateRow(d); }); dirty.clear(); });
  const unsubscribe = store.subscribe((e) => { if (e.type === 'device') { dirty.add(e.device.id); flush(); } });
  return unsubscribe;
}
