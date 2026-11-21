import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
import {
  Area,
  Brush,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  Activity,
  BatteryCharging,
  Cpu,
  Factory,
  House,
  MapPinOff,
  PanelsTopLeft,
  UtilityPole,
} from "lucide-react";
import L from "leaflet";
import "leaflet.markercluster";
import "leaflet/dist/leaflet.css";
import "./visuals.css";

export type Period = "day" | "week" | "month";
type Metric =
  "power" | "voltage" | "current" | "temperature" | "battery_percent";
export type HistorySample = { timestamp: string | number } & Partial<
  Record<Metric, number | null>
>;

const periods: Record<Period, string> = {
  day: "День",
  week: "Неделя",
  month: "Месяц",
};
const metrics = {
  power: { name: "Мощность", unit: "кВт" },
  voltage: { name: "Напряжение", unit: "В" },
  current: { name: "Ток", unit: "А" },
  temperature: { name: "Температура", unit: "°C" },
  battery_percent: { name: "Заряд", unit: "%" },
};
const metricKeys = Object.keys(metrics) as Metric[];
const number = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 });

function finite(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

type ChartPoint = { time: number } & Partial<Record<Metric, number | null>>;

function preparePoints(input: HistorySample[], period: Period, gapMs?: number) {
  const rows: ChartPoint[] = [];
  for (const sample of input) {
    const time =
      typeof sample.timestamp === "number"
        ? sample.timestamp
        : Date.parse(sample.timestamp);
    if (!finite(time)) continue;
    const row: ChartPoint = { time };
    for (const key of metricKeys) {
      const value = sample[key];
      row[key] = finite(value)
        ? key === "power"
          ? value / 1000
          : value
        : null;
    }
    rows.push(row);
  }
  rows.sort((a, b) => a.time - b.time);
  const intervals = rows
    .slice(1)
    .map((row, i) => row.time - rows[i].time)
    .filter((ms) => ms > 0)
    .sort((a, b) => a - b);
  // When no cadence is supplied, separate long silences relative to the observed cadence.
  const median = intervals[Math.floor((intervals.length - 1) / 2)] ?? 0;
  const periodLimit = { day: 3600000, week: 21600000, month: 86400000 }[period];
  const maxGap =
    gapMs ?? (intervals.length > 1 ? Math.max(30000, median * 3) : periodLimit);
  const chart: ChartPoint[] = [];
  for (const row of rows) {
    const previous = chart.at(-1);
    if (previous && row.time - previous.time > maxGap) {
      chart.push({ time: (row.time + previous.time) / 2 });
    }
    chart.push(row);
  }
  return chart;
}

interface ChartProps {
  points: HistorySample[];
  period?: Period;
  onPeriodChange?: (period: Period) => void;
  multiMetric?: boolean;
  timezone?: string;
  loading?: boolean;
  error?: string;
  gapMs?: number;
}

export function TelemetryChart({
  points,
  period = "day",
  onPeriodChange,
  multiMetric = false,
  timezone = "Asia/Bishkek",
  loading = false,
  error,
  gapMs,
}: ChartProps) {
  const paint = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  const [selected, setSelected] = useState<Metric[]>(["power"]);
  const data = useMemo(
    () => preparePoints(points, period, gapMs),
    [points, period, gapMs],
  );
  const primary = multiMetric ? selected[0] : "power";
  const secondary = multiMetric ? selected[1] : undefined;
  const active = secondary ? [primary, secondary] : [primary];
  const hasSeries = data.some((row) => active.some((key) => finite(row[key])));
  const { axis, exact } = useMemo(() => {
    const create = (options: Intl.DateTimeFormatOptions) => {
      try {
        return new Intl.DateTimeFormat("ru-RU", {
          ...options,
          timeZone: timezone,
        });
      } catch {
        return new Intl.DateTimeFormat("ru-RU", {
          ...options,
          timeZone: "Asia/Bishkek",
        });
      }
    };
    return {
      axis: create(
        period === "day"
          ? { hour: "2-digit", minute: "2-digit" }
          : { day: "numeric", month: "short" },
      ),
      exact: create({
        day: "numeric",
        month: "long",
        hour: "2-digit",
        minute: "2-digit",
      }),
    };
  }, [timezone, period]);

  function toggle(key: Metric) {
    setSelected((old) => {
      if (old.includes(key))
        return old.length === 1 ? old : old.filter((metric) => metric !== key);
      return [old[0], key];
    });
  }

  return (
    <section className="ata-visual-chart" aria-label="График показаний">
      <div className="ata-chart-tools">
        <div className="ata-chart-legend">
          {active.map((key, index) => (
            <span key={key}>
              <i className={index ? "secondary" : ""} />
              {metrics[key].name}, {metrics[key].unit}
            </span>
          ))}
        </div>
        {onPeriodChange && (
          <div className="ata-periods" role="group" aria-label="Период графика">
            {(Object.keys(periods) as Period[]).map((item) => (
              <button
                type="button"
                key={item}
                aria-pressed={item === period}
                onClick={() => onPeriodChange(item)}
              >
                {periods[item]}
              </button>
            ))}
          </div>
        )}
      </div>
      {multiMetric && (
        <div
          className="ata-metric-picker"
          role="group"
          aria-label="Показатели графика, до двух одновременно"
        >
          {metricKeys.map((key) => (
            <button
              type="button"
              key={key}
              aria-pressed={selected.includes(key)}
              onClick={() => toggle(key)}
            >
              {metrics[key].name}
            </button>
          ))}
        </div>
      )}
      {error ? (
        <div role="alert" className="ata-visual-empty">
          {error}
        </div>
      ) : loading ? (
        <div role="status" className="ata-visual-empty ata-chart-loading">
          Загрузка показаний…
        </div>
      ) : !hasSeries ? (
        <div className="ata-visual-empty">
          <Activity aria-hidden="true" />
          <span>За этот период нет показаний</span>
        </div>
      ) : (
        <div className="ata-chart-plot">
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart
              key={period}
              data={data}
              margin={{ top: 8, right: 10, bottom: 0, left: 0 }}
              accessibilityLayer
            >
              <defs>
                <linearGradient id={paint} x1="0" y1="0" x2="0" y2="1">
                  <stop
                    offset="0%"
                    stopColor="var(--accent)"
                    stopOpacity={0.42}
                  />
                  <stop
                    offset="100%"
                    stopColor="var(--accent)"
                    stopOpacity={0.035}
                  />
                </linearGradient>
              </defs>
              <CartesianGrid
                stroke="var(--border)"
                strokeDasharray="3 5"
                vertical={false}
              />
              <XAxis
                dataKey="time"
                type="number"
                domain={["dataMin", "dataMax"]}
                tickFormatter={(n) => axis.format(n)}
                tickLine={false}
                axisLine={false}
                minTickGap={32}
                dy={5}
              />
              <YAxis
                yAxisId="primary"
                tickFormatter={(n) => number.format(n)}
                tickLine={false}
                axisLine={false}
                width={44}
              />
              {secondary && (
                <YAxis
                  yAxisId="secondary"
                  orientation="right"
                  tickFormatter={(n) => number.format(n)}
                  tickLine={false}
                  axisLine={false}
                  width={42}
                />
              )}
              <Tooltip
                cursor={{ stroke: "var(--text-muted)", strokeDasharray: "3 4" }}
                content={({ active: visible, payload, label }) => {
                  if (!visible || !payload?.length || !finite(label))
                    return null;
                  const row = payload[0].payload as ChartPoint;
                  return (
                    <div className="ata-chart-tip">
                      <time>{exact.format(label)}</time>
                      {active.map((key) => (
                        <div key={key}>
                          <span>{metrics[key].name}</span>
                          <b>
                            {finite(row[key])
                              ? number.format(row[key]) +
                                " " +
                                metrics[key].unit
                              : "Нет данных"}
                          </b>
                        </div>
                      ))}
                    </div>
                  );
                }}
              />
              <Area
                dataKey={primary}
                name={metrics[primary].name}
                type="linear"
                yAxisId="primary"
                stroke="var(--accent)"
                strokeWidth={2.3}
                fill={"url(#" + paint + ")"}
                isAnimationActive={false}
                connectNulls={false}
                dot={data.length < 12}
              />
              {secondary && (
                <Line
                  dataKey={secondary}
                  name={metrics[secondary].name}
                  type="linear"
                  yAxisId="secondary"
                  stroke="var(--ata-series-b)"
                  strokeWidth={2}
                  dot={data.length < 12}
                  connectNulls={false}
                  isAnimationActive={false}
                />
              )}
              {multiMetric && data.length > 8 && (
                <Brush
                  dataKey="time"
                  height={20}
                  stroke="var(--text-muted)"
                  fill="var(--surface-2)"
                  tickFormatter={(n) => axis.format(n)}
                  travellerWidth={10}
                  ariaLabel="Границы видимого периода"
                />
              )}
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}
    </section>
  );
}

interface MapSite {
  id: string;
  name?: string | null;
  status?: string | null;
  latitude?: number | null;
  longitude?: number | null;
}

function located(
  site: MapSite,
): site is MapSite & { latitude: number; longitude: number } {
  return (
    finite(site.latitude) &&
    Math.abs(site.latitude) <= 90 &&
    finite(site.longitude) &&
    Math.abs(site.longitude) <= 180
  );
}

function siteState(status: string | null | undefined) {
  if (status === "normal" || status === "online")
    return { label: "В сети", tone: "online" };
  if (status === "offline") return { label: "Нет связи", tone: "offline" };
  if (status === "warning")
    return { label: "Часть устройств без связи", tone: "warning" };
  if (status === "empty") return { label: "Без устройств", tone: "unknown" };
  return { label: "Нет данных", tone: "unknown" };
}

export function SiteMap({
  sites,
  compact = false,
  onOpen,
}: {
  sites: MapSite[];
  compact?: boolean;
  onOpen?: (id: string) => void;
}) {
  const element = useRef<HTMLDivElement>(null);
  const instance = useRef<L.Map | null>(null);
  const fitted = useRef(false);
  const open = useRef(onOpen);
  const stations = useMemo(() => sites.filter(located), [sites]);
  const populated = stations.length > 0;
  const [tilesFailed, setTilesFailed] = useState(false);
  useEffect(() => {
    open.current = onOpen;
  }, [onOpen]);

  useEffect(() => {
    if (!element.current || !populated) return;
    const map = L.map(element.current, {
      scrollWheelZoom: false,
      maxZoom: 19,
      zoomControl: false,
    });
    let failed = 0;
    const tiles = L.tileLayer(
      "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
      {
        maxZoom: 19,
        attribution:
          '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
      },
    );
    tiles.on("tileerror", () => {
      if (++failed >= 3) setTilesFailed(true);
    });
    tiles.on("tileload", () => {
      failed = 0;
      setTilesFailed(false);
    });
    tiles.addTo(map);
    L.control
      .zoom({
        position: "topright",
        zoomInTitle: "Приблизить",
        zoomOutTitle: "Отдалить",
      })
      .addTo(map);
    instance.current = map;
    const observer = new ResizeObserver(() => {
      map.invalidateSize();
    });
    observer.observe(element.current);
    return () => {
      observer.disconnect();
      map.remove();
      instance.current = null;
      fitted.current = false;
    };
  }, [populated]);

  useEffect(() => {
    const map = instance.current;
    if (!map || !stations.length) return;
    const bounds = L.latLngBounds([]);
    const cluster = L.markerClusterGroup({
      maxClusterRadius: 42,
      showCoverageOnHover: false,
      animate: false,
      iconCreateFunction: (group) =>
        L.divIcon({
          className: "ata-site-cluster",
          iconSize: [38, 38],
          html: "<span>" + group.getChildCount() + "</span>",
        }),
    });
    for (const site of stations) {
      const state = siteState(site.status);
      const label = (site.name || "Объект " + site.id) + " · " + state.label;
      const latlng: L.LatLngTuple = [site.latitude, site.longitude];
      const marker = L.marker(latlng, {
        title: label,
        alt: label,
        keyboard: true,
        icon: L.divIcon({
          className: "ata-map-marker " + state.tone,
          html: "<i></i>",
          iconSize: [22, 22],
        }),
      });
      const popup = document.createElement("div");
      popup.className = "ata-site-popup";
      const link = document.createElement("a");
      link.textContent = site.name || "Объект " + site.id;
      link.href = "/sites/" + encodeURIComponent(site.id);
      link.addEventListener("click", (event) => {
        if (
          !open.current ||
          event.ctrlKey ||
          event.metaKey ||
          event.shiftKey ||
          event.altKey
        )
          return;
        event.preventDefault();
        open.current(site.id);
      });
      const status = document.createElement("span");
      status.textContent = state.label;
      popup.append(link, status);
      marker.bindPopup(popup);
      cluster.addLayer(marker);
      bounds.extend(latlng);
    }
    if (!fitted.current) {
      map.fitBounds(bounds, { padding: [36, 36], maxZoom: 13, animate: false });
      fitted.current = true;
    }
    map.addLayer(cluster);
    return () => {
      cluster.remove();
    };
  }, [stations]);

  return (
    <div className={"ata-map" + (compact ? " compact" : "")}>
      {populated ? (
        <div
          ref={element}
          className="ata-map-canvas"
          role="region"
          aria-label="Карта станций"
        />
      ) : (
        <div className="ata-map-empty">
          <MapPinOff aria-hidden="true" />
          <strong>Нет объектов с координатами</strong>
        </div>
      )}
      {populated && tilesFailed && (
        <div className="ata-map-note" role="status">
          Картографическая подложка недоступна
        </div>
      )}
      {populated && sites.length > stations.length && (
        <div className="ata-map-count">
          Без координат · {number.format(sites.length - stations.length)}
        </div>
      )}
    </div>
  );
}

export function BatteryRing({ value }: { value: number | null | undefined }) {
  const known = finite(value) && value >= 0 && value <= 100;
  return (
    <div
      className="ata-battery"
      aria-label={
        "Заряд батареи: " +
        (known ? number.format(value) + " процентов" : "нет данных")
      }
    >
      <div
        style={{ "--charge": known ? value + "%" : "0%" } as CSSProperties}
        aria-hidden="true"
      >
        <b>{known ? number.format(value) + "%" : "—"}</b>
      </div>
      <span>Заряд батареи{!known && <small>Нет данных</small>}</span>
    </div>
  );
}

const flowKinds = [
  { key: "panel", name: "Панели", Icon: PanelsTopLeft },
  { key: "inverter", name: "Инвертор", Icon: Cpu },
  { key: "load", name: "Нагрузка", Icon: House },
  { key: "generator", name: "Генератор", Icon: Factory },
  { key: "battery", name: "Батарея", Icon: BatteryCharging },
  { key: "grid", name: "Сеть", Icon: UtilityPole },
] as const;
type FlowKind = (typeof flowKinds)[number]["key"];
interface FlowDevice {
  id: number;
  device_type: string;
  name?: string;
  status?: string | null;
  last_data?: { power?: unknown } | null;
}
const connections: Partial<Record<FlowKind, string>> = {
  panel: "M 17 26 H 50",
  load: "M 50 26 H 83",
  generator: "M 17 76 V 55 Q 17 51 22 51 H 46 Q 50 51 50 46 V 26",
  battery: "M 50 26 V 76",
  grid: "M 83 76 V 55 Q 83 51 78 51 H 54 Q 50 51 50 46 V 26",
};

export function PowerFlow({
  devices,
  onOpen,
}: {
  devices: FlowDevice[];
  onOpen?: (id: number) => void;
}) {
  const nodes = flowKinds.map((kind) => {
    const matches = devices.filter((device) => device.device_type === kind.key);
    const valid = matches.map((d) =>
      d.status === "offline" ? null : d.last_data?.power,
    );
    const power =
      valid.length && valid.every(finite)
        ? valid.reduce((sum, v) => sum + v, 0)
        : null;
    return { ...kind, devices: matches, power };
  });
  if (!nodes.some((node) => node.devices.length))
    return (
      <div className="ata-visual-empty">
        <Activity aria-hidden="true" />К объекту пока не подключено оборудование
      </div>
    );
  return (
    <div
      className="ata-flow"
      role="group"
      aria-label="Поток энергии на объекте"
    >
      <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
        {nodes.map(({ key, power }) => {
          if (!connections[key]) return null;
          const flowing = finite(power) && Math.abs(power) > 0;
          const reversed =
            (key === "battery" || key === "grid") && finite(power) && power < 0;
          return (
            <path
              key={key}
              className={
                flowing ? "is-flowing" + (reversed ? " reverse" : "") : ""
              }
              d={connections[key]}
              vectorEffect="non-scaling-stroke"
            />
          );
        })}
      </svg>
      {nodes.map(({ devices: members, Icon, key, name, power }) => {
        const watts = finite(power)
          ? number.format(Math.abs(power) / 1000) + " кВт"
          : "—";
        const direction = !members.length
          ? "Не подключено"
          : !finite(power)
            ? "Нет данных"
            : !power
              ? "Поток отсутствует"
              : key === "battery"
                ? power > 0
                  ? "Заряд"
                  : "Разряд"
                : key === "grid"
                  ? power > 0
                    ? "Из сети"
                    : "В сеть"
                  : "";
        return (
          <div
            key={key}
            className={
              "ata-flow-node " + key + (!members.length ? " absent" : "")
            }
          >
            <div className="ata-flow-icon">
              <Icon size={20} aria-hidden="true" />
            </div>
            {members.length === 1 && onOpen ? (
              <button type="button" onClick={() => onOpen(members[0].id)}>
                {name}
              </button>
            ) : (
              <strong>{name}</strong>
            )}
            <b>{watts}</b>
            {direction && <small>{direction}</small>}
            {members.length > 1 && onOpen && (
              <details>
                <summary>{members.length} устр.</summary>
                <div>
                  {members.map((device) => (
                    <button
                      type="button"
                      key={device.id}
                      onClick={() => onOpen(device.id)}
                    >
                      {device.name || "#" + device.id}
                    </button>
                  ))}
                </div>
              </details>
            )}
          </div>
        );
      })}
    </div>
  );
}
