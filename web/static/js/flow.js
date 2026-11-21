// =============================================================================
// flow.js — схема потоков энергии станции (SVG):
//
//        [ПАНЕЛИ]                  [СЕТЬ]
//              \                  /
//               ---[ИНВЕРТОР]----
//              /        |         \
//        [БАТАРЕЯ]      |       [ГЕНЕРАТОР]
//                   [НАГРУЗКА]
//
// Бегущие точки на линиях показывают, куда идёт энергия.
// Использование:  const flow = createFlow(div);  flow.update(summarize(devices));
// =============================================================================
import { fmtNum, fmtPower } from './ui.js';

const IDLE_W = 20; // меньше 20 Вт считаем "ничего не течёт"

// ---------- Иконки (рисуются в квадрате 44x44) ----------
const ICONS = {
  pv: `<circle cx="11" cy="9" r="5"/><path d="M11 0.5v1.8M11 15.7v1.8M2.5 9h1.8M17.7 9h1.8M5 3l1.2 1.2M15.8 13.8L17 15M5 15l1.2-1.2M15.8 4.2L17 3"/>
       <path d="M15 22h27l-5 19H9z"/><path d="M12 31.5h28M24 22l-3 19M33 22l-3 19"/>`,
  bat: `<rect x="10" y="7" width="24" height="35" rx="4"/><rect x="17" y="2.5" width="10" height="4.5" rx="1.5" class="fill"/>
        <rect x="14" y="11" width="16" height="27" rx="2" class="fill" data-batfill opacity=".85"/>`,
  grid: `<path d="M22 2L12 42M22 2l10 40M15 31h14M17.5 21h9M6 11h32M10 21h24M6 11l-2 5M38 11l2 5M10 21l-1.5 4M34 21l1.5 4"/>`,
  gen: `<rect x="3" y="11" width="38" height="25" rx="4"/><circle cx="11" cy="39.5" r="3"/><circle cx="33" cy="39.5" r="3"/>
        <path d="M25 14l-7 11h6l-2 8 8-12h-6l2-7z" class="fill"/>`,
  load: `<path d="M4 22L22 6l18 16"/><path d="M9 18v22h26V18"/><rect x="18" y="27" width="8" height="13"/>`,
};

// Одна "кнопка-кружок" на схеме
function node(k, cls, x, y, label, icon) {
  return `<g class="node ${cls}" data-node="${k}">
    <circle cx="${x}" cy="${y}" r="46" class="node-bg"/>
    <g class="icon" transform="translate(${x - 22} ${y - 22})">${icon}</g>
    <text x="${x}" y="${y + 68}" text-anchor="middle" class="node-label">${label}</text>
    <text x="${x}" y="${y + 98}" text-anchor="middle" class="node-value" data-val="${k}">—</text>
    <text x="${x}" y="${y + 121}" text-anchor="middle" class="node-sub" data-sub="${k}"></text>
  </g>`;
}

// Линия + "бегущие точки" поверх неё
function line(k, cls, d) {
  return `<g data-line="${k}"><path class="line" d="${d}"/><path class="flow ${cls}" data-flow="${k}" d="${d}"/></g>`;
}

const SVG = `
<svg class="flow-svg" viewBox="0 0 760 590" role="img" aria-label="Схема потоков энергии">
  ${line('pv', 'c-pv', 'M156 80 H330 V165')}
  ${line('grid', 'c-grid', 'M604 80 H430 V165')}
  ${line('bat', 'c-bat', 'M156 330 H330 V275')}
  ${line('gen', 'c-gen', 'M604 330 H430 V275')}
  ${line('load', 'c-load', 'M380 275 V394')}

  ${node('pv', 'c-pv', 110, 80, 'Панели', ICONS.pv)}
  ${node('grid', 'c-grid', 650, 80, 'Сеть', ICONS.grid)}
  ${node('bat', 'c-bat', 110, 330, 'Батарея', ICONS.bat)}
  ${node('gen', 'c-gen', 650, 330, 'Генератор', ICONS.gen)}

  <g class="node c-inv" data-node="inv">
    <rect x="315" y="165" width="130" height="110" rx="16" class="inv-box"/>
    <text x="380" y="192" text-anchor="middle" class="node-label">Инвертор</text>
    <path d="M348 210 q8 -14 16 0 t16 0 t16 0" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>
    <text x="380" y="245" text-anchor="middle" class="node-value inv-value" data-val="inv">—</text>
    <text x="380" y="264" text-anchor="middle" class="node-sub inv-sub" data-sub="inv"></text>
  </g>

  ${node('load', 'c-load', 380, 440, 'Нагрузка', ICONS.load)}
</svg>`;

// ---------- Сводка по устройствам станции ----------
// Превращает список устройств в понятные числа для схемы.
export function summarize(devices) {
  const of = (t) => devices.filter((d) => d.device_type === t);
  const on = (d) => d.status === 'online';
  const pw = (d) => (on(d) && d.last_data ? Number(d.last_data.power) || 0 : 0);
  const pack = (list) => ({
    exists: list.length > 0,
    online: list.some(on),
    power: list.reduce((s, d) => s + pw(d), 0),
    voltage: list.find(on)?.last_data?.voltage ?? null,
    temperature: list.find(on)?.last_data?.temperature ?? null,
    error: list.map((d) => on(d) && d.last_data?.error).find(Boolean) || null,
  });
  const bat = pack(of('battery'));
  bat.percent = of('battery').map((d) => d.last_data?.battery_percent).find((v) => v != null) ?? null;
  return {
    pv: pack(of('panel')),
    inv: pack(of('inverter')),
    bat,
    gen: pack(of('generator')),
    grid: pack(of('grid')),
    load: pack(of('load')),
    online: devices.filter(on).length,
    total: devices.length,
  };
}

