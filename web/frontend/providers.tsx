import { useEffect, useState } from 'react';
import { api } from '../static/js/api.js';
import { confirmModal, fmtPower, toast } from '../static/js/ui.js';

type Provider = {code:string; name:string; adapter:string; enabled:boolean; mock:boolean; sites_count:number; devices_count:number};
type Site = {id:string; provider_code:string; provider_name:string; credential_id:number|null; name:string; address:string; timezone:string;
  client_id:number|null; client_name:string|null; status:string; archived:boolean; enabled:boolean; mock:boolean; modem_ids:number[]; device_count:number;
  latitude:number|null; longitude:number|null; last_synced_at:string|null; devices?:{id:number;name:string;serial_number:string;power:number|null}[]};
type Account={id:number; provider_code:string; name:string; base_url:string; is_active:boolean; has_credentials:boolean; last_success_at:string|null; catalog_error:string|null};
type Client={id:number;name:string};
type IntegrationEvent={id:number;account_id:number;actor_name:string|null;action:string;created_at:string};
const eventNames:Record<string,string>={created:'Создание',updated:'Изменение',deleted:'Удаление'};
const status:Record<string,string>={normal:'В норме',warning:'Внимание',offline:'Нет связи',empty:'Нет оборудования',unknown:'Нет данных'};
const errorText=(err:unknown)=>err instanceof Error ? err.message : String(err);
const date=(value:string|null) => value ? new Date(value).toLocaleString('ru-KG') : '—';
const providerLink=(code:string)=> '#/sites?provider='+encodeURIComponent(code);

export function ProviderBadge({code,name}:{code:string;name:string}) {
  return <a className="provider-badge" href={providerLink(code)}><span aria-hidden="true">{name.slice(0,1).toUpperCase()}</span>{name}</a>;
}

export function SitesPage({sites,providers}:{sites:Site[];providers:Provider[]}) {
  const [,reload]=useState(0);
  const query=new URLSearchParams(location.hash.split('?')[1]);
  const provider=query.get('provider')?.split(',') ?? [];
  const q=query.get('q') || '';
  const link=query.get('link') || '';
  const scope=query.get('client') || '';
  function filter(key:string,value:string) {
    if(value) query.set(key,value); else query.delete(key);
    history.replaceState(null,'','#/sites'+(query.size ? '?'+query : ''));
    reload(v=>v+1);
  }
  const rows=sites.filter(site=>(!scope || String(site.client_id)===scope) &&
    (!provider.length || provider.includes(site.provider_code)) &&
    (link==='archived' ? site.archived : !site.archived && (link!=='unlinked' || !site.client_id)) &&
    [site.name,site.address,site.client_name].some(text=>text?.toLowerCase().includes(q.trim().toLowerCase())));
  return <section className="crm-list">
    <div className="crm-head"><h2>Объекты <small>{rows.length}</small></h2></div>
    <div className="crm-kpis">
      <button className="overview-metric" onClick={()=>filter('link','')}><span>Действующие</span><strong>{sites.filter(x=>!x.archived).length}</strong></button>
      <button className="overview-metric" onClick={()=>filter('link','unlinked')}><span>Непривязанные</span><strong>{sites.filter(x=>!x.archived&&!x.client_id).length}</strong></button>
      <button className="overview-metric" onClick={()=>filter('link','archived')}><span>Архив</span><strong>{sites.filter(x=>x.archived).length}</strong></button>
    </div>
    <div className="toolbar"><input type="search" aria-label="Поиск объектов" placeholder="Название, адрес или клиент" value={q} onChange={e=>filter('q',e.target.value)}/>
      {scope && <button className="btn" onClick={()=>filter('client','')}>Клиент №{scope} ×</button>}
      {providers.map(p=><label className="provider-badge" key={p.code}><input type="checkbox" checked={provider.includes(p.code)} onChange={e=>filter('provider',e.target.checked?[...provider,p.code].join(','):provider.filter(x=>x!==p.code).join(','))}/>{p.name}</label>)}
    </div>
    {rows.length ? <div className="card crm-table"><table><thead><tr><th>Объект</th><th>Провайдер</th><th>Клиент</th><th>Статус</th><th>Устройства</th></tr></thead><tbody>
      {rows.map(s=><tr key={s.id}><td><a href={`#/sites/${s.id}`}>{s.name}</a><br/><small>{s.address}</small></td>
        <td><ProviderBadge code={s.provider_code} name={s.provider_name}/></td><td>{s.client_id?<a href={`#/client/${s.client_id}`}>{s.client_name}</a>:'Не привязан'}</td>
        <td><span className={`badge ${s.status==='normal'?'online':s.status==='warning'?'warn':'offline'}`}>{s.archived?'Архив':status[s.status]}</span>
          {!s.enabled && <small> Синхронизация отключена</small>}</td><td>{s.device_count}</td></tr>)}
    </tbody></table></div>:<p className="card empty">Объекты по этому фильтру не найдены</p>}
  </section>;
}

