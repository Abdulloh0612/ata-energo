import { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { IntegrationsPage, ProviderBadge, SitePage, SitesPage } from './providers';
import { EquipmentPage, SettingsPage } from './equipment';
import { RolesPage } from './roles';
import { loadProviderIndex } from '../static/js/provider-filters.js';
import { store } from '../static/js/store.js';
import { api } from '../static/js/api.js';
import { confirmModal, toast } from '../static/js/ui.js';

type Kind = 'requests' | 'tasks';
type Permission = { create: boolean; edit: boolean; delete: boolean; assign: boolean; relations: boolean };
type History = { id: number; actor_id: number | null; action: string; created_at: string };
type Work = {
  id: number; title: string; description: string; status: string; priority: string;
  client_id: number | null; device_id: number | null; assignee_id: number | null;
  due_at: string | null; updated_at: string; history?: History[];
};
type Activity = History & { work_id: number | null; snapshot: Work & { kind: Kind } };
const statuses: Record<string, string> = { open: 'Открыта', progress: 'В работе', closed: 'Закрыта' };
const priorities: Record<string, string> = { low: 'Низкий', normal: 'Обычный', high: 'Высокий' };
const actions: Record<string, string> = { create: 'Создание', update: 'Изменение', delete: 'Удаление' };
const date = (value: string | null) => value ? new Date(value).toLocaleString('ru-KG') : '—';
const endpoint = (kind: Kind) => '/api/crm/' + kind;
const errorText = (error: unknown) => error instanceof Error ? error.message : String(error);
const options = (values: Record<string, string>) => Object.entries(values).map(([key, label]) => <option key={key} value={key}>{label}</option>);

function StatusBadge({ value }: { value: string }) {
  return <span className={`badge ${value === 'closed' ? 'online' : value === 'progress' ? 'warn' : 'offline'}`}>{statuses[value]}</span>;
}

type ProviderIndex = Awaited<ReturnType<typeof loadProviderIndex>>;
function WorkList({ kind, rows, permission, index }: { kind: Kind; rows: Work[]; permission: Permission; index: ProviderIndex }) {
  const query = new URLSearchParams(location.hash.split('?')[1]);
  const provider = query.get('provider') || '';
  const codesFor = (row: Work): string[] => {
    const device = row.device_id == null ? undefined : store.devices.get(row.device_id);
    if (device) return [index.modems.get(device.modem) || 'local'];
    return row.client_id == null ? [] : Array.from(index.clients.get(row.client_id) || []);
  };
  const state = query.get('status') || '';
  const client = query.get('client') || '';
  const equipment = query.get('device') || '';
  const text = query.get('q') || '';
  const priority = query.get('priority') || '';
  const sort = query.get('sort') || 'updated';
  const page = Math.max(1, Number(query.get('page')) || 1);
  const overdue = (row: Work) => row.status !== 'closed' && row.due_at && Date.parse(row.due_at) < Date.now();
  const list = rows.filter(row => (!state || (state === 'overdue' ? overdue(row) : state === row.status))
    && (!client || String(row.client_id) === client) && (!equipment || String(row.device_id) === equipment)
    && (!provider || codesFor(row).includes(provider)) && (!priority || row.priority === priority) && row.title.toLowerCase().includes(text.trim().toLowerCase()))
    .sort((a, b) => sort === 'title' ? a.title.localeCompare(b.title, 'ru') : Date.parse(b.updated_at) - Date.parse(a.updated_at));
  const [, refresh] = useState(0);
  function filter(key: string, value: string) {
    if (value) query.set(key, value); else query.delete(key);
    if (key !== 'page') query.delete('page');
    const suffix = query.toString();
    history.replaceState(null, '', `#/${kind}${suffix ? '?' + suffix : ''}`);
    refresh(count => count + 1);
  }
  const kpis = [['', 'Всего', rows.length], ...Object.entries(statuses).map(([key, label]) =>
    [key, label, rows.filter(row => row.status === key).length]), ['overdue', 'Просрочено', rows.filter(overdue).length]] as [string, string, number][];
  const last = Math.max(1, Math.ceil(list.length / 20));
  const current = Math.min(last, page);
  return <section className="crm-list">
    <div className="crm-head"><h2>{kind === 'requests' ? 'Сервисные заявки' : 'Задачи'} <small>{rows.length}</small></h2>
      {permission.create && <a className="btn btn-primary" href={`#/${kind}/new`}>Добавить</a>}</div>
    <div className="crm-kpis">{kpis.map(([key, label, count]) => <button type="button" className="overview-metric" key={key} aria-pressed={state === key} onClick={() => filter('status', key)}><span>{label}</span><strong>{count}</strong></button>)}</div>
    <div className="toolbar">
      {client && <button className="btn" type="button" onClick={() => filter('client', '')}>Клиент №{client} ×</button>}
      {equipment && <button className="btn" type="button" onClick={() => filter('device', '')}>Оборудование №{equipment} ×</button>}
      <input type="search" aria-label="Поиск по названию" placeholder="Поиск по названию" value={text} onChange={e => filter('q', e.target.value)} />
      <select aria-label="Статус" value={state} onChange={e => filter('status', e.target.value)}><option value="">Все статусы</option>{options(statuses)}<option value="overdue">Просрочено</option></select>
      <select aria-label="Приоритет" value={priority} onChange={e => filter('priority', e.target.value)}><option value="">Все приоритеты</option>{options(priorities)}</select>
      <select aria-label="Провайдер" value={provider} onChange={e=>filter('provider',e.target.value)}><option value="">Все провайдеры</option>
      {index.providers.map((p:{code:string;name:string})=><option key={p.code} value={p.code}>{p.name}</option>)}</select>
      <select aria-label="Сортировка" value={sort} onChange={e => filter('sort', e.target.value)}><option value="updated">Последние изменения</option><option value="title">По названию</option></select>
      {(state || text || priority || provider || sort !== 'updated') && <button className="btn btn-ghost" type="button" onClick={() => { history.replaceState(null, '', `#/${kind}`); refresh(count => count + 1); }}>Сбросить</button>}
    </div>
    {list.length ? <div className="card crm-table"><table><thead><tr><th>Название</th><th>Статус</th><th>Приоритет</th><th>Срок</th><th>Клиент</th><th>Провайдер</th></tr></thead>
      <tbody>{list.slice((current - 1) * 20, current * 20).map(row => <tr key={row.id}>
        <td><a href={`#/${kind}/${row.id}`}>{row.title}</a></td><td><StatusBadge value={row.status}/></td>
        <td>{priorities[row.priority]}</td><td>{date(row.due_at)}</td>
        <td>{row.client_id ? <a href={`#/client/${row.client_id}`}>Клиент №{row.client_id}</a> : '—'}</td>
        <td>{codesFor(row).map(code=><ProviderBadge key={code} code={code} name={index.providers.find((p:{code:string})=>p.code===code)?.name||code}/>)}</td>
      </tr>)}</tbody></table></div> : <p className="card empty">{rows.length ? 'По этим фильтрам ничего не найдено' : 'Пока нет записей'}</p>}
    {last > 1 && <div className="toolbar"><button className="btn" disabled={current <= 1} onClick={() => filter('page', String(current - 1))}>Назад</button><span>{current} / {last}</span><button className="btn" disabled={current >= last} onClick={() => filter('page', String(current + 1))}>Далее</button></div>}
  </section>;
}

type Choice = { id: number; name?: string; username?: string; client?: number };
function WorkCard({ kind, row, permission }: { kind: Kind; row?: Work; permission: Permission }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [choices, setChoices] = useState<{ clients: Choice[]; devices: Choice[]; staff: Choice[] }>({ clients: [], devices: [], staff: [] });
  const [client, setClient] = useState(String(row?.client_id ?? ''));
  const [device, setDevice] = useState(String(row?.device_id ?? ''));
  const allowEdit = row ? permission.edit : permission.create;
  useEffect(() => {
    if (!allowEdit || !permission.relations) return;
    let active = true;
    Promise.all([api('/api/clients/'), api('/api/devices/'), permission.assign ? api('/api/users/', { params: { staff: 1 } }) : []])
      .then(([clients, devices, staff]) => { if (active) setChoices({ clients, devices, staff }); })
      .catch(err => { if (active) setError(errorText(err)); });
    return () => { active = false; };
  }, [allowEdit, permission.assign, permission.relations]);

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setError('');
    const data = new FormData(event.currentTarget);
    const number = (key: string) => data.get(key) ? Number(data.get(key)) : null;
    const due = data.get('due_at');
    try {
      const saved = await api(endpoint(kind) + (row ? '/' + row.id : '/'), { method: row ? 'PUT' : 'POST', body: {
        title: data.get('title'), description: data.get('description'), status: data.get('status'), priority: data.get('priority'),
        client_id: Number(client) || null, device_id: Number(device) || null, assignee_id: number('assignee_id'),
        due_at: due ? new Date(String(due)).toISOString() : null,
      } });
      toast('Запись сохранена');
      location.hash = row ? `#/${kind}` : `#/${kind}/${saved.id}`;
    } catch (err) { setError(errorText(err)); }
    finally { setPending(false); }
  }
  async function remove() {
    if (!row || !await confirmModal('Удалить запись?')) return;
    setPending(true);
    setError('');
    try { await api(endpoint(kind) + '/' + row.id, { method: 'DELETE' }); toast('Запись удалена'); location.hash = `#/${kind}`; }
    catch (err) { setError(errorText(err)); }
    finally { setPending(false); }
  }
  const dueLocal = row?.due_at ? new Date(Date.parse(row.due_at) - new Date(row.due_at).getTimezoneOffset()*60000).toISOString().slice(0,16) : '';
  return <section className="crm-list">
    <div className="crm-head"><h2>{row?.title || (kind === 'tasks' ? 'Новая задача' : 'Новая заявка')}</h2><a className="btn" href={`#/${kind}`}>К списку</a></div>
    {row && <div className="toolbar">
      <StatusBadge value={row.status}/>
      {row.client_id && <a href={`#/client/${row.client_id}`}>Клиент №{row.client_id}</a>}
      {row.device_id && <a href={`#/equipment/${row.device_id}`}>Оборудование №{row.device_id}</a>}
      {row.assignee_id && <span>Исполнитель №{row.assignee_id}</span>}
    </div>}
    {error && <p className="form-error" role="alert">{error}</p>}
    {allowEdit ? <form className="card crm-form" onSubmit={save}>
      <label>Название<input name="title" maxLength={160} required defaultValue={row?.title}/></label>
      <label>Описание<textarea name="description" maxLength={20000} rows={4} defaultValue={row?.description}/></label>
      <div className="crm-fields">
        <label>Статус<select name="status" defaultValue={row?.status || 'open'}>{options(statuses)}</select></label>
        <label>Приоритет<select name="priority" defaultValue={row?.priority || 'normal'}>{options(priorities)}</select></label>
        <label>Срок<input type="datetime-local" name="due_at" defaultValue={dueLocal}/></label>
        <label>Клиент<select disabled={!permission.relations} value={client} onChange={e => {setClient(e.target.value);setDevice('');}}><option value="">Без клиента</option>{client && !choices.clients.some(x => String(x.id) === client) && <option value={client}>Клиент №{client}</option>}{choices.clients.map(x => <option key={x.id} value={x.id}>{x.name}</option>)}</select></label>
        <label>Оборудование<select disabled={!permission.relations} value={device} onChange={e => setDevice(e.target.value)}><option value="">Без оборудования</option>{device && !choices.devices.some(x => String(x.id) === device) && <option value={device}>Оборудование №{device}</option>}{choices.devices.filter(x => String(x.client) === client).map(x => <option key={x.id} value={x.id}>{x.name || `Оборудование №${x.id}`}</option>)}</select></label>
        {permission.assign && <label>Исполнитель<select name="assignee_id" defaultValue={row?.assignee_id ?? ''}><option value="">Не назначен</option>{row?.assignee_id && !choices.staff.some(x => x.id === row.assignee_id) && <option value={row.assignee_id}>Сотрудник №{row.assignee_id}</option>}{choices.staff.map(x => <option key={x.id} value={x.id}>{x.username}</option>)}</select></label>}
      </div>
      <div className="toolbar"><button className="btn btn-primary" disabled={pending}>{pending ? 'Сохранение…' : 'Сохранить'}</button>
        {row && permission.delete && <button type="button" className="btn btn-danger" disabled={pending} onClick={remove}>Удалить</button>}</div>
    </form> : <div className="card"><p>{row?.description || 'Описание отсутствует'}</p><p>Приоритет {priorities[row?.priority || 'normal']} · Срок {date(row?.due_at ?? null)}</p></div>}
    {row && <section className="card"><h3>История изменений</h3>{row.history?.map(entry => <p key={entry.id}>{actions[entry.action]} · {date(entry.created_at)} · Сотрудник №{entry.actor_id ?? '—'}</p>)}</section>}
  </section>;
}