// ---------- Создание схемы ----------
export function createFlow(container) {
  container.innerHTML = SVG;
  const $ = (sel) => container.querySelector(sel);
  const refs = {};
  for (const k of ['pv', 'grid', 'bat', 'gen', 'load', 'inv']) {
    refs[k] = { line: $(`[data-line="${k}"]`), node: $(`[data-node="${k}"]`), val: $(`[data-val="${k}"]`), sub: $(`[data-sub="${k}"]`), flow: $(`[data-flow="${k}"]`) };
  }
  const batFill = $('[data-batfill]');

  // Меняем текст только если он изменился (меньше работы для браузера)
  const setText = (el, text) => { if (el && el.textContent !== text) el.textContent = text; };

  // Включает/выключает поток. forward=true: от устройства к инвертору (для нагрузки — к нагрузке)
  const setFlow = (k, power, forward) => {
    const el = refs[k].flow;
    const active = Math.abs(power) > IDLE_W;
    el.classList.toggle('on', active);
    el.classList.toggle('rev', active && !forward);
    // чем больше мощность — тем быстрее бегут точки
    const dur = Math.max(0.35, 2.2 - Math.abs(power) / 2500).toFixed(2) + 's';
    if (el.style.animationDuration !== dur) el.style.animationDuration = dur;
  };

  const setNode = (k, info) => {
    const n = refs[k].node;
    n.classList.toggle('missing', !info.exists);
    n.classList.toggle('off', info.exists && !info.online);
    n.classList.toggle('alarm', !!info.error);
  };

  // Генератора у клиента нет -> вообще не рисуем его на схеме
  const setVisible = (k, visible) => {
    const v = visible ? '' : 'none';
    if (refs[k].node.style.display !== v) refs[k].node.style.display = v;
    if (refs[k].line && refs[k].line.style.display !== v) refs[k].line.style.display = v;
  };

  function update(s) {
    // Панели
    setNode('pv', s.pv);
    setText(refs.pv.val, s.pv.online ? fmtPower(s.pv.power) : (s.pv.exists ? 'нет связи' : '—'));
    setText(refs.pv.sub, s.pv.online && s.pv.voltage ? fmtNum(s.pv.voltage, 'В') : '');
    setFlow('pv', s.pv.power, true);

    // Сеть (+ берём из сети, - отдаём)
    setNode('grid', s.grid);
    const gridDown = s.grid.online && (s.grid.error || !s.grid.voltage);
    refs.grid.node.classList.toggle('alarm', !!gridDown);
    setText(refs.grid.val, !s.grid.online ? (s.grid.exists ? 'нет связи' : '—') : gridDown ? 'Отключена' : fmtPower(Math.abs(s.grid.power)));
    setText(refs.grid.sub, !s.grid.online ? '' : gridDown ? 'нет напряжения' : s.grid.power > IDLE_W ? 'Покупка' : s.grid.power < -IDLE_W ? 'Продажа' : 'Ожидание');
    setFlow('grid', s.grid.power, s.grid.power > 0);

    // Батарея (+ заряд, - разряд)
    setNode('bat', s.bat);
    const pct = s.bat.percent;
    setText(refs.bat.val, pct != null ? Math.round(pct) + '%' : (s.bat.exists ? 'нет связи' : '—'));
    setText(refs.bat.sub, !s.bat.online ? '' : s.bat.power > IDLE_W ? 'Заряд ' + fmtPower(s.bat.power)
      : s.bat.power < -IDLE_W ? 'Разряд ' + fmtPower(-s.bat.power) : 'Покой');
    setFlow('bat', s.bat.power, s.bat.power < 0);
    if (batFill) {
      const p = Math.max(0, Math.min(100, pct ?? 0)) / 100;
      batFill.setAttribute('height', (27 * p).toFixed(1));
      batFill.setAttribute('y', (11 + 27 * (1 - p)).toFixed(1));
    }

    // Генератор
    setVisible('gen', s.gen.exists);
    setNode('gen', s.gen);
    setText(refs.gen.val, !s.gen.online ? 'нет связи' : s.gen.power > IDLE_W ? fmtPower(s.gen.power) : 'Выключен');
    setText(refs.gen.sub, s.gen.online && s.gen.power > IDLE_W ? 'работает' : '');
    setFlow('gen', s.gen.power, true);

    // Нагрузка (дом)
    setNode('load', s.load);
    setText(refs.load.val, s.load.online ? fmtPower(s.load.power) : (s.load.exists ? 'нет связи' : '—'));
    setText(refs.load.sub, s.load.online ? 'потребление' : '');
    setFlow('load', s.load.power, true);

    // Инвертор
    setNode('inv', s.inv);
    setText(refs.inv.val, s.inv.online ? fmtPower(s.inv.power) : (s.inv.exists ? 'нет связи' : '—'));
    setText(refs.inv.sub, s.inv.error ? '⚠ ' + s.inv.error : s.inv.temperature != null && s.inv.online ? fmtNum(s.inv.temperature, '°C') : '');
  }

  return { update };
}