export function SitePage({initial,canLink}:{initial:Site;canLink:boolean}) {
  const [site,setSite]=useState(initial);
  const [clients,setClients]=useState<Client[]>([]);
  const [error,setError]=useState('');
  const [pending,setPending]=useState(false);
  const eligible=canLink && !site.client_id && !site.archived && site.id.startsWith('s');
  useEffect(()=>{
    let active=true;
    if(eligible) api('/api/clients/').then(rows=>{if(active)setClients(rows);}).catch(err=>{if(active)setError(errorText(err));});
    return ()=>{active=false;};
  },[eligible]);
  async function link(e:React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const client_id=Number(new FormData(e.currentTarget).get('client_id'));
    if(!await confirmModal('Привязать выбранного клиента к объекту?','Привязать')) return;
    setPending(true);
    try {
      await api(`/api/crm/sites/${site.id}/client`,{method:'PUT',body:{client_id}});
      setSite(await api(`/api/crm/sites/${site.id}`));
      toast('Объект привязан');
    } catch(err){setError(errorText(err));} finally{setPending(false);}
  }
  return <section className="crm-list">
    <div className="crm-head"><h2>{site.name}</h2><ProviderBadge code={site.provider_code} name={site.provider_name}/></div>
    <div className="card">
      <div className="toolbar"><span className="badge">{site.archived?'Архив':status[site.status]}</span>{site.mock&&<span>Демонстрационный объект</span>}
      {!site.enabled&&<span>Синхронизация отключена</span>}</div>
      <dl><dt>Адрес</dt><dd>{site.address||'—'}</dd><dt>Часовой пояс</dt><dd>{site.timezone}</dd>
        <dt>Последнее обновление каталога</dt><dd>{date(site.last_synced_at)}</dd><dt>Клиент</dt>
        <dd>{site.client_id?<a href={`#/client/${site.client_id}`}>{site.client_name}</a>:'Не привязан'}</dd></dl>
    </div>
    {eligible&&<form className="card crm-form" onSubmit={link}><label>Привязка к клиенту<select name="client_id" required defaultValue=""><option value="">Выберите клиента</option>
      {clients.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}</select></label><div><button className="btn btn-primary" disabled={pending||!clients.length}>Привязать</button></div></form>}
    {error&&<p role="alert" className="form-error">{error}</p>}
    <section className="card"><h3>Оборудование</h3>{site.devices?.length?<ul>{site.devices.map(d=><li key={d.id}><a href={`#/equipment/${d.id}`}>{d.name||d.serial_number}</a> · {fmtPower(d.power)}</li>)}</ul>:<p className="muted">К объекту пока нет оборудования в локальном мониторинге</p>}</section>
  </section>;
}

