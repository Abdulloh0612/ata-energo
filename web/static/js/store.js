// =============================================================================
// store.js — общее хранилище данных в браузере.
// Здесь лежат текущий пользователь и все доступные ему устройства.
// WebSocket обновляет устройства, а страницы подписываются на изменения.
// =============================================================================

export const store = {
  /** @type {{id: number, can_view_all: boolean} | null} */
  me: null,                 // текущий пользователь (/api/me/)
  /** @type {{requests?: {view: boolean}, tasks?: {view: boolean}}} */
  crmAccess: {},
  devices: new Map(),       // id устройства -> устройство
  modems: new Map(),        // id модема -> модем
  byClient: new Map(),      // id клиента -> Set(id устройств)
  unreadAlerts: 0,
  listeners: new Set(),

  // Подписка на изменения. Возвращает функцию отписки.
  subscribe(fn) {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  },

  notify(event) { this.listeners.forEach((fn) => fn(event)); },

  // Загрузить список устройств (из API)
  setDevices(list) {
    this.devices.clear();
    this.byClient.clear();
    list.forEach((d) => this.addDevice(d));
  },

  addDevice(d) {
    this.devices.set(d.id, d);
    if (!this.byClient.has(d.client)) this.byClient.set(d.client, new Set());
    this.byClient.get(d.client).add(d.id);
  },

  removeDevice(id) {
    const d = this.devices.get(id);
    if (!d) return;
    this.devices.delete(id);
    this.byClient.get(d.client)?.delete(id);
  },

  // Все устройства одного клиента
  devicesOfClient(clientId) {
    const ids = this.byClient.get(Number(clientId));
    return ids ? [...ids].map((id) => this.devices.get(id)).filter(Boolean) : [];
  },

  // Пришли новые показания из WebSocket
  applyTelemetry(msg) {
    let d = this.devices.get(msg.device.id);
    if (!d) {
      // новое устройство, которого ещё нет в списке
      d = { ...msg.device, client: msg.device.client_id, modem: msg.device.modem_id, last_data: {} };
      this.addDevice(d);
    }
    d.last_data = msg.data;
    d.status = msg.device.status;
    d.last_seen = msg.data.timestamp;
    this.notify({ type: 'device', device: d });
  },

  // Загрузить список модемов (из API)
  setModems(list) {
    this.modems.clear();
    list.forEach((m) => this.modems.set(m.id, m));
  },

  modemsOfClient(clientId) {
    return [...this.modems.values()].filter((m) => m.client === Number(clientId));
  },

  // Пришёл статус модема из WebSocket (в сети / нет связи)
  applyModem(msg) {
    const m = this.modems.get(msg.modem.id) || { id: msg.modem.id, client: msg.modem.client_id };
    Object.assign(m, { serial: msg.modem.serial, status: msg.modem.status, last_seen: msg.modem.last_seen,
      has_generator: msg.modem.has_generator });
    this.modems.set(m.id, m);
    this.notify({ type: 'modem', modem: m });
  },

  // Изменился статус (online/offline)
  applyStatus(msg) {
    const d = this.devices.get(msg.device.id);
    if (!d) return;
    d.status = msg.device.status;
    this.notify({ type: 'device', device: d });
  },
};
