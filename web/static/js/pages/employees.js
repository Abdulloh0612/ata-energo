// =============================================================================
// Страница "Сотрудники" (только admin и it_head).
// Здесь создаются учётные записи admin, it_head и dispatcher.
// Аккаунты клиентов создаются на странице "Клиенты".
// =============================================================================
import { api } from '../api.js';
import { store } from '../store.js';
import { confirmModal, esc, fmtDate, formModal, ROLE_LABELS, toast } from '../ui.js';

const STAFF_ROLES = Object.entries(ROLE_LABELS).filter(([role]) => role !== 'user');

export async function render(view) {
  const canManage = store.me.role === "admin";
  let roleOptions = STAFF_ROLES;
  view.innerHTML = `
    <div class="kpis">
      <div class="kpi" style="--c:var(--accent)"><small>Всего сотрудников</small><b data-k="all">—</b></div>
      <div class="kpi" style="--c:#f87171"><small>Администраторы</small><b data-k="admin">—</b></div>
      <div class="kpi" style="--c:var(--inv)"><small>Руководители IT</small><b data-k="it_head">—</b></div>
      <div class="kpi" style="--c:var(--pv)"><small>Диспетчеры</small><b data-k="dispatcher">—</b></div>
    </div>
    <div class="toolbar">
      <div class="seg" id="em-role">
        <button data-r="" class="active">Все</button><button data-r="admin">Админы</button>
        <button data-r="it_head">IT</button><button data-r="dispatcher">Диспетчеры</button>
      </div>
      <input type="search" id="em-search" placeholder="Поиск по имени, логину, телефону…">
      <span class="spacer"></span>
      ${canManage ? '<button class="btn btn-primary" id="em-add">Добавить сотрудника</button>' : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>Сотрудник</th><th>Логин</th><th>Роль</th><th>Должность</th><th>Телефон</th><th>Статус</th><th>Последний вход</th><th></th></tr></thead>
        <tbody id="em-body"><tr><td colspan="8" class="loading">Загрузка…</td></tr></tbody>
      </table></div>
    </div>`;

  const $ = (s) => view.querySelector(s);
  const body = $('#em-body');
  let users = [];
  let role = '';

  async function load() {
    users = await api('/api/users/', { params: { staff: 1 } });
    draw();
  }

  function draw() {
    // счётчики
    $('[data-k="all"]').textContent = users.length;
    for (const r of ['admin', 'it_head', 'dispatcher']) $(`[data-k="${r}"]`).textContent = users.filter((u) => u.role === r).length;

    const q = $('#em-search').value.trim().toLowerCase();
    const list = users.filter((u) => (!role || u.role === role)
      && (!q || `${u.username} ${u.first_name} ${u.last_name} ${u.phone}`.toLowerCase().includes(q)));
    if (!list.length) { body.innerHTML = '<tr><td colspan="8" class="empty">Никого не найдено</td></tr>'; return; }
    body.innerHTML = list.map((u) => {
      const name = `${u.first_name} ${u.last_name}`.trim() || u.username;
      return `<tr>
        <td><div class="user-box" style="padding:0"><span class="avatar">${esc(name[0].toUpperCase())}</span>
          <div><b>${esc(name)}</b><small>${esc(u.email || '')}</small></div></div></td>
        <td>${esc(u.username)}${u.id === store.me.id ? ' <span class="muted">(вы)</span>' : ''}</td>
        <td><span class="role role-${u.role}">${esc(roleOptions.find(([code]) => code === u.role)?.[1] || u.role_display)}</span></td>
        <td>${esc(u.position || '—')}</td>
        <td class="nowrap">${esc(u.phone || '—')}</td>
        <td>${u.is_active ? '<span class="badge online">активен</span>' : '<span class="badge offline">заблокирован</span>'}</td>
        <td class="muted nowrap">${u.last_login ? fmtDate(u.last_login) : 'не входил'}</td>
        <td class="actions">${canManage ? `<button class="btn btn-sm" data-edit="${u.id}">Изменить</button>` : ''}
          ${canManage && u.id !== store.me.id ? `<button class="btn btn-sm btn-danger" data-del="${u.id}">Удалить</button>` : ''}</td>
      </tr>`;
    }).join('');
  }

  function openForm(user) {
    formModal({
      title: user ? 'Изменить сотрудника' : 'Новый сотрудник',
      values: user || { role: 'dispatcher', is_active: true },
      fields: [
        { name: 'first_name', label: 'Имя', half: true },
        { name: 'last_name', label: 'Фамилия', half: true },
        { name: 'username', label: 'Логин', required: true, half: true },
        { name: 'password', label: 'Пароль', type: 'password', required: !user, half: true,
          help: user ? 'Пусто — не менять' : 'Минимум 6 символов' },
        { name: 'role', label: 'Роль', type: 'select', required: true, options: roleOptions, half: true },
        { name: 'position', label: 'Должность', half: true },
        { name: 'phone', label: 'Телефон', type: 'tel', half: true },
        { name: 'email', label: 'Email', type: 'email', half: true },
        { name: 'is_active', label: 'Активен (может входить)', type: 'checkbox' },
      ],
      onSubmit: async (data) => {
        if (!data.password) delete data.password;
        if (user) await api(`/api/users/${user.id}/`, { method: 'PATCH', body: data });
        else await api('/api/users/', { method: 'POST', body: data });
        toast('Сохранено', 'ok');
        await load();
      },
    });
  }

  if (canManage) $('#em-add').onclick = () => openForm(null);
  $('#em-search').oninput = draw;
  $('#em-role').onclick = (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    role = b.dataset.r;
    $('#em-role').querySelectorAll('button').forEach((x) => x.classList.toggle('active', x === b));
    draw();
  };
  body.onclick = async (e) => {
    const edit = e.target.closest('[data-edit]');
    const del = e.target.closest('[data-del]');
    if (edit) openForm(users.find((u) => u.id === Number(edit.dataset.edit)));
    if (del) {
      const u = users.find((x) => x.id === Number(del.dataset.del));
      if (!(await confirmModal(`Удалить сотрудника ${u.username}?`))) return;
      try { await api(`/api/users/${u.id}/`, { method: 'DELETE' }); toast('Удалено', 'ok'); await load(); }
      catch (err) { toast(err.message, 'error'); }
    }
  };

  if (canManage && document.documentElement.classList.contains('crm-enabled')) {
    const roles = await api('/api/crm/roles/');
    roleOptions = roles.map(role => [role.code, role.name]);
  }
  await load();
  return () => {};
}
