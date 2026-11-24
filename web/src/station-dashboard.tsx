import { useCallback, useEffect, useRef, useState, useMemo } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import { InverterDashboard } from "./inverter-dashboard";
import { api, useList, usePrefs, useLiveDevices, type Device } from "./core";

type Station = {
  id: string;
  name: string;
  status: string;
  timezone: string;
  address: string;
  provider_code: string;
  archived: boolean;
  client_id: number | null;
  client_name: string | null;
  modem_ids: number[];
};
type Reading = {
  deviceSn: string;
  deviceType: string;
  deviceState: number;
  dataList: { key: string; value: string; unit: string }[];
};
type CloudSnapshot = {
  station: { installedCapacity: number | null } | null;
  latest: {
    chargePower: number | null;
    dischargePower: number | null;
    batteryPower: number | null;
    batterySOC: number | null;
    lastUpdateTime: string | null;
  } | null;
  flow: Record<string, unknown>;
  inverter_flow: Record<string, unknown> | null;
  devices: {
    deviceId: number | string;
    deviceSn: string;
    deviceType: string;
    connectStatus: number;
  }[];
  readings: Reading[];
  warnings: string[];
};
type EnergyPoint = {
  timeStamp: string;
  year: number | null;
  month: number | null;
  day: number | null;
  generationPower: number | null;
  consumptionPower: number | null;
  purchasePower: number | null;
  wirePower: number | null;
  gridPower: number | null;
  batteryPower: number | null;
  batterySOC: number | null;
  generationValue: number | null;
  consumptionValue: number | null;
  purchaseValue: number | null;
  gridValue: number | null;
  chargeValue: number | null;
  dischargeValue: number | null;
};
type History = { stationDataItems: EnergyPoint[] };
type Period = { mode: "D" | "M" | "Y" | "T"; date: string };
type BridgeRequest = {
  channel: string;
  type: string;
  id?: string;
  context?: "solar" | "history" | "power";
  period?: Period;
  requestId?: number;
  height?: number;
  tab?: string;
};
function dateInZone(date: Date, timezone: string) {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}
// Смещение часового пояса как в Deye Cloud: "UTC+06:00".
function utcOffset(date: Date, timezone: string) {
  const name = new Intl.DateTimeFormat("en-US", {
    timeZone: timezone,
    timeZoneName: "longOffset",
  })
    .formatToParts(date)
    .find((p) => p.type === "timeZoneName")?.value;
  if (!name || name === "GMT") return "UTC+00:00";
  return name.replace("GMT", "UTC");
}
function cloudDate(value: string | null | undefined) {
  if (!value) return null;
  const numeric = Number(value);
  const date = new Date(Number.isFinite(numeric) ? numeric * 1000 : value);
  return Number.isFinite(date.getTime()) ? date : null;
}
function flowValues(
  raw: Record<string, unknown> | null | undefined,
  soc: number | null | undefined,
) {
  function power(key: string) {
    const value = raw?.[key];
    const parts = Array.isArray(value) ? value : [value];
    const powers = parts.map(
      (part) => (part as { power?: number } | undefined)?.power,
    );
    return powers.length && powers.every((p) => typeof p === "number")
      ? powers.reduce((total, p) => total! + p!, 0)
      : null;
  }
  return {
    pv: power("pv"),
    grid: power("grid"),
    battery: power("battery"),
    load: power("load"),
    soc: soc ?? (raw?.battery as { soc?: number } | undefined)?.soc ?? null,
    offGrid:
      typeof (raw?.grid as { voltage?: number } | undefined)?.voltage ===
      "number"
        ? (raw!.grid as { voltage: number }).voltage < 50
        : null,
  };
}
function sum(points: EnergyPoint[], key: keyof EnergyPoint) {
  const values = points
    .map((p) => p[key])
    .filter((v): v is number => typeof v === "number");
  return values.length ? values.reduce((a, b) => a + b, 0) : null;
}
function balance(points: EnergyPoint[]) {
  if (!points.length) return null;
  const consumption = sum(points, "consumptionValue");
  const purchased = sum(points, "purchaseValue");
  const discharge = sum(points, "dischargeValue");
  return {
    production: sum(points, "generationValue"),
    consumption,
    purchased,
    discharge,
    charge: sum(points, "chargeValue"),
    feedIn: sum(points, "gridValue"),
    pvUsed:
      consumption !== null && purchased !== null && discharge !== null
        ? Math.max(0, consumption - purchased - discharge)
        : null,
  };
}
function powerRows(points: EnergyPoint[], timezone: string) {
  return points
    .flatMap((p) => {
      const date = cloudDate(p.timeStamp);
      if (!date) return [];
      const parts = new Intl.DateTimeFormat("en-GB", {
        timeZone: timezone,
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      }).formatToParts(date);
      const minute =
        Number(parts.find((v) => v.type === "hour")?.value) * 60 +
        Number(parts.find((v) => v.type === "minute")?.value);
      const kw = (value: number | null) =>
        typeof value === "number" ? value / 1000 : null;
      return [
        {
          minute,
          production: kw(p.generationPower),
          pv: null,
          generator: null,
          gridInverter: null,
          grid: kw(p.gridPower ?? p.purchasePower),
          battery: kw(p.batteryPower),
          consumption: kw(p.consumptionPower),
          soc: p.batterySOC,
        },
      ];
    })
    .sort((a, b) => a.minute - b.minute);
}
function historyRows(points: EnergyPoint[], period: Period) {
  const known = new Map(
    points.map((p) => {
      const date = [
        p.year,
        p.month && String(p.month).padStart(2, "0"),
        p.day && String(p.day).padStart(2, "0"),
      ]
        .filter(Boolean)
        .join("-");
      return [date, p];
    }),
  );
  let dates: string[];
  if (period.mode === "M") {
    const count = new Date(
      Number(period.date.slice(0, 4)),
      Number(period.date.slice(5, 7)),
      0,
    ).getDate();
    dates = Array.from(
      { length: count },
      (_, i) => period.date + "-" + String(i + 1).padStart(2, "0"),
    );
  } else if (period.mode === "Y") {
    dates = Array.from(
      { length: 12 },
      (_, i) => period.date + "-" + String(i + 1).padStart(2, "0"),
    );
  } else dates = Array.from(known.keys()).sort();
  return dates.map((date) => {
    const point = known.get(date);
    const negative = (v: number | null | undefined) =>
      typeof v === "number" ? -Math.abs(v) : null;
    return {
      date,
      label:
        period.mode === "M"
          ? String(Number(date.slice(-2)))
          : period.mode === "Y"
            ? new Date(date + "-01T12:00:00Z").toLocaleString("en-US", {
                month: "short",
                timeZone: "UTC",
              })
            : date,
      production: point?.generationValue,
      discharge: point?.dischargeValue,
      purchased: point?.purchaseValue,
      consumption: negative(point?.consumptionValue),
      charge: negative(point?.chargeValue),
      feedIn: negative(point?.gridValue),
    };
  });
}
function dashboardPalette() {
  return Object.fromEntries(
    Object.entries({
      bg: "--bg",
      card: "--surface",
      soft: "--surface-2",
      ink: "--text",
      muted: "--text-muted",
      border: "--border",
      accent: "--accent",
      green: "--success",
      danger: "--danger",
    }).map(([key, token]) => [
      key,
      getComputedStyle(document.documentElement).getPropertyValue(token).trim(),
    ]),
  );
}
function localFlow(devices: Device[]) {
  const power = (kind: string) => {
    const values = devices
      .filter((d) => d.device_type === kind)
      .map((d) => (d.status === "online" ? d.last_data?.power : null));
    return values.length && values.every((v) => typeof v === "number")
      ? (values as number[]).reduce((a, b) => a + b, 0)
      : null;
  };
  const batteries = devices.filter(
    (d) => d.device_type === "battery" && d.status === "online",
  );
  const soc = batteries
    .map((d) => d.last_data?.battery_percent)
    .filter((v): v is number => typeof v === "number");
  const voltage = devices.find(
    (d) => d.device_type === "grid" && d.status === "online",
  )?.last_data?.voltage;
  return {
    generator: power("generator"),
    pv: power("panel"),
    battery: power("battery"),
    grid: power("grid"),
    load: power("load"),
    soc: soc.length ? soc.reduce((a, b) => a + b, 0) / soc.length : null,
    offGrid: typeof voltage === "number" ? voltage < 50 : null,
  };
}
async function localPowerProfile(
  devices: Device[],
  day: string,
  zone: string,
  cache: QueryClient,
) {
  const start = new Date(day + "T00:00:00Z");
  if (!Number.isFinite(start.getTime())) return [];
  const from = new Date(start.getTime() - 14 * 3600000).toISOString();
  const to = new Date(
    Math.min(Date.now(), start.getTime() + 38 * 3600000),
  ).toISOString();
  const readings = await Promise.all(
    devices.map(async (device) => {
      const path =
        "/devices/" +
        device.id +
        "/history?" +
        new URLSearchParams({ from, to, limit: "5000" });
      const reply = await cache.fetchQuery({
        queryKey: [path],
        queryFn: ({ signal }) =>
          api<{ points: ({ timestamp: string } & Record<string, unknown>)[] }>(
            path,
            { signal },
          ),
        staleTime: 180000,
      });
      return { device, points: reply.points };
    }),
  );
  const buckets = new Map<number, Map<number, Record<string, unknown>>>();
  for (const { device, points } of readings)
    for (const point of points) {
      const date = new Date(point.timestamp);
      if (!Number.isFinite(date.getTime()) || dateInZone(date, zone) !== day)
        continue;
      const time = date
        .toLocaleTimeString("en-GB", { timeZone: zone, hour12: false })
        .split(":");
      const minute =
        Math.floor((Number(time[0]) * 60 + Number(time[1])) / 5) * 5;
      if (!buckets.has(minute)) buckets.set(minute, new Map());
      buckets.get(minute)!.set(device.id, point);
    }
  return Array.from(buckets, ([minute, points]) => {
    const measured = devices.map((d) => ({
      ...d,
      status: points.has(d.id) ? "online" : "offline",
      last_data: points.get(d.id) || {},
    }));
    const values = localFlow(measured);
    const kw = (v: number | null) => (v === null ? null : v / 1000);
    return {
      minute,
      production: kw(values.pv),
      pv: kw(values.pv),
      consumption: kw(values.load),
      battery: kw(values.battery),
      grid: kw(values.grid),
      soc: values.soc,
      generator: kw(values.generator),
      gridInverter: null,
    };
  }).sort((a, b) => a.minute - b.minute);
}
export function StationDashboard({ site }: { site: Station }) {
  const [params, setParams] = useSearchParams();
  const inverter = params.get("inverter");
  if (inverter)
    return (
      <InverterDashboard
        key={inverter}
        site={site}
        deviceId={inverter}
        onClose={() => {
          const next = new URLSearchParams(params);
          next.delete("inverter");
          next.set("tab", "devices");
          setParams(next);
        }}
      />
    );
  return <StationOverview site={site} />;
}
function StationOverview({ site }: { site: Station }) {
  const frame = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(1100);
  const [ready, setReady] = useState(false);
  const navigate = useNavigate();
  const cache = useQueryClient();
  const theme = usePrefs((p) => p.theme);
  const cloud =
    ["deye", "valueclouds"].includes(site.provider_code) &&
    site.id.startsWith("s");
  const localDevices = useLiveDevices();
  const attached = useMemo(
    () =>
      (localDevices.data || []).filter(
        (d) => d.modem !== null && site.modem_ids.includes(d.modem),
      ),
    [localDevices.data, site.modem_ids],
  );
  const attachedRef = useRef(attached);
  attachedRef.current = attached;
  const deviceLinks = JSON.stringify(
    attached.map(({ id, serial_number, modem_serial, device_type }) => ({
      id,
      serial_number,
      modem_serial,
      device_type,
    })),
  );
  const path =
    (site.provider_code === "valueclouds"
      ? "/crm/valueclouds/sites/"
      : "/crm/sites/") + encodeURIComponent(site.id);
  const snapshot = useList<CloudSnapshot>(path + "/telemetry", cloud, 180000);
  const snapshotRef = useRef(snapshot.data);
  snapshotRef.current = snapshot.data;
  const post = useCallback((data: Record<string, unknown>) => {
    frame.current?.contentWindow?.postMessage(
      { channel: "ata-station", ...data },
      window.location.origin,
    );
  }, []);
  useEffect(() => {
    if (!ready || cloud || !localDevices.data) return;
    const latest = attached
      .map((d) => d.last_seen)
      .filter((v): v is string => !!v)
      .sort()
      .at(-1);
    post({
      type: "init",
      initialTab: new URLSearchParams(window.location.search).get("tab"),
      palette: dashboardPalette(),
      theme: theme === "solar" ? "light" : "dark",
      today: dateInZone(new Date(), site.timezone),
      station: {
        ...site,
        online: site.status === "normal",
        client: site.client_id ? { name: site.client_name || "Клиент" } : null,
        onlineInverters: attached.filter(
          (d) => d.device_type === "inverter" && d.status === "online",
        ).length,
        updated: latest
          ? new Date(latest).toLocaleString("sv-SE", {
              timeZone: site.timezone,
            })
          : null,
        flow: localFlow(attached),
        inverterFlow: null,
        summary: {},
        alerts: null,
        devices: attached.map((d) => ({
          id: d.id,
          localId: d.id,
          kind: d.device_type,
          deviceSn: d.serial_number,
          deviceType: d.name || d.device_type,
          online: d.status === "online",
          metrics: Object.entries(d.last_data || {})
            .filter(([key]) => key !== "timestamp")
            .map(([key, value]) => ({
              key,
              value: value === null ? "—" : String(value),
              unit:
                (
                  {
                    power: "W",
                    voltage: "V",
                    current: "A",
                    temperature: "°C",
                    battery_percent: "%",
                  } as Record<string, string>
                )[key] || "",
            })),
        })),
      },
    });
  }, [ready, cloud, localDevices.data, attached, site, theme, post]);
  useEffect(() => {
    if (ready && !cloud && localDevices.error)
      post({ type: "error", message: localDevices.error.message });
  }, [ready, cloud, localDevices.error, post]);
  useEffect(() => {
    if (!ready || cloud) return;
    const timer = window.setInterval(() => post({ type: "reload" }), 180000);
    return () => window.clearInterval(timer);
  }, [ready, cloud, post]);
  const getHistory = useCallback(
    (period: Period) => {
      const query = new URLSearchParams({
        mode: period.mode,
        date: period.date,
      });
      const url = path + "/history?" + query;
      return cache.fetchQuery({
        queryKey: [url],
        queryFn: ({ signal }) => api<History>(url, { signal }),
        staleTime: 180000,
      });
    },
    [cache, path],
  );
  useEffect(() => {
    if (!ready || !cloud || !snapshot.data) return;
    const data = snapshot.data;
    const linked: Pick<
      Device,
      "id" | "serial_number" | "modem_serial" | "device_type"
    >[] = JSON.parse(deviceLinks);
    const date = cloudDate(data.latest?.lastUpdateTime);
    const palette = dashboardPalette();
    const stationFlow = flowValues(data.flow, data.latest?.batterySOC);
    const latest = data.latest;
    if (
      latest &&
      (typeof latest.chargePower === "number" ||
        typeof latest.dischargePower === "number")
    ) {
      stationFlow.battery =
        Math.abs(latest.chargePower ?? 0) -
        Math.abs(latest.dischargePower ?? 0);
    } else if (typeof latest?.batteryPower === "number")
      stationFlow.battery = -latest.batteryPower;
    post({
      type: "init",
      initialTab: new URLSearchParams(window.location.search).get("tab"),
      refresh: true,
      palette,
      theme: theme === "solar" ? "light" : "dark",
      today: dateInZone(new Date(), site.timezone),
      station: {
        ...site,
        client: site.client_id ? { name: site.client_name || "Клиент" } : null,
        capacity: data.station?.installedCapacity,
        online: site.status === "normal" || site.status === "warning",
        onlineInverters: data.devices.filter(
          (d) => d.deviceType === "INVERTER" && d.connectStatus !== 0,
        ).length,
        updated: date
          ? date.toLocaleString("sv-SE", { timeZone: site.timezone }) +
            " (" +
            utcOffset(date, site.timezone) +
            ")"
          : null,
        flow: stationFlow,
        inverterFlow: data.inverter_flow
          ? flowValues(data.inverter_flow, data.latest?.batterySOC)
          : null,
        summary: {},
        alerts: null,
        devices: data.devices.map((d) => ({
          id: d.deviceId,
          localId: linked.find(
            (device) =>
              device.serial_number === d.deviceSn ||
              (device.modem_serial === d.deviceSn &&
                device.device_type === "inverter"),
          )?.id,
          ...d,
          online: d.connectStatus !== 0,
          metrics:
            data.readings.find(
              (r) => r.deviceSn === d.deviceSn && r.deviceType === d.deviceType,
            )?.dataList || [],
        })),
      },
    });
    if (data.warnings.length)
      post({ type: "error", message: data.warnings.join(". ") });
  }, [ready, snapshot.data, cloud, deviceLinks, site, theme, post]);
  useEffect(() => {
    if (ready && cloud && snapshot.error)
      post({ type: "error", message: snapshot.error.message });
  }, [ready, cloud, snapshot.error, post]);
  useEffect(() => {
    let active = true;
    async function summary() {
      try {
        const total = await getHistory({ mode: "T", date: "" });
        const today = dateInZone(new Date(), site.timezone);
        const month = await getHistory({ mode: "M", date: today.slice(0, 7) });
        const daily = month.stationDataItems.find(
          (p) => p.day === Number(today.slice(-2)),
        );
        if (active)
          post({
            type: "summary",
            value: {
              production: sum(total.stationDataItems, "generationValue"),
              consumption: sum(total.stationDataItems, "consumptionValue"),
              daily: daily?.generationValue ?? null,
              weather: null,
            },
          });
      } catch (err) {
        if (active)
          post({
            type: "error",
            message:
              err instanceof Error
                ? err.message
                : "Не удалось загрузить сводку",
          });
      }
    }
    if (ready && cloud && snapshot.data) void summary();
    return () => {
      active = false;
    };
  }, [ready, cloud, snapshot.data, site.timezone, getHistory, post]);
  useEffect(() => {
    let active = true;
    async function receive(event: MessageEvent<BridgeRequest>) {
      if (
        event.origin !== window.location.origin ||
        event.source !== frame.current?.contentWindow ||
        event.data?.channel !== "ata-station"
      )
        return;
      const message = event.data;
      switch (message.type) {
        case "ready":
          setReady(true);
          break;
        case "height":
          if (typeof message.height === "number")
            setHeight(Math.min(50000, Math.max(500, message.height)));
          break;
        case "client":
          if (site.client_id) navigate("/clients/" + site.client_id);
          break;
        case "inverter": {
          const match = cloud
            ? snapshotRef.current?.devices.some(
                (d) =>
                  String(d.deviceId) === String(message.id) &&
                  d.deviceType === "INVERTER",
              )
            : attachedRef.current.some(
                (d) =>
                  String(d.id) === String(message.id) &&
                  d.device_type === "inverter",
              );
          if (match)
            navigate(
              "/sites/" +
                encodeURIComponent(site.id) +
                "?inverter=" +
                encodeURIComponent(message.id!),
            );
          break;
        }
        case "device":
          if (
            attachedRef.current.some((d) => String(d.id) === String(message.id))
          )
            navigate("/devices/" + message.id);
          break;
        case "equipment": {
          const device = attachedRef.current.find(
            (d) => d.device_type === message.id,
          );
          if (device) navigate("/devices/" + device.id);
          else post({ type: "openDevices" });
          break;
        }
        case "refresh":
          await cache.invalidateQueries({
            predicate: (q) =>
              typeof q.queryKey[0] === "string" &&
              (q.queryKey[0].startsWith(path) ||
                (!cloud && q.queryKey[0] === "/devices")),
          });
          if (active && !cloud) post({ type: "reload" });
          break;
        case "download":
          window.open("/station-dashboard.html", "_blank", "noopener");
          break;
        case "tab":
          if (
            message.tab &&
            ["overview", "devices", "alerts", "info"].includes(message.tab)
          ) {
            const query = new URLSearchParams(window.location.search);
            query.set("tab", message.tab);
            navigate(window.location.pathname + "?" + query, { replace: true });
          }
          if (message.tab === "alerts") {
            try {
              if (!cloud) {
                const alerts =
                  await api<
                    { device: number; text: string; created_at: string }[]
                  >("/alerts");
                if (active)
                  post({
                    type: "alerts",
                    value: alerts
                      .filter((a) =>
                        attachedRef.current.some((d) => d.id === a.device),
                      )
                      .map((a) => ({
                        time: new Date(a.created_at).toLocaleString("sv-SE", {
                          timeZone: site.timezone,
                        }),
                        text: a.text,
                      })),
                  });
                break;
              }
              const reply = await api<{
                stationAlertItems: {
                  alertName: string;
                  alertStartTime: number;
                }[];
              }>(path + "/alerts");
              if (active)
                post({
                  type: "alerts",
                  value: reply.stationAlertItems.map((a) => ({
                    time: new Date(a.alertStartTime * 1000).toLocaleString(
                      "sv-SE",
                      { timeZone: site.timezone },
                    ),
                    text: a.alertName,
                  })),
                });
            } catch (err) {
              if (active)
                post({
                  type: "error",
                  message:
                    err instanceof Error
                      ? err.message
                      : "Не удалось загрузить оповещения",
                });
            }
          }
          break;
        case "request":
          if (!message.context || !message.period) break;
          try {
            if (!cloud) {
              const value =
                message.context === "power"
                  ? await localPowerProfile(
                      attachedRef.current,
                      message.period.date,
                      site.timezone,
                      cache,
                    )
                  : null;
              if (active)
                post({
                  type: "result",
                  stationId: site.id,
                  context: message.context,
                  period: message.period,
                  requestId: message.requestId,
                  value,
                });
              break;
            }
            const result = await getHistory(message.period);
            const value =
              message.context === "solar"
                ? balance(result.stationDataItems)
                : message.context === "power"
                  ? powerRows(result.stationDataItems, site.timezone)
                  : historyRows(result.stationDataItems, message.period);
            if (active)
              post({
                type: "result",
                stationId: site.id,
                context: message.context,
                period: message.period,
                requestId: message.requestId,
                value,
              });
          } catch (err) {
            if (active)
              post({
                type: "result",
                stationId: site.id,
                context: message.context,
                period: message.period,
                requestId: message.requestId,
                value: null,
                error:
                  err instanceof Error
                    ? err.message
                    : "Не удалось загрузить данные из облака",
              });
          }
          break;
      }
    }
    window.addEventListener("message", receive);
    return () => {
      active = false;
      window.removeEventListener("message", receive);
    };
  }, [
    cache,
    getHistory,
    navigate,
    path,
    post,
    site.id,
    site.timezone,
    cloud,
    site.client_id,
  ]);
  return (
    <iframe
      ref={frame}
      src="/station-dashboard.html?source=crm"
      title={"Мониторинг станции · " + site.name}
      onLoad={() => setReady(true)}
      style={{
        width: "100%",
        height,
        border: 0,
        borderRadius: 14,
        display: "block",
      }}
    />
  );
}
