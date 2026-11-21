import { useState } from 'react';
import { api } from '../static/js/api.js';
import { confirmModal, toast } from '../static/js/ui.js';

const actions = { view: 'Просмотр', create: 'Создание', edit: 'Редактирование', delete: 'Удаление', assign: 'Назначение', own: 'Только свои' };
const modules = { requests: 'Заявки', tasks: 'Задачи' };
type Grants = Record<keyof typeof modules, Record<keyof typeof actions, boolean>>;
type Role = { code: string; name: string; system: boolean; grants: Grants };
const keys = Object.keys(actions) as (keyof typeof actions)[];
const kinds = Object.keys(modules) as (keyof typeof modules)[];
const none = {view: false, create: false, edit: false, delete: false, assign: false, own: false};
const empty = (): Role => ({code: 'crm_', name: '', system: false, grants: {requests: {...none}, tasks: {...none}}});

export function RolesPage({ initial }: { initial: Role[] }) {
  const [roles, setRoles] = useState(initial);
  const [draft, setDraft] = useState<Role>(initial[0] ?? empty());
  const [savedCode, setSavedCode] = useState(initial[0]?.code ?? '');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const locked = draft.code === 'admin' && savedCode === 'admin';
  const endpoint = '/api/crm/roles/';
  function select(role: Role | undefined) {
    setDraft(role ?? empty());
    setSavedCode(role?.code ?? '');
    setError('');
  }
  function change(kind: keyof Grants, action: keyof typeof actions, enabled: boolean) {
    const line = {...draft.grants[kind], [action]: enabled};
    if (!line.view) Object.assign(line, none);
    if (!line.edit && !line.create) line.assign = false;
    setDraft({...draft, grants: {...draft.grants, [kind]: line}});
  }
  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setError('');
    try {
      await api(endpoint + savedCode, {method: savedCode ? 'PUT' : 'POST', body: draft});
      const list = await api(endpoint) as Role[];
      setRoles(list);
      select(list.find(role => role.code === draft.code));
      toast('Права сохранены');
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); }
    finally { setPending(false); }
  }
  async function remove() {
    if (!await confirmModal('Удалить эту роль?')) return;
    setPending(true);
    try {
      await api(endpoint + savedCode, {method: 'DELETE'});
      const list = await api(endpoint) as Role[];
      setRoles(list);
      select(list[0]);
      toast('Роль удалена');
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); }
    finally { setPending(false); }
  }
  return <section className="crm-list">
    <div className="crm-head"><h2>Роли и права</h2><button className="btn btn-primary" disabled={pending} onClick={() => select(undefined)}>Создать роль</button></div>
    <label>Роль<select disabled={pending} value={savedCode} onChange={event => select(roles.find(role => role.code === event.target.value))}>
      <option value="">Новая роль</option>{roles.map(role => <option key={role.code} value={role.code}>{role.name}</option>)}</select></label>
    <form className="card crm-form" onSubmit={save}>
      <div className="crm-fields">
        <label>Название<input required maxLength={80} value={draft.name} disabled={locked || pending} onChange={event => setDraft({...draft, name: event.target.value})}/></label>
        <label>Код<input required pattern="crm_[a-z][a-z0-9_]{1,15}" title="crm_ и строчные латинские буквы, цифры или подчёркивание" value={draft.code} disabled={Boolean(savedCode) || pending} onChange={event => setDraft({...draft, code: event.target.value})}/></label>
      </div>
      <div className="table-wrap"><table>
        <thead><tr><th>Модуль</th>{keys.map(action => <th key={action}>{actions[action]}</th>)}</tr></thead>
        <tbody>{kinds.map(kind => <tr key={kind}><th scope="row">{modules[kind]}</th>{keys.map(action => <td key={action}><input type="checkbox"
          aria-label={modules[kind] + ': ' + actions[action]} checked={draft.grants[kind][action]}
          disabled={locked || pending || action !== 'view' && !draft.grants[kind].view || action === 'assign' && !draft.grants[kind].create && !draft.grants[kind].edit}
          onChange={event => change(kind, action, event.target.checked)}/></td>)}</tr>)}</tbody>
      </table></div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="toolbar">
        {!locked && <button className="btn btn-primary" disabled={pending}>Сохранить</button>}
        {savedCode && <button type="button" className="btn" disabled={pending} onClick={() => {
          setDraft({...draft, name: 'Копия роли ' + draft.name, code: 'crm_', system: false});
          setSavedCode('');
          setError('');
        }}>Копировать</button>}
        {savedCode && !draft.system && <button type="button" className="btn btn-danger" disabled={pending} onClick={remove}>Удалить</button>}
      </div>
    </form>
  </section>;
}
