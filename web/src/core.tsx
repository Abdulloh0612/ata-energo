import { createContext, useContext, useEffect, useState } from "react";
import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { QueryClient, useQuery } from "@tanstack/react-query";

export type Theme = "solar" | "night" | "aurora" | "forest";
export const themes: Record<Theme, string> = {
  solar: "Solar Light",
  night: "Night Grid",
  aurora: "Aurora",
  forest: "Eco Forest",
};
export const usePrefs = create(
  persist(
    () => ({
      theme: (window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "night"
        : "solar") as Theme,
      compact: false,
      sound: false,
      notifications: true,
    }),
    { name: "ata-workspace-preferences" },
  ),
);
export interface Me {
  id: number;
  username: string;
  role: string;
  role_display: string;
  first_name: string;
  last_name: string;
  client_id: number | null;
  client?: { name: string } | null;
}
export interface Grant {
  view: boolean;
  create: boolean;
  edit: boolean;
  assign: boolean;
  delete: boolean;
  own: boolean;
}
export interface Access {
  requests: Grant;
  tasks: Grant;
  sections: Record<string, boolean>;
}
export interface Device {
  id: number;
  name: string;
  client: number;
  client_name: string;
  modem: number | null;
  modem_serial: string | null;
  device_id: string;
  device_type: string;
  type_display: string;
  serial_number: string;
  status: string;
  last_seen: string | null;
  created_at: string;
  last_data: Record<string, any>;
}
export interface Site {
  id: string;
  name: string;
  status: string;
  client_id: number | null;
  client_name: string | null;
  latitude: number | null;
  longitude: number | null;
  address: string;
  timezone: string;
  provider_code: string;
  provider_name: string;
  modem_ids: number[];
  archived: boolean;
}
export interface AlertItem {
  id: number;
  device: number;
  device_code: string;
  device_type: string;
  client_id: number;
  client_name: string;
  kind: string;
  text: string;
  created_at: string;
  is_read: boolean;
}
export const cache = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 20000, refetchOnWindowFocus: true },
    mutations: { retry: false },
  },
});
export const useAuth = create(
  persist(() => ({ access: "", refresh: "" }), {
    name: "ata-tab-session",
    storage: createJSONStorage(() => sessionStorage),
  }),
);
export function logout() {
  useAuth.setState({ access: "", refresh: "" });
  void cache.cancelQueries();
  cache.clear();
}
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
async function response<T>(r: Response): Promise<T> {
  const body = await r.text();
  let data;
  try {
    data = body ? JSON.parse(body) : null;
  } catch {
    data = null;
  }
  if (!r.ok) {
    const fields =
      data && typeof data === "object"
        ? Object.entries(data)
            .map(([field, value]) => field + " — " + String(value))
            .join(". ")
        : "";
    throw new ApiError(
      data?.detail || fields || "Сервер не выполнил запрос (" + r.status + ").",
      r.status,
    );
  }
  return data as T;
}
export async function login(username: string, password: string) {
  const res = await response<{ access: string; refresh: string; user: Me }>(
    await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    }),
  );
  cache.clear();
  cache.setQueryData(["/me"], res.user);
  useAuth.setState({ access: res.access, refresh: res.refresh });
}
let refreshJob: Promise<void> | undefined;
export function refreshAccess() {
  if (refreshJob) return refreshJob;
  const token = useAuth.getState().refresh;
  refreshJob = (async () => {
    const r = await fetch("/api/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh: token }),
    });
    if (token !== useAuth.getState().refresh) return;
    if (r.status === 401) {
      logout();
      throw new ApiError("Войдите в систему ещё раз.", 401);
    }
    const data = await response<{ access: string }>(r);
    if (token === useAuth.getState().refresh)
      useAuth.setState({ access: data.access });
  })().finally(() => {
    refreshJob = undefined;
  });
  return refreshJob;
}
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const { access, refresh } = useAuth.getState();
  const request = (token: string) =>
    fetch("/api" + path, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        Authorization: "Bearer " + token,
        ...init.headers,
      },
    });
  let r = await request(access);
  if (r.status === 401 && refresh === useAuth.getState().refresh && refresh) {
    if (access === useAuth.getState().access) await refreshAccess();
    if (refresh === useAuth.getState().refresh)
      r = await request(useAuth.getState().access);
  }
  return response<T>(r);
}
export function useList<T>(
  path: string,
  enabled = true,
  refetchInterval?: number,
) {
  return useQuery<T>({
    queryKey: [path],
    queryFn: ({ signal }) => api<T>(path, { signal }),
    enabled,
    refetchInterval,
  });
}
export const Session = createContext<{ me: Me; access: Access } | null>(null);
export function useSession() {
  const value = useContext(Session);
  if (!value) throw new Error("Требуется вход");
  return value;
}
export function useLiveDevices() {
  return useList<Device[]>(
    useSession().me.role === "user" ? "/my/devices" : "/devices",
  );
}
export function fmtNumber(value: number, digits = 1) {
  return value.toLocaleString("ru-RU", { maximumFractionDigits: digits });
}
export function relative(value: string | null) {
  if (!value) return "нет данных";
  const seconds = Math.max(
    0,
    Math.floor((Date.now() - Date.parse(value)) / 1000),
  );
  if (seconds < 60) return seconds + " сек назад";
  const mins = Math.floor(seconds / 60);
  if (mins < 60) return mins + " мин назад";
  if (mins < 1440)
    return Math.floor(mins / 60) + " ч " + (mins % 60) + " мин назад";
  return Math.floor(mins / 1440) + " дн назад";
}
export function toast(message: string) {
  window.dispatchEvent(new CustomEvent("ata:toast", { detail: message }));
}
export function useStream(role: string) {
  const token = useAuth((s) => s.access);
  const [state, setState] = useState<"connecting" | "online" | "offline">(
    "connecting",
  );
  useEffect(() => {
    let stop = false,
      retries = 0,
      retry = 0,
      ws: WebSocket | undefined;
    const pending = new Map<
      number,
      { status: string; data?: Device["last_data"] }
    >();
    const path = role === "user" ? "/my/devices" : "/devices";
    type ModemEvent = { id: number; status: string; last_seen: string | null };
    const modems = new Map<number, ModemEvent>();
    function flush() {
      if (modems.size) {
        cache.setQueryData<ModemEvent[]>(["/modems"], (list) =>
          list?.map((modem) => {
            const event = modems.get(modem.id);
            return event
              ? { ...modem, status: event.status, last_seen: event.last_seen }
              : modem;
          }),
        );
        // Catalog station status belongs to its provider. Only local station
        // identities correspond directly to the modem sending this heartbeat.
        const stationsPath = role === "user" ? "/my/sites" : "/crm/sites";
        cache.setQueryData<Site[]>([stationsPath], (list) => {
          if (!list) return list;
          let changed = false;
          const patched = list.map((site) => {
            if (!site.id.startsWith("m")) return site;
            const event = modems.get(Number(site.id.slice(1)));
            if (!event) return site;
            const status = event.status === "online" ? "normal" : "offline";
            if (status === site.status) return site;
            changed = true;
            return { ...site, status };
          });
          return changed ? patched : list;
        });
        modems.clear();
      }
      if (!pending.size) return;
      cache.setQueryData<Device[]>([path], (old) =>
        old?.map((d) => {
          const msg = pending.get(d.id);
          return msg
            ? {
                ...d,
                status: msg.status,
                last_seen: msg.data?.timestamp ?? d.last_seen,
                last_data: msg.data ?? d.last_data,
              }
            : d;
        }),
      );
      pending.clear();
    }
    const timer = window.setInterval(flush, 750);
    function connect() {
      if (stop) return;
      ws = new WebSocket(
        (location.protocol === "https:" ? "wss://" : "ws://") +
          location.host +
          "/ws/telemetry?token=" +
          encodeURIComponent(useAuth.getState().access),
      );
      ws.onmessage = (e) => {
        if (stop) return;
        let m;
        try {
          m = JSON.parse(e.data);
        } catch {
          return;
        }
        if (m.type === "hello") {
          if (retries) {
            for (const key of [
              path,
              "/alerts",
              "/crm/sites",
              "/my/sites",
              "/modems",
            ])
              void cache.invalidateQueries({ queryKey: [key] });
          }
          retries = 0;
          setState("online");
        }
        if (m.device && (m.type === "telemetry" || m.type === "status")) {
          pending.set(m.device.id, {
            status: m.device.status,
            data: m.data ?? pending.get(m.device.id)?.data,
          });
        }
        if (m.type === "modem" && m.modem) modems.set(m.modem.id, m.modem);
        if (m.type === "alert") {
          const a = m.alert;
          const item: AlertItem = {
            ...a,
            device: a.device.id,
            device_code: a.device.device_id,
            device_type: a.device.device_type,
            client_id: a.device.client_id,
          };
          cache.setQueryData<AlertItem[]>(["/alerts"], (old) =>
            old
              ? [item, ...old.filter((x) => x.id !== item.id)].slice(0, 200)
              : old,
          );
          void cache.invalidateQueries({ queryKey: ["/crm/active-alerts"] });
          if (a.kind === "error") {
            const prefs = usePrefs.getState();
            if (prefs.notifications) toast(a.text);
            if (prefs.sound) beep();
          }
        }
      };
      ws.onclose = async (e) => {
        if (stop) return;
        setState("offline");
        if (e.code === 4003) return;
        if (e.code === 4001) {
          try {
            await refreshAccess();
          } catch {
            /* reconnection retries below */
          }
        }
        if (!stop)
          retry = window.setTimeout(
            connect,
            Math.min(15000, 1000 * 2 ** retries++),
          );
      };
    }
    connect();
    return () => {
      stop = true;
      clearInterval(timer);
      clearTimeout(retry);
      ws?.close();
      pending.clear();
    };
  }, [token, role]);
  return state;
}
let audio: AudioContext | undefined;
export function beep() {
  try {
    audio ??= new AudioContext();
    void audio.resume().then(() => {
      if (!audio) return;
      const o = audio.createOscillator(),
        g = audio.createGain();
      o.frequency.value = 660;
      g.gain.setValueAtTime(0.055, audio.currentTime);
      g.gain.exponentialRampToValueAtTime(0.001, audio.currentTime + 0.13);
      o.connect(g).connect(audio.destination);
      o.start();
      o.stop(audio.currentTime + 0.15);
    });
  } catch {
    /* Audio may be unavailable on the current device. */
  }
}
