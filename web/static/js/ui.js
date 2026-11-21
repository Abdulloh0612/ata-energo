// =============================================================================
// ui.js — маленькие помощники для интерфейса:
// форматирование чисел и дат, окна (модалки), формы, уведомления.
// =============================================================================

// Защита от HTML-вставок: всегда пропускайте данные пользователя через esc()
export function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// Мощность: 850 -> "850 Вт", 2450 -> "2.45 кВт"
export function fmtPower(w, signed = false) {
  if (w == null || Number.isNaN(w)) return '—';
  const sign = signed && w > 0 ? '+' : '';
  const a = Math.abs(w);
  if (a >= 1000) return sign + (w / 1000).toFixed(a >= 10000 ? 1 : 2) + ' кВт';
  return sign + Math.round(w) + ' Вт';
}

export function fmtNum(v, unit = '', digits = 1) {
  if (v == null || Number.isNaN(v)) return '—';
  return Number(v).toFixed(digits).replace(/\.0+$/, '') + (unit ? ' ' + unit : '');
}

export function fmtDate(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

// "5 сек назад", "3 мин назад"
export function timeAgo(iso) {
  if (!iso) return 'нет данных';
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s} сек назад`;
  if (s < 3600) return `${Math.floor(s / 60)} мин назад`;
  if (s < 86400) return `${Math.floor(s / 3600)} ч назад`;
  return fmtDate(iso);
}

export const TYPE_LABELS = {
  panel: 'Панели', inverter: 'Инвертор', battery: 'Батарея',
  generator: 'Генератор', grid: 'Сеть', load: 'Нагрузка',
};
export const TYPE_ORDER = ['panel', 'inverter', 'battery', 'generator', 'grid', 'load'];
export const ROLE_LABELS = { admin: 'Администратор', it_head: 'Руководитель IT', dispatcher: 'Диспетчер', manager: 'Менеджер', technician: 'Техник', observer: 'Наблюдатель', user: 'Клиент' };

export function typeLabel(t) { return TYPE_LABELS[t] || t; }

export function statusBadge(status) {
  return status === 'online'
    ? '<span class="badge online">в сети</span>'
    : '<span class="badge offline">нет связи</span>';
}

// Вызывает fn не чаще одного раза за кадр (экран обновляется плавно, без тормозов)
export function rafThrottle(fn) {
  let scheduled = false;
  return () => {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(() => { scheduled = false; fn(); });
  };
}

// Всплывающее уведомление в углу экрана
export function toast(text, type = '') {
  const box = document.getElementById('toasts');
  const el = document.createElement('div');
  el.className = 'toast ' + type;
  el.textContent = text;
  box.appendChild(el);
  while (box.children.length > 4) box.firstChild.remove();
  setTimeout(() => el.remove(), 4500);
}

// Открыть модальное окно. Возвращает { el, close }.
export function openModal(html) {
  const root = document.getElementById('modal-root');
  const bg = document.createElement('div');
  bg.className = 'modal-bg';
  bg.innerHTML = `<div class="modal" role="dialog">${html}</div>`;
  const close = () => { bg.remove(); document.removeEventListener('keydown', onKey); };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  bg.addEventListener('mousedown', (e) => { if (e.target === bg) close(); });
  document.addEventListener('keydown', onKey);
  root.appendChild(bg);
  return { el: bg.querySelector('.modal'), close };
}

// Окно "Вы уверены?" — возвращает Promise<true/false>
export function confirmModal(text, okText = 'Удалить') {
  return new Promise((resolve) => {
    const m = openModal(`<h3>Подтверждение</h3><p>${esc(text)}</p>
      <div class="modal-actions"><button class="btn" data-no>Отмена</button>
      <button class="btn btn-primary" data-yes>${esc(okText)}</button></div>`);
    m.el.querySelector('[data-no]').onclick = () => { m.close(); resolve(false); };
    m.el.querySelector('[data-yes]').onclick = () => { m.close(); resolve(true); };
  });
}

// Универсальная форма в модальном окне.
// fields: [{ name, label, type: 'text'|'password'|'select'|'checkbox'|'email'|'tel',
//            options: [[value, label]], required, help, half }]
// onSubmit(data) — async; если бросит ApiError, ошибки покажутся под полями.
export function formModal({ title, fields, values = {}, submitText = 'Сохранить', onSubmit }) {
  const fieldHtml = (f) => {
    const v = values[f.name] ?? '';
    const req = f.required ? 'required' : '';
    let input;
    if (f.type === 'select') {
      input = `<select name="${f.name}" ${req}>${f.options.map(([val, lab]) =>
        `<option value="${esc(val)}" ${String(val) === String(v) ? 'selected' : ''}>${esc(lab)}</option>`).join('')}</select>`;
    } else if (f.type === 'checkbox') {
      return `<label class="check"><input type="checkbox" name="${f.name}" ${v ? 'checked' : ''}> ${esc(f.label)}</label>`;
    } else {
      input = `<input name="${f.name}" type="${f.type || 'text'}" value="${esc(v)}" ${req}
        ${f.type === 'password' ? 'autocomplete="new-password"' : ''} placeholder="${esc(f.placeholder || '')}">`;
    }
    return `<label data-field="${f.name}">${esc(f.label)}${f.required ? ' *' : ''}${input}
      ${f.help ? `<div class="field-help">${esc(f.help)}</div>` : ''}<div class="field-error"></div></label>`;
  };

  // поля с half: true ставим по два в строку
  let body = '';
  for (let i = 0; i < fields.length; i++) {
    if (fields[i].half && fields[i + 1]?.half) {
      body += `<div class="row2">${fieldHtml(fields[i])}${fieldHtml(fields[i + 1])}</div>`;
      i++;
    } else body += fieldHtml(fields[i]);
  }

  const m = openModal(`<h3>${esc(title)}</h3><form novalidate>${body}
    <div class="form-error"></div>
    <div class="modal-actions"><button type="button" class="btn" data-cancel>Отмена</button>
    <button type="submit" class="btn btn-primary">${esc(submitText)}</button></div></form>`);
  const form = m.el.querySelector('form');
  form.querySelector('[data-cancel]').onclick = m.close;
  form.querySelector('input, select')?.focus();

  form.onsubmit = async (e) => {
    e.preventDefault();
    form.querySelectorAll('.field-error').forEach((el) => (el.textContent = ''));
    form.querySelector('.form-error').textContent = '';
    const data = {};
    for (const f of fields) {
      const input = form.elements[f.name];
      data[f.name] = f.type === 'checkbox' ? input.checked : input.value.trim();
      if (f.required && !data[f.name]) {
        form.querySelector(`[data-field="${f.name}"] .field-error`).textContent = 'Обязательное поле';
        return;
      }
    }
    const btn = form.querySelector('[type=submit]');
    btn.disabled = true;
    try {
      await onSubmit(data);
      m.close();
    } catch (err) {
      let shown = false;
      if (err.data && typeof err.data === 'object') {
        for (const [k, v] of Object.entries(err.data)) {
          const slot = form.querySelector(`[data-field="${k}"] .field-error`);
          if (slot) { slot.textContent = [].concat(v).join(' '); shown = true; }
        }
      }
      if (!shown) form.querySelector('.form-error').textContent = err.message;
    } finally {
      btn.disabled = false;
    }
  };
  return m;
}
