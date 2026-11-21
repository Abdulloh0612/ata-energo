// =============================================================================
// Страница "Клиенты".
// admin / it_head: добавлять, изменять, удалять. dispatcher: только просмотр.
// =============================================================================
import { api } from '../api.js';
import { store } from '../store.js';
import { confirmModal, esc, formModal, statusBadge, toast } from '../ui.js';

export async function render(view) {
  const canEdit = store.me.can_edit;
  view.innerHTML = `
    <div class="toolbar">
      <input type="search" id="cl-search" placeholder="Поиск по имени, телефону, адресу, SN модема…">
      <span class="spacer"></span>
      ${canEdit ? '<button class="btn btn-primary" id="cl-add">+ Добавить клиента</button>' : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>Клиент</th><th>Телефон</th><th>Адрес</th><th>Логин</th><th>Модем (SN)</th><th>Устройства</th><th></th></tr></thead>
        <tbody id="cl-body"><tr><td colspan="7" class="loading">Загрузка…</td></tr></tbody>
      </table></div>
    </div>`;

  const body = view.querySelector('#cl-body');
  let clients = [];

  async function load() {
    clients = await api('/api/clients/', { params: { search: view.querySelector('#cl-search').value.trim() } });
    draw();
  }

  function draw() {
    if (!clients.length) { body.innerHTML = '<tr><td colspan="7" class="empty">Ничего не найдено</td></tr>'; return; }
    body.innerHTML = clients.map((c) => `
      <tr>
        <td><b>${esc(c.name)}</b></td>
        <td class="nowrap">${esc(c.phone || '—')}</td>
        <td>${esc(c.address || '—')}</td>
        <td>${c.username ? esc(c.username) : '<span class="muted">нет входа</span>'}</td>
        <td class="nowrap">${c.modems.length ? c.modems.map((m) => `${esc(m.serial)} ${statusBadge(m.status)}`).join('<br>') : '<span class="muted">не указан</span>'}</td>
        <td class="nowrap"><span class="badge ${c.online_count === c.devices_count && c.devices_count ? 'online' : c.online_count ? 'warn' : 'offline'}">${c.online_count}/${c.devices_count} в сети</span></td>
        <td class="actions">
          <a class="btn btn-sm" href="#/client/${c.id}">Открыть</a>
          <a class="btn btn-sm" href="#/devices?client=${c.id}">Оборудование</a>
          ${canEdit ? `<button class="btn btn-sm" data-edit="${c.id}">Изменить</button>
                       <button class="btn btn-sm btn-danger" data-del="${c.id}">Удалить</button>` : ''}
        </td>
      </tr>`).join('');
  }

  // Форма добавления/изменения клиента
  function openForm(client) {
    formModal({
      title: client ? 'Изменить клиента' : 'Новый клиент',
      values: client ? {
        ...client,
        login_username: client.username || '',
        modem_serial: client.modems[0]?.serial || '',
        modem_name: client.modems[0]?.name || '',
        has_generator: client.modems[0]?.has_generator || false,
      } : {},
      fields: [
        { name: 'name', label: 'Название / ФИО', required: true },
        { name: 'phone', label: 'Телефон', type: 'tel', half: true, placeholder: '+996 …' },
        { name: 'address', label: 'Адрес', half: true },
        { name: 'modem_serial', label: 'SN модема (номер под QR-кодом)', half: true, placeholder: 'например 2712345678',
          help: 'Инвертор, панели, батарея, сеть и нагрузка добавятся сами' },
        { name: 'modem_name', label: 'Модель инвертора', half: true, placeholder: 'например Deye 8 кВт' },
        { name: 'has_generator', label: 'У клиента есть генератор', type: 'checkbox' },
        { name: 'login_username', label: 'Логин для входа клиента', half: true, help: 'Можно оставить пустым' },
        { name: 'login_password', label: 'Пароль', type: 'password', half: true,
          help: client ? 'Пусто — не менять' : 'Минимум 6 символов' },
      ],
      onSubmit: async (data) => {
        if (!data.login_password) delete data.login_password;
        let saved;
        if (client) saved = await api(`/api/clients/${client.id}/`, { method: 'PATCH', body: data });
        else saved = await api('/api/clients/', { method: 'POST', body: data });
        // обновляем устройства и модемы (сервер мог создать новые)
        const [devices, modems] = await Promise.all([api('/api/devices/'), api('/api/modems/')]);
        store.setDevices(devices);
        store.setModems(modems);
        toast(client ? 'Клиент сохранён' : `Клиент «${saved.name}» добавлен`, 'ok');
        await load();
      },
    });
  }

  view.querySelector('#cl-add')?.addEventListener('click', () => openForm(null));
  body.onclick = async (e) => {
    const edit = e.target.closest('[data-edit]');
    const del = e.target.closest('[data-del]');
    if (edit) openForm(clients.find((c) => c.id === Number(edit.dataset.edit)));
    if (del) {
      const c = clients.find((x) => x.id === Number(del.dataset.del));
      if (!(await confirmModal(`Удалить клиента «${c.name}» вместе со всем оборудованием и историей?`))) return;
      try {
        await api(`/api/clients/${c.id}/`, { method: 'DELETE' });
        store.devicesOfClient(c.id).forEach((d) => store.removeDevice(d.id));
        toast('Клиент удалён', 'ok');
        await load();
      } catch (err) { toast(err.message, 'error'); }
    }
  };

  let t;
  view.querySelector('#cl-search').oninput = () => { clearTimeout(t); t = setTimeout(load, 300); };
  await load();
  return () => clearTimeout(t);
}
