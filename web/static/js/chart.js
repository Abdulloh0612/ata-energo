// =============================================================================
// chart.js — лёгкий линейный график на <canvas> без внешних библиотек.
// Быстро рисует тысячи точек, показывает подсказку при наведении/касании.
//
// Пример:  const chart = new LineChart(canvas);
//          chart.setData([{t: new Date(), v: 123}], {color: '#f5b301', unit: 'Вт'});
// =============================================================================

export class LineChart {
  constructor(canvas) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d');
    this.points = [];
    this.color = '#f5b301';
    this.unit = '';
    this.hoverX = null;
    this.redraw = () => requestAnimationFrame(() => this.draw());
    this.resizeObserver = new ResizeObserver(this.redraw);
    this.resizeObserver.observe(canvas);
    canvas.addEventListener('pointermove', (e) => { this.hoverX = e.offsetX; this.redraw(); });
    canvas.addEventListener('pointerleave', () => { this.hoverX = null; this.redraw(); });
  }

  // points: [{t: Date, v: число или null}]
  setData(points, { color, unit } = {}) {
    this.points = points;
    if (color) this.color = color;
    if (unit !== undefined) this.unit = unit;
    this.redraw();
  }

  // Добавить одну точку "вживую"; maxAgeMs — удалить точки старше этого возраста
  push(point, maxAgeMs) {
    this.points.push(point);
    if (maxAgeMs) {
      const limit = Date.now() - maxAgeMs;
      while (this.points.length && this.points[0].t < limit) this.points.shift();
    }
    this.redraw();
  }

  destroy() { this.resizeObserver.disconnect(); }

  draw() {
    const { canvas, ctx } = this;
    const dpr = window.devicePixelRatio || 1;
    const W = canvas.clientWidth;
    const H = canvas.clientHeight;
    if (!W || !H) return;
    canvas.width = Math.round(W * dpr);
    canvas.height = Math.round(H * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, W, H);

    const css = getComputedStyle(document.documentElement);
    const muted = css.getPropertyValue('--muted').trim() || '#888';
    const lineCol = css.getPropertyValue('--line').trim() || '#333';
    const text = css.getPropertyValue('--text').trim() || '#fff';
    const card = css.getPropertyValue('--card').trim() || '#000';
    ctx.font = '12px system-ui, sans-serif';

    const pts = this.points.filter((p) => p.v != null && !Number.isNaN(p.v));
    if (pts.length < 2) {
      ctx.fillStyle = muted;
      ctx.textAlign = 'center';
      ctx.fillText('Нет данных за выбранный период', W / 2, H / 2);
      return;
    }

    // Поля графика
    const L = 56, R = 14, T = 14, B = 28;
    const t0 = pts[0].t.getTime();
    const t1 = pts[pts.length - 1].t.getTime();
    let vMin = Infinity, vMax = -Infinity;
    for (const p of pts) { if (p.v < vMin) vMin = p.v; if (p.v > vMax) vMax = p.v; }
    if (vMin > 0 && vMin < vMax * 0.5) vMin = 0;
    if (vMin === vMax) { vMin -= 1; vMax += 1; }
    const pad = (vMax - vMin) * 0.08;
    vMin -= pad; vMax += pad;

    const x = (t) => L + ((t - t0) / Math.max(1, t1 - t0)) * (W - L - R);
    const y = (v) => T + (1 - (v - vMin) / (vMax - vMin)) * (H - T - B);

    // Сетка и подписи
    ctx.strokeStyle = lineCol;
    ctx.lineWidth = 1;
    ctx.fillStyle = muted;
    ctx.textAlign = 'right';
    ctx.textBaseline = 'middle';
    for (let i = 0; i <= 4; i++) {
      const v = vMin + ((vMax - vMin) * i) / 4;
      const yy = Math.round(y(v)) + 0.5;
      ctx.beginPath(); ctx.moveTo(L, yy); ctx.lineTo(W - R, yy); ctx.stroke();
      ctx.fillText(formatValue(v), L - 8, yy);
    }
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    const longSpan = t1 - t0 > 36 * 3600 * 1000;
    const ticks = Math.max(2, Math.min(6, Math.floor((W - L) / 110)));
    for (let i = 0; i <= ticks; i++) {
      const t = t0 + ((t1 - t0) * i) / ticks;
      ctx.fillText(formatTime(new Date(t), longSpan), Math.min(W - R - 20, Math.max(L + 20, x(t))), H - B + 8);
    }

    // Линия и заливка под ней
    ctx.beginPath();
    pts.forEach((p, i) => (i ? ctx.lineTo(x(p.t), y(p.v)) : ctx.moveTo(x(p.t), y(p.v))));
    ctx.strokeStyle = this.color;
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    ctx.stroke();
    ctx.lineTo(x(pts[pts.length - 1].t), H - B);
    ctx.lineTo(x(pts[0].t), H - B);
    ctx.closePath();
    const grad = ctx.createLinearGradient(0, T, 0, H - B);
    grad.addColorStop(0, this.color + '55');
    grad.addColorStop(1, this.color + '00');
    ctx.fillStyle = grad;
    ctx.fill();

    // Подсказка при наведении
    if (this.hoverX != null && this.hoverX >= L) {
      const target = t0 + ((this.hoverX - L) / (W - L - R)) * (t1 - t0);
      let lo = 0, hi = pts.length - 1;
      while (lo < hi) { const mid = (lo + hi) >> 1; if (pts[mid].t.getTime() < target) lo = mid + 1; else hi = mid; }
      const p = pts[lo];
      const px = x(p.t), py = y(p.v);
      ctx.strokeStyle = muted;
      ctx.setLineDash([4, 4]);
      ctx.beginPath(); ctx.moveTo(px, T); ctx.lineTo(px, H - B); ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = this.color;
      ctx.beginPath(); ctx.arc(px, py, 4.5, 0, Math.PI * 2); ctx.fill();

      const label = `${formatValue(p.v)} ${this.unit}  ·  ${formatTime(p.t, true)}`;
      ctx.font = '600 12.5px system-ui, sans-serif';
      const tw = ctx.measureText(label).width + 16;
      const bx = Math.min(W - R - tw, Math.max(L, px - tw / 2));
      ctx.fillStyle = card;
      ctx.strokeStyle = lineCol;
      ctx.beginPath(); ctx.roundRect(bx, T, tw, 26, 7); ctx.fill(); ctx.stroke();
      ctx.fillStyle = text;
      ctx.textAlign = 'left';
      ctx.textBaseline = 'middle';
      ctx.fillText(label, bx + 8, T + 13);
    }
  }
}

function formatValue(v) {
  const a = Math.abs(v);
  if (a >= 10000) return (v / 1000).toFixed(1) + 'k';
  if (a >= 100) return Math.round(v).toString();
  return v.toFixed(1);
}

function formatTime(d, withDate) {
  const hm = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });
  return withDate ? d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' }) + ' ' + hm : hm;
}
