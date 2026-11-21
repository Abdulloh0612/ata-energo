import { useState } from 'react';
import { api } from '../static/js/api';
import { store } from '../static/js/store';
import { fmtPower, toast } from '../static/js/ui';

type Meta = {
  condition: string; can_edit: boolean; model: string | null; nominal_watts: number | null;
  installed_on: string | null; serviced_on: string | null; service_due_on: string | null; warranty_until: string | null;
  maintenance: boolean; service_due: boolean; on_warranty: boolean;
};
type Device = {id: number; client: number; client_name: string; device_id: string; name: string; serial_number: string; last_data?: {power?: number}};
const labels: Record<string, string> = {normal:'В норме',low:'Низкая мощность',problem:'Проблема',maintenance:'Обслуживание',offline:'Отключено'};
const dates = {installed_on:'Дата установки', serviced_on:'Последнее обслуживание', service_due_on:'Следующее обслуживание', warranty_until:'Гарантия до'} as const;

export function EquipmentPage({device, initial}: {device: Device; initial: Meta}) {
  const [metadata, setMetadata] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const path = '/api/crm/equipment/' + device.id;
  const power = device.last_data?.power;
  const percent = metadata.nominal_watts != null && power != null ? Math.round(power/metadata.nominal_watts*100) : null;
  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError('');
    const form = new FormData(event.currentTarget);
    try {
      await api(path, {method:'PUT', body:{
        model: form.get('model'), nominal_watts: form.get('nominal_watts') ? Number(form.get('nominal_watts')) : null,
        ...Object.fromEntries(Object.keys(dates).map(key => [key, form.get(key) || null])),
        maintenance: form.has('maintenance'),
      }});
      setMetadata(await api(path));
      toast('Сведения сохранены');
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); }
    finally { setSaving(false); }
  }
  return <section className="crm-list">
    <div className="crm-head"><h2>{metadata.model || device.name || device.device_id}</h2>
      <span className={`badge ${metadata.condition === 'normal' ? 'online' : metadata.condition === 'problem' ? 'danger' : metadata.condition === 'offline' ? 'offline' : 'warn'}`}>{labels[metadata.condition]}</span>
    </div>
    <div className="toolbar"><a className="btn" href={`#/client/${device.client}`}>{device.client_name || 'Карточка клиента'}</a>
      <a className="btn" href={`#/devices?client=${device.client}`}>Оборудование клиента</a>
      {store.crmAccess.requests?.view && <a className="btn" href={`#/requests?device=${device.id}`}>Заявки по устройству</a>}
    </div>
    <div className="crm-kpis">
      <div className="overview-metric"><span>Серийный номер</span><b>{device.serial_number || '—'}</b></div>
      <div className="overview-metric"><span>Текущая мощность</span><b>{fmtPower(power)}{percent == null ? '' : ` · ${percent}%`}</b></div>
      <div className="overview-metric"><span>Номинальная мощность</span><b>{fmtPower(metadata.nominal_watts)}</b></div>
    </div>
    {metadata.can_edit ? <form className="card crm-form" onSubmit={save}>
      <div className="crm-fields">
        <label>Модель<input name="model" maxLength={120} defaultValue={metadata.model || ''}/></label>
        <label>Номинальная мощность, Вт<input type="number" name="nominal_watts" min="0.01" max="1000000000" step="any" defaultValue={metadata.nominal_watts ?? ''}/></label>
        {Object.entries(dates).map(([key,label]) => <label key={key}>{label}<input type="date" name={key} defaultValue={metadata[key as keyof typeof dates] || ''}/></label>)}
      </div>
      <label><input type="checkbox" name="maintenance" defaultChecked={Boolean(metadata.maintenance)}/> На обслуживании</label>
      {error && <p role="alert" className="form-error">{error}</p>}
      <div><button className="btn btn-primary" disabled={saving}>{saving ? 'Сохранение…' : 'Сохранить'}</button></div>
    </form> : <dl className="card">{Object.entries(dates).map(([key,label]) => <div key={key}><dt>{label}</dt><dd>{metadata[key as keyof typeof dates] || '—'}</dd></div>)}</dl>}
  </section>;
}

export function SettingsPage({initial}: {initial: number}) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const percent = Number(new FormData(event.currentTarget).get('percent'));
    setSaving(true);
    setError('');
    try {
      await api('/api/crm/settings/', {method:'PUT', body:{low_power_percent:percent}});
      toast('Настройки сохранены');
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); }
    finally { setSaving(false); }
  }
  return <section className="crm-list"><div className="crm-head"><h2>Настройки мониторинга</h2></div>
    <form className="card crm-form" onSubmit={save}>
      <label>Порог низкой мощности, %<input type="number" name="percent" min={1} max={100} defaultValue={initial} required/></label>
      <p className="muted">Применяется к оборудованию в сети, для которого указана номинальная мощность.</p>
      {error && <p role="alert" className="form-error">{error}</p>}
      <div><button className="btn btn-primary" disabled={saving}>{saving ? 'Сохранение…' : 'Сохранить'}</button></div>
    </form></section>;
}
