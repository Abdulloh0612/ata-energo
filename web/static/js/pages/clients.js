// =============================================================================
// Страница "Клиенты".
// admin / it_head: добавлять, изменять, удалять. dispatcher: только просмотр.
// =============================================================================
import { api } from '../api.js';
import { loadProviderIndex, providerOptions, providerBadges } from '../provider-filters.js';
import { equipmentCondition } from '../equipment-status.js';
import { store } from '../store.js';
import { confirmModal, esc, formModal, statusBadge, toast } from '../ui.js';

export async function render(view, { query }) {
  const canEdit = store.me.can_edit || store.me.role === 'manager';
  const providerIndex = document.documentElement.classList.contains('crm-enabled') ? await loadProviderIndex() : null;
  const canDelete = store.me.role === 'admin';
  view.innerHTML = `
    <div class="crm-kpis" id="cl-kpis"></div>
    <div class="toolbar">
      <input type="search" id="cl-search" placeholder="Поиск по имени, телефону, адресу, SN модема…">
      <select id="cl-status" aria-label="Статус"><option value="">Все статусы</option><option value="online">В норме</option><option value="attention">Требуют внимания</option><option value="problem">Проблемные</option><option value="inactive">Без активности 30+ дней</option><option value="new">Новые за месяц</option><option value="empty">Без оборудования</option></select>
      ${providerIndex ? `<select id="cl-provider" aria-label="Провайдер">${providerOptions(providerIndex.providers)}</select>` : ''}
      <select id="cl-sort" aria-label="Сортировка"><option value="">По имени</option><option value="devices">По количеству оборудования</option></select>
      <span class="spacer"></span>
      ${canEdit ? '<button class="btn btn-primary" id="cl-add">+ Добавить клиента</button>' : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>Клиент</th>${providerIndex ? "<th>Провайдер</th>" : ""}<th>Телефон</th><th>Адрес</th><th>Логин</th><th>Модем (SN)</th><th>Устройства</th><th></th></tr></thead>
        <tbody id="cl-body"><tr><td colspan="7" class="loading">Загрузка…</td></tr></tbody>
      </table></div>
    </div>`;

  const body = view.querySelector('#cl-body');
  let clients = [];
  const search = view.querySelector('#cl-search');
  const status = view.querySelector('#cl-status');
  const provider = view.querySelector('#cl-provider');
  if (provider) provider.value = query.get('provider') || '';
  const sort = view.querySelector('#cl-sort');
  search.value = query.get('q') || '';
  status.value = query.get('status') || '';
  sort.value = query.get('sort') || '';
  let profiles = new Map();
  const states = {online:'В норме',problem:'Есть проблемы',attention:'Низкая мощность',inactive:'Без активности 30+ дней',new:'Новые за месяц',empty:'Без оборудования'};
  const match = (client, key) => {
    if (!key) return true;
    const devices = store.devicesOfClient(client.id);
    const conditions = devices.map(device => equipmentCondition(device, profiles.get(device.id)));
    if (key === 'inactive') return devices.length && devices.every(device => !device.last_seen || Date.parse(device.last_seen) < Date.now()-30*86400000);
    if (key === 'new') return Date.parse(client.created_at) >= Date.now()-30*86400000;
    if (key === 'problem') return conditions.includes('problem') || conditions.includes('offline');
    if (key === 'attention') return conditions.includes('low');
    if (key === 'empty') return !devices.length;
    return devices.length && conditions.every(condition => condition === 'normal');
  };

  async function load() {
    const [list, metadata] = await Promise.all([api('/api/clients/'),
      document.documentElement.classList.contains('crm-enabled') ? api('/api/crm/equipment/') : []]);
    clients = list;
    profiles = new Map(metadata.map(profile => [profile.device_id, profile]));
    draw();
  }

  function draw() {
    const metrics = [['', 'Всего клиентов'], ...Object.entries(states)];
    view.querySelector('#cl-kpis').innerHTML = metrics.map(([key, label]) =>
      `<button type="button" class="overview-metric" data-status="${key}" aria-pressed="${(status.value || '') === key}"><span>${label}</span><strong>${key ? clients.filter(c => match(c, key)).length : clients.length}</strong></button>`).join('');
    const q = search.value.trim().toLowerCase();
    const visible = clients.filter(c => match(c, status.value)
      && (!provider?.value || providerIndex.clients.get(c.id)?.has(provider.value))
      && [c.name, c.phone, c.address, ...c.modems.map(m => m.serial)].some(value => value?.toLowerCase().includes(q)))
      .sort((a,b) => sort.value === 'devices' ? b.devices_count - a.devices_count : a.name.localeCompare(b.name,'ru'));
    if (!visible.length) { body.innerHTML = '<tr><td colspan="7" class="empty">Ничего не найдено</td></tr>'; return; }
    body.innerHTML = visible.map((c) => `
      <tr>
        <td><a href="#/client/${c.id}"><strong>${esc(c.name)}</strong></a></td>
        ${providerIndex ? `<td>${providerBadges(providerIndex,providerIndex.clients.get(c.id)) || '—'}</td>` : ''}
        <td class="nowrap">${esc(c.phone || '—')}</td>
        <td>${esc(c.address || '—')}</td>
        <td>${c.username ? esc(c.username) : '<span class="muted">нет входа</span>'}</td>
        <td class="nowrap">${c.modems.length ? c.modems.map((m) => `${esc(m.serial)} ${statusBadge(m.status)}`).join('<br>') : '<span class="muted">не указан</span>'}</td>
        <td class="nowrap"><span class="badge ${c.online_count === c.devices_count && c.devices_count ? 'online' : c.online_count ? 'warn' : 'offline'}">${c.online_count}/${c.devices_count} в сети</span></td>
        <td class="actions">
          <a class="btn btn-sm" href="#/client/${c.id}">Открыть</a>
          <a class="btn btn-sm" href="#/devices?client=${c.id}">Оборудование</a>
          ${canEdit ? `<button class="btn btn-sm" data-edit="${c.id}">Изменить</button>` : ''}
          ${canDelete ? `<button class="btn btn-sm btn-danger" data-del="${c.id}">Удалить</button>` : ''}
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

  function updateFilters() {
    const filters = new URLSearchParams();
    for (const [key, field] of [['q', search], ['status', status], ['sort', sort], ['provider',provider]]) {
      if (field?.value) filters.set(key, field.value);
    }
    history.replaceState(null, '', '#/clients' + (filters.size ? '?' + filters : ''));
    draw();
  }
  view.querySelector('#cl-kpis').onclick = event => {
    const button = event.target.closest('button[data-status]');
    if (button) { status.value = button.dataset.status; updateFilters(); }
  };
  if (provider) provider.onchange = updateFilters;
  search.oninput = status.onchange = sort.onchange = updateFilters;
  await load();
  return () => {};
}
