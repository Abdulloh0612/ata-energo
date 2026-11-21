// =============================================================================
// Страница "Аварии": пропажа связи и ошибки устройств. Новые появляются сразу.
// Сотрудники могут отмечать аварии прочитанными, клиент — только смотреть.
// =============================================================================
import { api } from '../api.js';
import { store } from '../store.js';
import { esc, fmtDate, toast } from '../ui.js';

export async function render(view, { refreshBell }) {
  const staff = store.me.can_view_all;
  view.innerHTML = `
    <div class="toolbar">
      <div class="seg" id="al-filter"><button data-f="" class="active">Все</button><button data-f="1">Непрочитанные</button></div>
      <span class="spacer"></span>
      ${staff ? '<button class="btn" id="al-readall">Отметить все прочитанными</button>' : ''}
    </div>
    <div class="card"><div class="alerts" id="al-list"><div class="loading">Загрузка…</div></div></div>`;

  const $ = (s) => view.querySelector(s);
  const list = $('#al-list');
  let unreadOnly = '';

  const html = (a) => `
    <div class="alert kind-${a.kind} ${a.is_read ? 'read' : ''}" data-id="${a.id}">
      <div>
        <b>${esc(a.text)}</b><br>
        <small>${fmtDate(a.created_at)}${staff ? ` · <a href="#/client/${a.client_id}">${esc(a.client_name)}</a>` : ''}
        · ${a.kind === 'offline' ? 'нет связи' : 'ошибка'}</small>
      </div>
      ${staff && !a.is_read ? `<button class="btn btn-sm" data-read="${a.id}">Прочитано</button>` : ''}
    </div>`;

  async function load() {
    const items = await api('/api/alerts/', { params: { unread: unreadOnly } });
    list.innerHTML = items.length ? items.map(html).join('') : '<div class="empty">Аварий нет 👍</div>';
  }

  $('#al-filter').onclick = (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    unreadOnly = b.dataset.f;
    $('#al-filter').querySelectorAll('button').forEach((x) => x.classList.toggle('active', x === b));
    load();
  };
  $('#al-readall')?.addEventListener('click', async () => {
    await api('/api/alerts/read_all/', { method: 'POST' });
    toast('Все аварии отмечены', 'ok');
    await refreshBell();
    load();
  });
  list.onclick = async (e) => {
    const b = e.target.closest('[data-read]');
    if (!b) return;
    try {
      await api(`/api/alerts/${b.dataset.read}/read/`, { method: 'POST' });
      b.closest('.alert').classList.add('read');
      b.remove();
      await refreshBell();
    } catch (err) { toast(err.message, 'error'); }
  };

  // новая авария приходит по WebSocket — добавляем наверх
  const unsubscribe = store.subscribe((e) => {
    if (e.type !== 'alert') return;
    const a = { ...e.alert, client_id: e.alert.device.client_id };
    if (list.querySelector('.empty, .loading')) list.innerHTML = '';
    list.insertAdjacentHTML('afterbegin', html(a));
    while (list.children.length > 200) list.lastElementChild.remove();
  });

  await load();
  return unsubscribe;
}
