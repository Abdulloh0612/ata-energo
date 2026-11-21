import { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { loadProviderIndex } from '../static/js/provider-filters.js';
import { api } from '../static/js/api.js';
import { summarize } from '../static/js/flow.js';
import { store } from '../static/js/store.js';
import { fmtPower } from '../static/js/ui.js';

type Client = { id: number; name: string; address: string; phone: string };
const number = new Intl.NumberFormat('ru-KG');

type Index = Awaited<ReturnType<typeof loadProviderIndex>>;
function readOverview(clients: Client[], index: Index, provider: string) {
  const includes = (device: {modem?: number}) => !provider || (index.modems.get(device.modem) || 'local') === provider;
  let devices = 0;
  let online = 0;
  let solar = 0;
  let load = 0;
  for (const device of store.devices.values()) {
    if (!includes(device)) continue;
    devices++;
    if (device.status !== 'online') continue;
    online++;
    const power = Number(device.last_data?.power) || 0;
    if (device.device_type === 'panel') solar += power;
    if (device.device_type === 'load') load += power;
  }

  const stations = clients.map(client => {
    const summary = summarize(store.devicesOfClient(client.id).filter(includes));
    const gridDown = summary.grid.online && (summary.grid.error || !summary.grid.voltage);
    const problem = summary.total > 0 && Boolean(
      summary.online < summary.total || gridDown || summary.bat.error || summary.inv.error,
    );
    return { client, summary, gridDown, problem };
  });

  return {
    stations, online, solar, load, devices, alerts: store.unreadAlerts,
    problems: stations.filter(station => station.problem).length,
  };
}

type WorkRow = {id: number; title: string; status: string; due_at: string | null; assignee_id: number | null};

function Overview({ clients, requests, tasks, index }: { clients: Client[]; requests: WorkRow[]; tasks: WorkRow[]; index: Index }) {
  const [provider,setProvider]=useState(new URLSearchParams(location.hash.split('?')[1]).get('provider') || '');
  const [data, setData] = useState(() => readOverview(clients,index,provider));
  const visibleClients = provider ? clients.filter(client=>index.clients.get(client.id)?.has(provider)) : clients;
  function selectProvider(value:string) {
    history.replaceState(null,'','#/dashboard'+(value?'?provider='+encodeURIComponent(value):''));
    setProvider(value);
  }
  const [search, setSearch] = useState('');
  const [onlyProblems, setOnlyProblems] = useState(false);

  useEffect(() => {
    let frame = 0;
    const refresh = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        setData(readOverview(visibleClients,index,provider));
      });
    };
    const unsubscribe = store.subscribe(refresh);
    refresh();
    return () => { unsubscribe(); cancelAnimationFrame(frame); };
  }, [clients,index,provider]);

  const now = new Date();
  const overdue = requests.filter(row => row.status !== 'closed' && row.due_at && Date.parse(row.due_at) < now.getTime());
  const today = tasks.filter(row => row.status !== 'closed' && row.assignee_id === store.me?.id
    && row.due_at && new Date(row.due_at).toDateString() === now.toDateString());
  const query = search.trim().toLowerCase();
  const stations = data.stations.filter(({ client, problem }) =>
    (!onlyProblems || problem) && [client.name, client.address, client.phone]
      .some(value => value?.toLowerCase().includes(query)),
  );

  return (
    <>
      <section className="overview-hero" aria-labelledby="overview-heading">
        <div className="overview-intro">
          <h2 id="overview-heading">Солнечные станции</h2>
          <p>Состояние оборудования и выработка в реальном времени</p>
          <a className="btn hero-link" href="#/clients">Открыть список клиентов <span aria-hidden="true">↗</span></a>
        </div>
        <div className="overview-production">
          <span>Текущая выработка</span>
          <strong>{fmtPower(data.solar)}</strong>
          <small>Потребление {fmtPower(data.load)}</small>
        </div>
      </section>

      <div className="toolbar"><label>Провайдер<select value={provider} onChange={e=>selectProvider(e.target.value)}>
        <option value="">Все провайдеры</option>{index.providers.map((p:{code:string;name:string})=><option key={p.code} value={p.code}>{p.name}</option>)}
      </select></label></div>
      <div className="crm-kpis">{index.providers.map((p:{code:string;name:string;sites_count:number})=><button key={p.code}
        className="overview-metric" aria-pressed={provider===p.code} onClick={()=>selectProvider(provider===p.code?'':p.code)}><span>{p.name}</span><strong>{p.sites_count}</strong></button>)}</div>
      <div className="overview-metrics">
        <a className="overview-metric" href="#/clients">
          <span>Клиенты</span><strong>{number.format(visibleClients.length)}</strong>
        </a>
        <a className="overview-metric" href="#/devices">
          <span>Оборудование онлайн</span><strong>{data.online} <small>/ {data.devices}</small></strong>
        </a>
        <button className="overview-metric" type="button" aria-pressed={onlyProblems} onClick={() => setOnlyProblems(value => !value)}>
          <span>Станции с проблемами</span><strong>{data.problems}</strong>
        </button>
        <a className="overview-metric" href="#/alerts">
          <span>Новые аварии</span><strong>{data.alerts}</strong>
        </a>
      </div>

      {(store.crmAccess.requests?.view || store.crmAccess.tasks?.view) && <div className="crm-fields">
        {store.crmAccess.requests?.view && <section className="card">
          <div className="card-head"><h2>Просроченные заявки</h2><span className="spacer"/><a href="#/requests?status=overdue">Все ({overdue.length})</a></div>
          {overdue.length ? <ul>{overdue.slice(0, 4).map(row => <li key={row.id}><a href={`#/requests/${row.id}`}>{row.title}</a></li>)}</ul> : <p className="muted">Просроченных заявок нет</p>}
        </section>}
        {store.crmAccess.tasks?.view && <section className="card">
          <div className="card-head"><h2>Мои задачи на сегодня</h2><span className="spacer"/><a href="#/tasks">Все задачи</a></div>
          {today.length ? <ul>{today.slice(0, 4).map(row => <li key={row.id}><a href={`#/tasks/${row.id}`}>{row.title}</a></li>)}</ul> : <p className="muted">На сегодня нет задач</p>}
        </section>}
      </div>}

      <section className="overview-stations" aria-labelledby="stations-heading">
        <div className="overview-section-head">
          <h2 id="stations-heading">Станции <small>{stations.length}</small></h2>
          <div className="toolbar">
            <input type="search" aria-label="Поиск клиента" placeholder="Имя, адрес или телефон" value={search} onChange={event => setSearch(event.target.value)} />
            <select aria-label="Состояние станций" value={onlyProblems ? 'problem' : 'all'} onChange={event => setOnlyProblems(event.target.value === 'problem')}>
              <option value="all">Все станции</option>
              <option value="problem">С проблемами</option>
            </select>
          </div>
        </div>
        {stations.length ? <div className="client-grid">{stations.map(({ client, summary, gridDown, problem }) => {
          const status = !summary.total ? 'offline' : problem ? (summary.online ? 'warn' : 'danger') : 'online';
          const battery = summary.bat.percent == null ? null : Math.round(summary.bat.percent);
          return <a className="ccard" key={client.id} href={`#/client/${client.id}`}>
            <div className="ccard-top">
              <div><b>{client.name}</b><small>{client.address || 'Адрес не указан'}</small></div>
              <span className={`badge ${status}`}>{summary.online}/{summary.total} в сети</span>
            </div>
            <div className="mini">
              <div className="c-pv">Солнце<b>{fmtPower(summary.pv.power)}</b></div>
              <div className="c-load">Нагрузка<b>{fmtPower(summary.load.power)}</b></div>
              <div className="c-bat">Батарея<b>{battery == null ? '—' : `${battery}%`}</b></div>
              <div className="c-grid">Сеть<b>{!summary.grid.online ? '—' : gridDown ? 'Нет сети' : fmtPower(summary.grid.power, true)}</b></div>
            </div>
            <div className="bar" title="Заряд батареи"><i style={{ width: `${Math.max(0, Math.min(100, battery ?? 0))}%` }} /></div>
          </a>;
        })}</div> : <p className="card empty">{clients.length ? 'Нет станций по этому фильтру' : 'Клиентов пока нет'}</p>}
      </section>
    </>
  );
}

export async function renderOverview(view: HTMLElement) {
  const [clients, requests, tasks, index] = await Promise.all([
    api('/api/clients/'),
    store.crmAccess.requests?.view ? api('/api/crm/requests/') : [],
    store.crmAccess.tasks?.view ? api('/api/crm/tasks/') : [],
    loadProviderIndex(),
  ]);
  if (!view.isConnected) return;
  const root = createRoot(view);
  root.render(<Overview clients={clients} requests={requests} tasks={tasks} index={index} />);
  return () => root.unmount();
}
