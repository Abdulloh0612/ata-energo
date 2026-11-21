// =============================================================================
// Страница "Модемы": модемы инверторов всех клиентов.
// Серийный номер (SN) модема написан на наклейке под QR-кодом.
// При добавлении модема сервер сам создаёт инвертор, панели, батарею, сеть,
// нагрузку и генератор (если отмечен). admin / it_head меняют, dispatcher смотрит.
// =============================================================================
import { api } from '../api.js';
import { store } from '../store.js';
import { confirmModal, esc, formModal, rafThrottle, statusBadge, timeAgo, toast } from '../ui.js';

export async function render(view) {
  const canEdit = store.me.can_edit;
  view.innerHTML = `
    <div class="toolbar">
      <input type="search" id="md-search" placeholder="Поиск по SN или клиенту…">
      <select id="md-status"><option value="">Любой статус</option><option value="online">В сети</option><option value="offline">Нет связи</option></select>
      <span class="spacer"></span>
      ${canEdit ? '<button class="btn btn-primary" id="md-add">+ Добавить модем</button>' : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>SN модема</th><th>Клиент</th><th>Инвертор</th><th>Генератор</th><th>Статус</th><th>Последние данные</th><th></th></tr></thead>
        <tbody id="md-body"><tr><td colspan="7" class="loading">Загрузка…</td></tr></tbody>
      </table></div>
    </div>`;

  const $ = (s) => view.querySelector(s);
  const body = $('#md-body');
  const clients = await api('/api/clients/');
  let modems = await api('/api/modems/');
  store.setModems(modems);

  function draw() {
    const q = $('#md-search').value.trim().toLowerCase();
    const status = $('#md-status').value;
    const list = [...store.modems.values()].filter((m) => (!status || m.status === status)
      && (!q || `${m.serial} ${m.client_name || ''}`.toLowerCase().includes(q)));
    if (!list.length) { body.innerHTML = '<tr><td colspan="7" class="empty">Модемов нет</td></tr>'; return; }
    body.innerHTML = list.map((m) => `
      <tr data-id="${m.id}">
        <td><b>${esc(m.serial)}</b></td>
        <td><a href="#/client/${m.client}">${esc(m.client_name || '')}</a></td>
        <td>${esc(m.name || '—')}</td>
        <td>${m.has_generator ? 'есть' : '<span class="muted">нет</span>'}</td>
        <td data-c="status">${statusBadge(m.status)}</td>
        <td class="muted nowrap" data-c="seen">${timeAgo(m.last_seen)}</td>
        <td class="actions">
          <a class="btn btn-sm" href="#/client/${m.client}">Мониторинг</a>
          ${canEdit ? `<button class="btn btn-sm" data-edit="${m.id}">Изменить</button>
            <button class="btn btn-sm btn-danger" data-del="${m.id}">Удалить</button>` : ''}
        </td>
      </tr>`).join('');
  }

  async function reload() {
    modems = await api('/api/modems/');
    store.setModems(modems);
    store.setDevices(await api('/api/devices/')); // у модема могли появиться/исчезнуть устройства
    draw();
  }

  function openForm(modem) {
    formModal({
      title: modem ? 'Изменить модем' : 'Новый модем',
      values: modem || { client: clients[0]?.id, has_generator: false },
      fields: [
        { name: 'client', label: 'Клиент', type: 'select', required: true, options: clients.map((c) => [c.id, c.name]) },
        { name: 'serial', label: 'SN модема (номер под QR-кодом)', required: true, placeholder: 'например 2712345678' },
        { name: 'name', label: 'Модель инвертора', placeholder: 'например Deye 8 кВт' },
        { name: 'has_generator', label: 'У клиента есть генератор', type: 'checkbox' },
      ],
      onSubmit: async (data) => {
        data.client = Number(data.client);
        if (modem) await api(`/api/modems/${modem.id}/`, { method: 'PATCH', body: data });
        else await api('/api/modems/', { method: 'POST', body: data });
        toast('Модем сохранён', 'ok');
        await reload();
      },
    });
  }

  $('#md-add')?.addEventListener('click', () => {
    if (!clients.length) { toast('Сначала добавьте клиента', 'error'); return; }
    openForm(null);
  });
  body.onclick = async (e) => {
    const edit = e.target.closest('[data-edit]');
    const del = e.target.closest('[data-del]');
    if (edit) openForm(store.modems.get(Number(edit.dataset.edit)));
    if (del) {
      const m = store.modems.get(Number(del.dataset.del));
      if (!(await confirmModal(`Удалить модем ${m.serial} вместе с его оборудованием и историей?`))) return;
      try { await api(`/api/modems/${m.id}/`, { method: 'DELETE' }); toast('Удалено', 'ok'); await reload(); }
      catch (err) { toast(err.message, 'error'); }
    }
  };
  $('#md-search').oninput = draw;
  $('#md-status').onchange = draw;
  draw();

  // Живое обновление статуса модемов
  const flush = rafThrottle(() => {
    body.querySelectorAll('tr[data-id]').forEach((tr) => {
      const m = store.modems.get(Number(tr.dataset.id));
      if (!m) return;
      const html = statusBadge(m.status);
      const cell = tr.querySelector('[data-c="status"]');
      if (cell.innerHTML !== html) cell.innerHTML = html;
      tr.querySelector('[data-c="seen"]').textContent = timeAgo(m.last_seen);
    });
  });
  const unsubscribe = store.subscribe((e) => { if (e.type === 'modem') flush(); });
  const timer = setInterval(flush, 5000);
  return () => { unsubscribe(); clearInterval(timer); };
}
