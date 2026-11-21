// =============================================================================
// ws.js — подключение к WebSocket ws://домен/ws/telemetry/?token=...
// Само переподключается при обрыве связи и обновляет токен при необходимости.
//
// Пример:  live.on('telemetry', msg => console.log(msg.device.device_id, msg.data.power));
// =============================================================================
import { auth, refreshAccess } from './api.js';

export class LiveSocket {
  constructor() {
    this.handlers = {};
    this.ws = null;
    this.retry = 0;
    this.stopped = true;
  }

  // Подписаться на тип сообщения: 'telemetry', 'status', 'alert', 'state' (состояние связи)
  on(type, fn) { (this.handlers[type] ||= []).push(fn); }

  emit(type, data) { (this.handlers[type] || []).forEach((fn) => fn(data)); }

  start() { this.stopped = false; this.connect(); }

  stop() {
    this.stopped = true;
    clearInterval(this.pingTimer);
    clearTimeout(this.retryTimer);
    const ws = this.ws;
    this.ws = null;
    if (ws) ws.close(1000);
  }

  connect() {
    if (this.stopped) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/ws/telemetry/?token=${encodeURIComponent(auth.access || '')}`);
    this.ws = ws;
    this.emit('state', 'connecting');

    ws.onopen = () => {
      this.retry = 0;
      this.emit('state', 'online');
      // ping раз в 25 сек, чтобы прокси/роутер не закрыл "тихое" соединение
      this.pingTimer = setInterval(() => ws.readyState === 1 && ws.send('{"type":"ping"}'), 25000);
    };

    ws.onmessage = (event) => {
      let msg;
      try { msg = JSON.parse(event.data); } catch { return; }
      this.emit(msg.type, msg);
    };

    ws.onclose = async (event) => {
      clearInterval(this.pingTimer);
      if (this.ws !== ws || this.stopped) return; // соединение закрыли мы сами
      this.emit('state', 'offline');
      if (event.code === 4001) {
        // токен устарел — обновляем и пробуем снова
        try { await refreshAccess(); } catch { this.emit('authLost'); return; }
      }
      const delay = Math.min(1000 * 2 ** this.retry, 15000);
      this.retry += 1;
      this.retryTimer = setTimeout(() => this.connect(), delay);
    };
  }
}

export const live = new LiveSocket();