function ActivityList({ rows }: { rows: Activity[] }) {
  const [filter, setFilter] = useState('');
  const visible = rows.filter(row => !filter || row.action === filter);
  return <section className="crm-list"><div className="crm-head"><h2>Журнал действий</h2><span>{rows.length} записей</span></div>
    <select aria-label="Действие" value={filter} onChange={e => setFilter(e.target.value)}><option value="">Все действия</option>{options(actions)}</select>
    {visible.length ? <div className="card crm-table"><table><thead><tr><th>Запись</th><th>Действие</th><th>Сотрудник</th><th>Дата</th></tr></thead><tbody>
      {visible.map(row => <tr key={row.id}><td>{row.work_id ? <a href={`#/${row.snapshot.kind}/${row.work_id}`}>{row.snapshot.title}</a> : row.snapshot.title}</td><td>{actions[row.action]}</td><td>№{row.actor_id ?? '—'}</td><td>{date(row.created_at)}</td></tr>)}
    </tbody></table></div> : <p className="card empty">Действий пока нет</p>}</section>;
}

export async function renderWork(view: HTMLElement, path: string) {
  const [, segment, id] = path.split('/');
  let content;
  if (segment === 'sites') {
    if (id) {
      const [initial,me]=await Promise.all([api('/api/crm/sites/'+id),api('/api/me/')]);
      content=<SitePage initial={initial} canLink={me.can_edit||me.role==='manager'}/>;
    } else {
      const [sites,providers]=await Promise.all([api('/api/crm/sites'),api('/api/crm/providers')]);
      content=<SitesPage sites={sites} providers={providers}/>;
    }
  } else if (segment === 'integrations') {
    const [initialProviders,initialAccounts,initialEvents]=await Promise.all([api('/api/crm/providers'),api('/api/crm/integrations/accounts'),api('/api/crm/integrations/activity')]);
    content=<IntegrationsPage initialProviders={initialProviders} initialAccounts={initialAccounts} initialEvents={initialEvents} newAccount={id==='new'?'new':id?Number(id):undefined}/>;
  } else if (segment === 'profile') {
    const info = await api('/api/me/');
    content = <section className="card"><h2>{[info.first_name,info.last_name].filter(Boolean).join(' ') || info.username}</h2>
      <dl><dt>Логин</dt><dd>{info.username}</dd><dt>Роль</dt><dd>{info.role_display || info.role}</dd>
      <dt>Должность</dt><dd>{info.position || '—'}</dd><dt>Email</dt><dd>{info.email ? <a href={`mailto:${info.email}`}>{info.email}</a> : '—'}</dd>
      <dt>Телефон</dt><dd>{info.phone ? <a href={`tel:${String(info.phone).replace(/[^+0-9]/g,'')}`}>{info.phone}</a> : '—'}</dd></dl></section>;
  } else if (segment === 'roles') {
    content = <RolesPage initial={await api('/api/crm/roles/')}/>;
  } else if (segment === 'activity') {
    content = <ActivityList rows={await api('/api/crm/activity')}/>;
  } else if (segment === 'settings') {
    const settings = await api('/api/crm/settings/');
    content = <SettingsPage initial={settings.low_power_percent}/>;
  } else if (segment === 'equipment') {
    const [device, meta] = await Promise.all([api(`/api/devices/${id}/`), api(`/api/crm/equipment/${id}`)]);
    content = <EquipmentPage device={device} initial={meta}/>;
  } else {
    const kind = segment as Kind;
    const permission: Permission = await api(endpoint(kind) + '/permissions');
    if (id === 'new' && !permission.create) {
      content = <p className="card empty">Нет доступа к созданию записи</p>;
    } else if (id) {
      const row = id === 'new' ? undefined : await api(endpoint(kind) + '/' + id);
      content = <WorkCard kind={kind} row={row} permission={permission}/>;
    } else {
      const [rows,index]=await Promise.all([api(endpoint(kind)+'/'),loadProviderIndex()]);
      content = <WorkList kind={kind} rows={rows} permission={permission} index={index}/>;
    }
  }
  if (!view.isConnected) return;
  const root = createRoot(view);
  root.render(content);
  return () => root.unmount();
}