export function IntegrationsPage({initialProviders,initialAccounts,initialEvents,newAccount}:{initialProviders:Provider[];initialAccounts:Account[];initialEvents:IntegrationEvent[];newAccount?:number|'new'}) {
  const [providers,setProviders]=useState(initialProviders);
  const [accounts,setAccounts]=useState(initialAccounts);
  const [events,setEvents]=useState(initialEvents);
  const [pending,setPending]=useState(false);
  const [error,setError]=useState('');
  const account=accounts.find(a=>a.id===newAccount);
  async function perform(action:()=>Promise<unknown>, message:string) {
    setError('');setPending(true);
    try {
      await action();
      const [p,a,e]=await Promise.all([api('/api/crm/providers'),api('/api/crm/integrations/accounts'),api('/api/crm/integrations/activity')]);
      setProviders(p);setAccounts(a);setEvents(e);toast(message);
    }catch(err){setError(errorText(err));}finally{setPending(false);}
  }
  async function saveAccount(e:React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data=Object.fromEntries(new FormData(e.currentTarget));
    const body=Object.fromEntries(Object.entries(data).filter(([,value])=>value!==''));
    await perform(async()=>{
      await api('/api/deye/accounts/'+(account?.id??''),{method:account?'PATCH':'POST',body});
      location.hash='#/integrations';
    },'Аккаунт сохранён');
  }
  return <section className="crm-list">
    <div className="crm-head"><h2>{account?'Аккаунт Deye':newAccount?'Новый аккаунт Deye':'Интеграции'}</h2>
      {newAccount?<a className="btn" href="#/integrations">К интеграциям</a>:<a className="btn btn-primary" href="#/integrations/new">Добавить аккаунт Deye</a>}</div>
    {error&&<p role="alert" className="form-error">{error}</p>}
    {newAccount ? (typeof newAccount==='number'&&!account?<p className="card empty">Аккаунт не найден</p>:<form className="card crm-form" onSubmit={saveAccount}>
      <div className="crm-fields">
        <label>Название<input name="name" required defaultValue={account?.name}/></label>
        <label>Регион<select name="base_url" defaultValue={account?.base_url.replace(/\/v1\.0\/?$/,'')||'https://eu1-developer.deyecloud.com'}>
          <option value="https://eu1-developer.deyecloud.com">Европа</option><option value="https://us1-developer.deyecloud.com">США</option></select></label>
        {Object.entries({app_id:'App ID',app_secret:'App Secret',password:'Пароль Deye',email:'Email',username:'Имя пользователя',mobile:'Телефон',country_code:'Код страны'}).map(([key,label])=><label key={key}>{label}
          <input name={key} type={key==='password'||key==='app_secret'?'password':key==='email'?'email':'text'}
            required={!account&&['app_id','app_secret','password'].includes(key)} placeholder={account?.has_credentials?'••••••':''} autoComplete="off"/></label>)}
      </div>
      {account&&<small className="muted">Пустые поля сохраняют прежние значения.</small>}
      <div><button className="btn btn-primary" disabled={pending}>Сохранить</button></div>
    </form>) : <>
      <div className="card crm-table"><table><thead><tr><th>Провайдер</th><th>Объекты</th><th>Устройства</th><th>Статус</th><th>Действие</th></tr></thead><tbody>
        {providers.map(p=><tr key={p.code}><td><ProviderBadge code={p.code} name={p.name}/></td><td>{p.sites_count}</td><td>{p.devices_count}</td>
          <td>{p.mock?'Демо · ':''}{p.enabled?'Включён':'Отключён'}</td><td>{p.adapter!=='local'&&<button className="btn" disabled={pending} onClick={()=>perform(()=>api('/api/crm/integrations/providers/'+p.code,{method:'PUT',body:{enabled:!p.enabled}}),'Статус обновлён')}>{p.enabled?'Отключить':'Включить'}</button>}</td></tr>)}
      </tbody></table></div>
      <form className="card crm-form" onSubmit={e=>{e.preventDefault();const body=Object.fromEntries(new FormData(e.currentTarget));void perform(()=>api('/api/crm/integrations/providers',{method:'POST',body}),'Провайдер добавлен');}}>
        <h3>Добавить провайдера</h3><div className="crm-fields"><label>Название<input name="name" required maxLength={100}/></label>
          <label>Код<input name="code" required pattern="[a-z][a-z0-9_-]{1,29}"/></label><label>Адаптер<select name="adapter"><option value="local">Локальный</option><option value="eybond">Eybond · демонстрационный</option></select></label></div>
        <div><button className="btn" disabled={pending}>Добавить провайдера</button></div>
      </form>
      <section className="card"><h3>Аккаунты Deye</h3>{accounts.length?accounts.map(a=><div className="integration-account" key={a.id}>
        <strong><a href={`#/integrations/${a.id}`}>{a.name}</a></strong><small>Каталог обновлён {date(a.last_success_at)} · {a.is_active?'Аккаунт включён':'Аккаунт выключен'}</small>
        {a.catalog_error&&<p className="form-error">{a.catalog_error}</p>}
        <div className="toolbar"><button className="btn" disabled={pending} onClick={()=>perform(()=>api(`/api/deye/accounts/${a.id}/test`,{method:'POST'}),'Соединение проверено')}>Проверить соединение</button>
          <button className="btn" disabled={pending||!a.is_active||!providers.find(p=>p.code===a.provider_code)?.enabled} onClick={()=>perform(()=>api(`/api/crm/integrations/accounts/${a.id}/sync`,{method:'POST'}),'Каталог синхронизирован')}>Синхронизировать сейчас</button>
          <button className="btn" disabled={pending} onClick={()=>perform(()=>api(`/api/deye/accounts/${a.id}/`,{method:'PATCH',body:{is_active:!a.is_active}}),'Аккаунт обновлён')}>{a.is_active?'Выключить аккаунт':'Включить аккаунт'}</button></div></div>):<p className="muted">Облачные аккаунты пока не подключены</p>}</section>
      <section className="card"><h3>Изменения облачных аккаунтов</h3>
        {events.length?<ul>{events.map(event=><li key={event.id}>{eventNames[event.action]} · аккаунт №{event.account_id} · {event.actor_name||'Система'} · {date(event.created_at)}</li>)}</ul>
          :<p className="muted">Записей пока нет</p>}</section>
    </>}
  </section>;
}
