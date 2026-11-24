import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  ArrowLeft,
  BatteryCharging,
  Cpu,
  Download,
  House,
  PanelsTopLeft,
  RefreshCw,
  UtilityPole,
} from "lucide-react";
import { api, useLiveDevices, usePrefs } from "./core";
import { Empty, Failure, Loading } from "./ui";
import "./inverter-dashboard.css";

type Point = {
  key: string;
  value: string;
  unit: string;
  label?: string;
  group?: Group;
  updated?: string;
};
type Flow = Record<string, unknown>;
type Snapshot = {
  device: {
    deviceId: number | string;
    deviceSn: string;
    deviceType: string;
    productId?: string;
  };
  reading: { deviceState: number; collectionTime: number; dataList: Point[] };
  flow: Flow;
  metadata?: {
    name: string;
    serial: string;
    pn: string;
    sn: string;
    address: number;
    code: number;
    logger_type: string;
    firmware: string;
    logger_firmware: string;
    utc_offset_seconds: number;
  } | null;
  active_parameters?: Point[];
  chart_fields?: { optional: string; order: number; visable: boolean }[];
  warnings?: string[];
};
type Site = {
  id: string;
  name: string;
  timezone: string;
  provider_code: string;
  modem_ids: number[];
  client_id: number | null;
  client_name: string | null;
};
type HistoryRow = { time: string; itemList: Record<string, unknown>[] };
type History = { dataList: HistoryRow[] };
type Chart = {
  setOption: (option: Record<string, unknown>, replace?: boolean) => void;
  resize: () => void;
  dispose: () => void;
};
type ECharts = { init: (element: HTMLElement) => Chart };
let chartLibrary: Promise<ECharts> | undefined;
function loadCharts() {
  chartLibrary ??= new Promise<ECharts>((resolve, reject) => {
    const scope = window as Window & { echarts?: ECharts };
    if (scope.echarts) {
      resolve(scope.echarts);
      return;
    }
    const script = document.createElement("script");
    script.src = "/vendor/echarts.min.js";
    script.onload = () =>
      scope.echarts
        ? resolve(scope.echarts)
        : reject(new Error("Библиотека графиков недоступна"));
    script.onerror = () => {
      chartLibrary = undefined;
      reject(new Error("Не удалось загрузить графики"));
    };
    document.head.append(script);
  });
  return chartLibrary;
}
const norm = (key: string) => key.toLowerCase().replace(/[^a-z0-9]/g, "");
const numeric = (value: unknown) =>
  value !== null &&
  value !== undefined &&
  value !== "" &&
  Number.isFinite(Number(value))
    ? Number(value)
    : null;
const format = (value: number | null, digits = 2) =>
  value === null
    ? "—"
    : value.toLocaleString("ru-RU", { maximumFractionDigits: digits });
const timestamp = (value: string | number) => {
  if (typeof value === "string" && /^\d{4}(-\d{2})?$/.test(value))
    return new Date(
      value + (value.length === 4 ? "-01-01" : "-01") + "T00:00:00Z",
    );
  const number = numeric(value);
  return new Date(
    number !== null ? number * (number < 1e12 ? 1000 : 1) : value,
  );
};
const dayAt = (date: Date, zone: string) =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: zone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
const defaults = [
  "DCPowerPV1",
  "DCPowerPV2",
  "DCVoltagePV1",
  "DCVoltagePV2",
  "DCCurrentPV1",
  "DCCurrentPV2",
  "SOC",
  "TotalInverterOutputPower",
  "power",
  "voltage",
  "current",
];
const groupLabels = {
  basic: "Основные сведения",
  version: "Версии",
  pv: "PV-вход",
  ac: "AC-выход",
  grid: "Сеть",
  load: "Нагрузка",
  battery: "Аккумулятор",
  bms: "BMS",
  temperature: "Температура",
  generator: "Генератор",
  other: "Другие показатели",
};
type Group = keyof typeof groupLabels;
function groupOf(key: string): Group {
  const n = norm(key);
  if (/version|^main$|^hmi$|^lcd|firmware|^protocol/.test(n)) return "version";
  if (n.startsWith("bms")) return "bms";
  if (/temperature/.test(n)) return "temperature";
  if (/^dc|^pv|solar/.test(n)) return "pv";
  if (
    /^gd|grid|energybuy|energysell|energypurchased|electricitypurchas|externalct/.test(
      n,
    )
  )
    return "grid";
  if (/^bc|^load|consumption|ups/.test(n)) return "load";
  if (/^bt|^battery|^soc$|^batt|charge|discharg/.test(n)) return "battery";
  if (/^gen|generator/.test(n)) return "generator";
  if (/^ac|^inverter|^totalinverter|production/.test(n)) return "ac";
  if (/rated|systemtime|invertertype|country|outputpowerlevel/.test(n))
    return "basic";
  return "other";
}
const labels: Record<string, string> = {
  PVPower: "Мощность PV",
  LoadPower: "Мощность нагрузки",
  GridPower: "Мощность сети",
  pv_voltage: "Напряжение PV1",
  pv_output_power: "Мощность PV",
  grid_active_power: "Мощность сети",
  gd_rating_voltage: "Напряжение сети",
  bt_battery_capacity: "Заряд батареи",
  bt_voltage_1: "Напряжение батареи",
  bt_battery_current: "Ток батареи",
  battery_active_power: "Мощность батареи",
  load_active_power: "Мощность нагрузки",
  energy_today: "Выработка за день",
  energy_total: "Общая выработка",
  load_energy_today: "Потребление за день",
  load_energy_total: "Общее потребление",
  energy_today_to_grid: "Продажа за день",
  energy_total_to_grid: "Общая продажа",
  energy_today_from_grid: "Покупка за день",
  energy_total_from_grid: "Общая покупка",
  Production: "Выработка",
  "GridFeed-in": "Отдача в сеть",
  Consumption: "Потребление",
  ElectricityPurchasing: "Покупка из сети",
  ChargingCapacity: "Заряд",
  DischargingCapacity: "Разряд",
  RatedPower: "Номинальная мощность",
  ProtocolVersion: "Протокол",
  MAIN: "MAIN",
  HMI: "HMI",
  LithiumBatteryVersionNumber: "Версия литиевой батареи",
  SOC: "Заряд батареи",
  BatteryVoltage: "Напряжение батареи",
  BatteryPower: "Мощность батареи",
  BatteryCurrent1: "Ток батареи 1",
  BatteryCurrent2: "Ток батареи 2",
  BatteryTotalCurrent: "Суммарный ток",
  DailyChargingEnergy: "Заряд за день",
  DailyDischargingEnergy: "Разряд за день",
  TotalChargeEnergy: "Общий заряд",
  TotalDischargeEnergy: "Общий разряд",
  GridRelayStatus: "Реле сети",
  GridStatus: "Состояние сети",
  UPSLoadPower: "Мощность UPS",
  TotalInverterOutputPower: "Выходная мощность",
  ACOutputFrequencyR: "Частота AC",
  DailyActiveProduction: "Выработка за день",
  TotalActiveProduction: "Общая выработка",
  BMSVoltage: "Напряжение BMS",
  BMSCurrent: "Ток BMS",
  BMSSOC: "Заряд BMS",
  BMSChargeVoltage: "Напряжение заряда BMS",
  BMSDisChargeVoltage: "Напряжение разряда BMS",
};
const sourceLabels: Record<string, string> = {
  "Load voltage": "Напряжение нагрузки",
  "PV1 voltage": "Напряжение PV1",
  "PV2 voltage": "Напряжение PV2",
  "Parallel state": "Параллельный режим",
};
function pointLabel(p: Pick<Point, "key" | "label">) {
  return labels[p.key] || sourceLabels[p.label || ""] || p.label || p.key;
}
function pointGroup(p: Point): Group {
  return p.group && p.group in groupLabels ? p.group : groupOf(p.key);
}
function offsetLabel(seconds: number) {
  return (
    "UTC" +
    (seconds < 0 ? "−" : "+") +
    String(Math.floor(Math.abs(seconds) / 3600)).padStart(2, "0") +
    ":" +
    String(Math.floor((Math.abs(seconds) % 3600) / 60)).padStart(2, "0")
  );
}
function displayPoint(point: Point | undefined) {
  if (!point || point.value === "") return { value: "—", unit: "" };
  const value = numeric(point.value);
  if (value !== null && ["w", "kw"].includes(point.unit.toLowerCase())) {
    const watts = value * (point.unit.toLowerCase() === "kw" ? 1000 : 1);
    return Math.abs(watts) >= 1000
      ? { value: format(watts / 1000, 3), unit: "кВт" }
      : { value: format(watts, 3), unit: "Вт" };
  }
  return {
    value: point.value,
    unit:
      ({ V: "В", A: "А", kWh: "кВт·ч", Hz: "Гц" } as Record<string, string>)[
        point.unit
      ] || point.unit,
  };
}
function hasContent(value: unknown) {
  if (typeof value === "number") return Number.isFinite(value);
  return (
    typeof value === "string" &&
    !["", "--", "—", "n/a", "null", "undefined", "nan"].includes(
      value.trim().toLowerCase(),
    )
  );
}
function hasPoint(point: Point | undefined): point is Point {
  return !!point && hasContent(point.value);
}
const metricAliases: Record<string, string[]> = {
  DailyActiveProduction: ["energy_today"],
  TotalActiveProduction: ["energy_total"],
  PVPower: ["pv_output_power", "TotalSolarPower", "TotalPVPower"],
  LoadPower: [
    "load_active_power",
    "UPSLoadPower",
    "TotalConsumptionPower",
    "TotalLoadPower",
  ],
  SOC: ["bt_battery_capacity", "battery_percent"],
  BatteryVoltage: ["bt_voltage_1"],
  BatteryCurrent1: ["bt_battery_current"],
  GridPower: ["grid_active_power", "TotalGridPower"],
};
const energyMetrics = [
  "DailyActiveProduction",
  "TotalActiveProduction",
  "PVPower",
  "LoadPower",
  "TotalInverterOutputPower",
  "ACOutputFrequencyR",
];
const batteryMetrics = [
  "DailyChargingEnergy",
  "DailyDischargingEnergy",
  "TotalChargeEnergy",
  "TotalDischargeEnergy",
];
function metricPoint(points: Point[], key: string) {
  const candidates = [key, ...(metricAliases[key] || [])];
  return candidates
    .map((candidate) =>
      points.find((p) => norm(p.key) === norm(candidate) && hasPoint(p)),
    )
    .find(Boolean);
}
type DetailEntry = { label: string; value: string | number | null | undefined };
function Details({ entries }: { entries: DetailEntry[] }) {
  const available = entries.filter((entry) => hasContent(entry.value));
  if (!available.length) return null;
  return (
    <dl className="inverter-fields">
      {available.map((entry, index) => (
        <div key={entry.label + ":" + index}>
          <dt>{entry.label}</dt>
          <dd>{entry.value}</dd>
        </div>
      ))}
    </dl>
  );
}
function FieldList({ points }: { points: Point[] }) {
  return (
    <Details
      entries={points.filter(hasPoint).map((point) => {
        const shown = displayPoint(point);
        return {
          label: pointLabel(point),
          value: [shown.value, shown.unit].filter(Boolean).join(" "),
        };
      })}
    />
  );
}
function exportCSV(name: string, rows: unknown[][]) {
  const text =
    "\uFEFF" +
    rows
      .map((row) =>
        row
          .map((v) => '"' + String(v ?? "").replace(/"/g, '""') + '"')
          .join(","),
      )
      .join("\r\n");
  const url = URL.createObjectURL(
    new Blob([text], { type: "text/csv;charset=utf-8" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function flowPart(flow: Flow, key: string): number | null {
  const values = (Array.isArray(flow[key]) ? flow[key] : [flow[key]]) as (
    { power?: unknown } | undefined
  )[];
  const powers = values.map((v) => numeric(v?.power));
  return powers.length && powers.every((v) => v !== null)
    ? powers.reduce<number>((a, b) => a + (b ?? 0), 0)
    : null;
}
function InverterFlow({
  data,
  onNavigate,
}: {
  data: Snapshot;
  onNavigate?: (kind: string) => void;
}) {
  const get = (key: string) => flowPart(data.flow, key);
  const pv = get("pv"),
    battery = get("battery"),
    grid = get("grid"),
    load = get("load");
  const soc = numeric(
    data.reading.dataList.find(
      (p) => norm(p.key) === "soc" || p.key === "battery_percent",
    )?.value,
  );
  const voltage = numeric(
    (data.flow.grid as { voltage?: unknown } | undefined)?.voltage,
  );
  const nodes = [
    { key: "pv", kind: "pv", name: "Панели", value: pv, icon: PanelsTopLeft },
    { key: "grid", kind: "grid", name: "Сеть", value: grid, icon: UtilityPole },
    {
      key: "battery",
      kind: "battery",
      name: "Аккумулятор",
      value: battery,
      icon: BatteryCharging,
    },
    { key: "load", kind: "load", name: "Нагрузка", value: load, icon: House },
  ];
  const paths: Record<string, string> = {
    pv: "M18 20 H29 Q33 20 33 25 V45 Q33 49 39 49 H43",
    grid: "M82 20 H71 Q67 20 67 25 V45 Q67 49 61 49 H57",
    battery: "M43 53 H39 Q33 53 33 59 V77 Q33 81 28 81 H18",
    load: "M57 53 H61 Q67 53 67 59 V77 Q67 81 72 81 H82",
  };
  return (
    <div className="inverter-flow" aria-label="Потоки энергии инвертора">
      <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
        {nodes.map((node) => {
          const active =
            data.reading.deviceState !== 3 &&
            node.value !== null &&
            node.value !== 0;
          return (
            <g key={node.key}>
              <path
                d={paths[node.key]}
                className={
                  "inverter-route" +
                  (active ? " active" : "") +
                  (node.value !== null && node.value < 0 ? " reverse" : "")
                }
              />
              {active && (
                <circle r=".8" className="inverter-particle">
                  <animateMotion
                    path={paths[node.key]}
                    dur="2.8s"
                    repeatCount="indefinite"
                    calcMode="linear"
                    keyTimes="0;1"
                    keyPoints={node.value! < 0 ? "1;0" : "0;1"}
                  />
                </circle>
              )}
            </g>
          );
        })}
        {voltage !== null && voltage < 50 && (
          <g>
            <circle cx="67" cy="31" r="2" className="inverter-fault" />
            <text x="67" y="32.2" textAnchor="middle" fontSize="4">
              !
            </text>
          </g>
        )}
      </svg>
      <div className="inverter-node inverter-center">
        <span className="inverter-node-icon">
          <Cpu size={26} />
        </span>
        <strong>Инвертор</strong>
      </div>
      {nodes.map(({ key, kind, name, value, icon: Icon }) => (
        <button
          type="button"
          key={key}
          className={"inverter-node inverter-node-" + key}
          onClick={() => {
            if (onNavigate) {
              onNavigate(kind);
              return;
            }
            document.getElementById("inverter-" + kind)?.scrollIntoView({
              behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
                ? "instant"
                : "smooth",
              block: "start",
            });
          }}
        >
          {key === "battery" && soc !== null && (
            <span className="inverter-soc">{format(soc, 0)} %</span>
          )}
          <span className="inverter-node-icon">
            <Icon size={23} />
          </span>
          <strong>{name}</strong>
          {value !== null && <b>{format(Math.abs(value) / 1000)} кВт</b>}
          {key === "grid" && voltage !== null && (
            <small className={voltage < 50 ? "danger" : "muted"}>
              {voltage < 50 ? "Сеть отключена" : "Сеть подключена"}
            </small>
          )}
          {key === "battery" && value !== null && (
            <small>
              {value > 0 ? "Заряд" : value < 0 ? "Разряд" : "Ожидание"}
            </small>
          )}
        </button>
      ))}
    </div>
  );
}
function PhaseTable({
  title,
  rows,
}: {
  title: string;
  rows: {
    name: string;
    voltage: Point | undefined;
    current: Point | undefined;
    power: Point | undefined;
  }[];
}) {
  const available = rows.filter(
    (row) =>
      hasPoint(row.voltage) || hasPoint(row.current) || hasPoint(row.power),
  );
  if (!available.length) return null;
  const columns = (
    [
      ["voltage", "Напряжение"],
      ["current", "Ток"],
      ["power", "Мощность"],
    ] as const
  ).filter(([key]) => available.some((row) => hasPoint(row[key])));
  const value = (point: Point | undefined) =>
    hasPoint(point)
      ? [displayPoint(point).value, displayPoint(point).unit]
          .filter(Boolean)
          .join(" ")
      : "—";
  return (
    <section>
      <h3>{title}</h3>
      <div className="inverter-table-wrap">
        <table>
          <thead>
            <tr>
              <th>Канал</th>
              {columns.map(([key, label]) => (
                <th key={key}>{label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {available.map((row) => (
              <tr key={row.name}>
                <th>{row.name}</th>
                {columns.map(([key]) => (
                  <td key={key}>{value(row[key])}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
function Conversion({ active }: { active: boolean }) {
  return (
    <div
      className={"inverter-conversion" + (active ? " active" : "")}
      role="img"
      aria-label="Преобразование постоянного тока PV в переменный ток AC"
    >
      <span className="inverter-wire" />
      <span className="inverter-chip">
        <Cpu size={42} />
        <strong>DC / AC</strong>
      </span>
      <span className="inverter-wire" />
    </div>
  );
}
function MetricTiles({ points, keys }: { points: Point[]; keys: string[] }) {
  const available = keys.flatMap((key) => {
    const point = metricPoint(points, key);
    return point ? [{ key, point }] : [];
  });
  if (!available.length) return null;
  return (
    <div className="inverter-tiles">
      {available.map(({ key, point }, index) => {
        const shown = displayPoint(point);
        return (
          <div className={"inverter-tile tone-" + (index % 4)} key={key}>
            <small>{labels[key] || pointLabel(point)}</small>
            <strong>
              {shown.value}
              <span>{shown.unit}</span>
            </strong>
          </div>
        );
      })}
    </div>
  );
}
function ParameterChart({
  rows,
  points,
  keys,
  zone,
  loading,
  error,
  retry,
}: {
  rows: HistoryRow[];
  points: Point[];
  keys: string[];
  zone: string;
  loading: boolean;
  error: Error | null;
  retry: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const unitSignature = JSON.stringify(
    points.map(({ key, unit }) => ({ key, unit })),
  );
  const [libraryError, setLibraryError] = useState("");
  const [chartRetry, setChartRetry] = useState(0);
  const theme = usePrefs((p) => p.theme);
  useEffect(() => {
    let cancelled = false;
    let chart: Chart | undefined;
    let observer: ResizeObserver | undefined;
    if (!host.current || !keys.length || !rows.length) return;
    void loadCharts()
      .then((library) => {
        if (cancelled || !host.current) return;
        chart = library.init(host.current);
        const css = getComputedStyle(host.current);
        const color = (key: string) => css.getPropertyValue(key).trim();
        const metadata: Pick<Point, "key" | "unit">[] =
          JSON.parse(unitSignature);
        const units = Array.from(
          new Set(
            keys.map((key) => metadata.find((p) => p.key === key)?.unit || ""),
          ),
        );
        const narrow = host.current.clientWidth < 650;
        const palette = [1, 2, 3, 4, 5, 6].map((index) =>
          color("--inverter-series-" + index),
        );
        const times = rows.map((row) => {
          const date = timestamp(row.time);
          return Number.isFinite(date.getTime())
            ? date.getTime()
            : Date.parse(row.time);
        });
        const series = keys.map((key, index) => ({
          name: labels[key] || key,
          type: "line",
          showSymbol: rows.length === 1,
          symbolSize: 7,
          connectNulls: false,
          yAxisIndex: units.indexOf(
            metadata.find((p) => p.key === key)?.unit || "",
          ),
          lineStyle: { width: 1.6 },
          itemStyle: { color: palette[index % palette.length] },
          data: rows.map((row, i) => {
            const item = row.itemList.find((p) => p.key === key);
            const value = item ? numeric(item.value) : null;
            return [times[i], value];
          }),
        }));
        chart.setOption({
          animation: !matchMedia("(prefers-reduced-motion: reduce)").matches,
          animationDuration: 350,
          textStyle: {
            fontFamily: "Inter, sans-serif",
            color: color("--text-muted"),
          },
          backgroundColor: "transparent",
          legend: {
            type: "scroll",
            textStyle: { color: color("--text-muted"), fontSize: 10 },
            top: 0,
          },
          tooltip: {
            trigger: "axis",
            confine: true,
            axisPointer: { type: "cross" },
            backgroundColor: color("--surface"),
            borderColor: color("--border"),
            textStyle: { color: color("--text") },
          },
          grid: {
            top: 65,
            bottom: 65,
            left: narrow
              ? 48
              : 48 + Math.max(0, Math.ceil(units.length / 2) - 1) * 42,
            right: narrow
              ? 40
              : 45 + Math.max(0, Math.floor(units.length / 2) - 1) * 42,
          },
          xAxis: {
            type: "time",
            axisLabel: {
              color: color("--text-muted"),
              hideOverlap: true,
              formatter: (value: number) =>
                new Date(value).toLocaleString("ru-RU", {
                  timeZone: zone,
                  month: "2-digit",
                  day: "2-digit",
                  hour: "2-digit",
                  minute: "2-digit",
                }),
            },
            axisLine: { lineStyle: { color: color("--border") } },
          },
          yAxis: units.map((unit, index) => ({
            type: "value",
            name: narrow && index > 1 ? "" : unit,
            position: index % 2 ? "right" : "left",
            offset: narrow ? 0 : Math.floor(index / 2) * 42,
            axisLabel: {
              show: !narrow || index < 2,
              color: color("--text-muted"),
            },
            axisLine: { show: !narrow || index < 2 },
            splitLine: {
              show: index === 0,
              lineStyle: { color: color("--border") },
            },
          })),
          dataZoom: [
            { type: "inside", filterMode: "none", zoomOnMouseWheel: "ctrl" },
            { type: "slider", height: 18, bottom: 8, showDataShadow: false },
          ],
          series,
        });
        observer = new ResizeObserver(() => chart?.resize());
        observer.observe(host.current);
      })
      .catch((err) => {
        if (!cancelled) setLibraryError(err.message);
      });
    return () => {
      cancelled = true;
      observer?.disconnect();
      chart?.dispose();
    };
  }, [rows, unitSignature, keys, zone, theme, chartRetry]);
  if (error) return <Failure message={error.message} retry={retry} />;
  if (libraryError)
    return (
      <Failure
        message={libraryError}
        retry={() => {
          setLibraryError("");
          setChartRetry((value) => value + 1);
          retry();
        }}
      />
    );
  return (
    <div className="inverter-plot-wrap" aria-busy={loading}>
      {loading && (
        <div className="inverter-plot-loading">
          <Loading />
        </div>
      )}
      {!loading && (!keys.length || !rows.length) ? (
        <Empty>
          {keys.length
            ? "Нет данных за выбранный период"
            : "Выберите параметры графика"}
        </Empty>
      ) : null}
      <div
        ref={host}
        className="inverter-plot"
        hidden={!rows.length || !keys.length}
        role="img"
        aria-label="История выбранных параметров инвертора"
      />
    </div>
  );
}

export function InverterDashboard({
  site,
  deviceId,
  onClose,
}: {
  site: Site;
  deviceId: string;
  onClose: () => void;
}) {
  const cloud =
    ["deye", "valueclouds"].includes(site.provider_code) &&
    site.id.startsWith("s");
  const devices = useLiveDevices();
  const path =
    (site.provider_code === "valueclouds"
      ? "/crm/valueclouds/sites/"
      : "/crm/sites/") +
    encodeURIComponent(site.id) +
    "/devices/" +
    encodeURIComponent(deviceId);
  const [tab, setTab] = useState("data");
  const [mode, setMode] = useState("D");
  const [date, setDate] = useState(dayAt(new Date(), site.timezone));
  const [draft, setDraft] = useState<string[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [energyDraft, setEnergyDraft] = useState<string[]>([]);
  const [energySelected, setEnergySelected] = useState<string[]>([]);
  const energyInitialized = useRef(false);
  const initialized = useRef(false);
  const [search, setSearch] = useState("");
  const localDevice = (devices.data || []).find(
    (d) =>
      String(d.id) === deviceId &&
      d.device_type === "inverter" &&
      d.modem !== null &&
      site.modem_ids.includes(d.modem),
  );
  const snapshot = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => api<Snapshot>(path, { signal }),
    enabled: cloud,
    refetchInterval: 180000,
    staleTime: 180000,
    refetchOnWindowFocus: false,
    retry: false,
  });
  const localSnapshot = useMemo<Snapshot | null>(() => {
    if (!localDevice) return null;
    const points = Object.entries(localDevice.last_data || {})
      .filter(([key]) => key !== "timestamp")
      .map(([key, value]) => ({
        key,
        value: value === null ? "" : String(value),
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
      }));
    const flow: Flow = {};
    const names: Record<string, string> = {
      panel: "pv",
      inverter: "inverter",
      battery: "battery",
      grid: "grid",
      load: "load",
      generator: "generator",
    };
    for (const d of devices.data || []) {
      if (d.modem !== localDevice.modem || !names[d.device_type]) continue;
      const key = names[d.device_type];
      const values = (flow[key] || []) as unknown[];
      values.push(d.last_data);
      flow[key] = values;
    }
    for (const key of ["inverter", "battery", "grid", "load", "generator"]) {
      const values = flow[key] as unknown[] | undefined;
      if (values?.length === 1) flow[key] = values[0];
    }
    return {
      device: {
        deviceId: localDevice.id,
        deviceSn: localDevice.serial_number,
        deviceType: "INVERTER",
      },
      reading: {
        deviceState:
          localDevice.status === "online"
            ? localDevice.last_data?.error
              ? 2
              : 1
            : 3,
        collectionTime: localDevice.last_seen
          ? Date.parse(localDevice.last_seen) / 1000
          : 0,
        dataList: points,
      },
      flow,
    };
  }, [localDevice, devices.data]);
  const data = cloud ? snapshot.data : localSnapshot;
  const points = useMemo(() => data?.reading.dataList || [], [data]);
  const groups = useMemo(() => {
    const result: Record<Group, Point[]> = {
      basic: [],
      version: [],
      pv: [],
      ac: [],
      grid: [],
      load: [],
      battery: [],
      bms: [],
      temperature: [],
      generator: [],
      other: [],
    };
    for (const point of points.filter(hasPoint))
      result[pointGroup(point)].push(point);
    // Both providers supply the same measured flow fields, even when raw register names differ.
    for (const section of ["pv", "grid", "load", "battery"] as const) {
      const raw = data?.flow[section];
      const measured = (
        !Array.isArray(raw) && raw && typeof raw === "object" ? raw : {}
      ) as Record<string, unknown>;
      for (const [field, unit, label] of [
        ["power", "W", "Мощность"],
        ["voltage", "V", "Напряжение"],
        ["current", "A", "Ток"],
        ["soc", "%", "Заряд батареи"],
      ]) {
        if (section === "pv" && field !== "power") continue;
        const value =
          field === "power"
            ? flowPart(data?.flow || {}, section)
            : numeric(measured[field]);
        if (
          value === null ||
          result[section].some(
            (p) =>
              p.unit.toLowerCase() === unit.toLowerCase() ||
              (unit === "W" && p.unit.toLowerCase() === "kw"),
          )
        )
          continue;
        result[section].push({
          key: "flow_" + section + "_" + field,
          label,
          value: String(value),
          unit,
          group: section,
        });
      }
    }
    return result;
  }, [points, data?.flow]);
  const find = (...keys: string[]) =>
    points.find((p) => keys.some((key) => norm(key) === norm(p.key)));
  useEffect(() => {
    if (!points.length || initialized.current) return;
    initialized.current = true;
    const available = (
      site.provider_code === "valueclouds"
        ? [
            "pv_output_power",
            "grid_active_power",
            "load_active_power",
            "battery_active_power",
          ]
        : defaults
    ).filter((key) => points.some((p) => p.key === key));
    const initial = available.length
      ? available
      : points
          .filter((p) => p.unit && numeric(p.value) !== null)
          .slice(0, 4)
          .map((p) => p.key);
    setDraft(initial);
    setSelected(initial);
  }, [points, site.provider_code]);
  const history = useQuery<History>({
    queryKey: [
      path + "/history",
      mode,
      date,
      mode === "D" ? selected.join(",") : "energy",
    ],
    enabled: !!data && tab === "data" && (mode !== "D" || selected.length > 0),
    refetchInterval: 180000,
    staleTime: 180000,
    refetchOnWindowFocus: false,
    retry: false,
    queryFn: async ({ signal }) => {
      if (cloud) {
        const reply = await api<History>(path + "/history", {
          method: "POST",
          signal,
          body: JSON.stringify({
            mode,
            date,
            measurePoints: mode === "D" ? selected : [],
          }),
        });
        return {
          dataList: (reply.dataList || []).map((row) => ({
            ...row,
            itemList: (row.itemList || []).flatMap((item) => {
              const key =
                typeof item.key === "string"
                  ? item.key
                  : typeof item.name === "string"
                    ? item.name
                    : "";
              return key ? [{ ...item, key }] : [];
            }),
          })),
        };
      }
      if (!localDevice) return { dataList: [] };
      const base =
        mode === "D" ? date : mode === "M" ? date + "-01" : date + "-01-01";
      const start = new Date(base + "T00:00:00Z");
      const end = new Date(start);
      if (mode === "D") end.setUTCDate(end.getUTCDate() + 2);
      else if (mode === "M") end.setUTCMonth(end.getUTCMonth() + 1);
      else end.setUTCFullYear(end.getUTCFullYear() + 1);
      const from = new Date(
        mode === "T"
          ? Date.parse(localDevice.created_at)
          : start.getTime() - 14 * 3600000,
      );
      const url =
        "/devices/" +
        localDevice.id +
        "/history?" +
        new URLSearchParams({
          from: from.toISOString(),
          to: new Date(
            Math.min(Date.now(), end.getTime() + 14 * 3600000),
          ).toISOString(),
          limit: "5000",
        });
      const reply = await api<{
        points: ({ timestamp: string } & Record<string, unknown>)[];
      }>(url, { signal });
      return {
        dataList: reply.points
          .filter(
            (p) =>
              mode === "T" ||
              dayAt(new Date(p.timestamp), site.timezone).startsWith(date),
          )
          .map((p) => ({
            time: p.timestamp,
            itemList: selected.map((key) => ({
              key,
              value: p[key],
              unit: points.find((v) => v.key === key)?.unit || "",
            })),
          })),
      };
    },
  });
  const historyPoints = useMemo(() => {
    const available = new Map<string, Point>();
    for (const row of history.data?.dataList || [])
      for (const item of row.itemList) {
        if (typeof item.key === "string" && typeof item.unit === "string")
          available.set(item.key, {
            key: item.key,
            unit: item.unit,
            value: String(item.value ?? ""),
          });
      }
    return Array.from(available.values());
  }, [history.data]);
  useEffect(() => {
    if (mode === "D" || energyInitialized.current || !historyPoints.length)
      return;
    energyInitialized.current = true;
    const keys = historyPoints.map((p) => p.key);
    setEnergyDraft(keys);
    setEnergySelected(keys);
  }, [mode, historyPoints]);
  const chartPoints = mode === "D" ? points : historyPoints;
  const chartKeys = mode === "D" ? selected : energySelected;
  const draftKeys = mode === "D" ? draft : energyDraft;
  const updateDraft = mode === "D" ? setDraft : setEnergyDraft;
  const alerts = useQuery({
    queryKey: [path + "/alerts"],
    enabled: !!data && tab === "alerts",
    refetchInterval: 180000,
    staleTime: 180000,
    refetchOnWindowFocus: false,
    retry: false,
    queryFn: async ({ signal }) => {
      if (cloud) {
        const reply = await api<{
          alertList: {
            alertId: string;
            alertName: string;
            description?: string;
            alertStartTime: number;
            status: number;
          }[];
        }>(path + "/alerts", { signal });
        return (reply.alertList || []).map((a) => ({
          id: a.alertId,
          text: a.alertName,
          detail: a.description,
          time: timestamp(a.alertStartTime),
          open: a.status === 1,
        }));
      }
      const reply = await api<
        { id: number; device: number; text: string; created_at: string }[]
      >("/alerts", { signal });
      return reply
        .filter((a) => a.device === localDevice?.id)
        .map((a) => ({
          id: String(a.id),
          text: a.text,
          detail: "",
          time: new Date(a.created_at),
          open: null,
        }));
    },
  });
  const update = () => {
    if (cloud) void snapshot.refetch();
    else void devices.refetch();
    if (selected.length) void history.refetch();
    if (tab === "alerts") void alerts.refetch();
  };
  const refreshBusy = snapshot.isFetching || (!cloud && devices.isFetching);
  const changeMode = (value: string) => {
    setMode(value);
    const today = dayAt(new Date(), site.timezone);
    setDate(
      value === "D"
        ? today
        : value === "M"
          ? today.slice(0, 7)
          : today.slice(0, 4),
    );
  };
  const moveDate = (step: number) => {
    const start = new Date(
      (mode === "D" ? date : mode === "M" ? date + "-01" : date + "-01-01") +
        "T12:00:00Z",
    );
    if (mode === "D") start.setUTCDate(start.getUTCDate() + step);
    else if (mode === "M") start.setUTCMonth(start.getUTCMonth() + step);
    else start.setUTCFullYear(start.getUTCFullYear() + step);
    setDate(
      start.toISOString().slice(0, mode === "D" ? 10 : mode === "M" ? 7 : 4),
    );
  };
  const maxDate = dayAt(new Date(), site.timezone).slice(
    0,
    mode === "D" ? 10 : mode === "M" ? 7 : 4,
  );
  const saveHistory = () =>
    exportCSV("inverter-history-" + date + ".csv", [
      [
        "Время",
        ...chartKeys.map(
          (key) =>
            key +
            " (" +
            (chartPoints.find((p) => p.key === key)?.unit || "") +
            ")",
        ),
      ],
      ...(history.data?.dataList || []).map((row) => [
        row.time,
        ...chartKeys.map(
          (key) => row.itemList.find((p) => p.key === key)?.value,
        ),
      ]),
    ]);
  const error = cloud ? snapshot.error : devices.error;
  const pvChannels = Array.from(
    new Set(
      points.flatMap((p) => {
        const match = p.key.match(
          /^(?:DC(?:Voltage|Current|Power)PV|PV)(\d+)/i,
        );
        return match ? [Number(match[1])] : [];
      }),
    ),
  ).sort((a, b) => a - b);
  const sourcePV = points.flatMap((p) => {
    const match = p.label?.match(/^PV(\d+)\s+(voltage|current|power)$/i);
    return match
      ? [{ point: p, index: Number(match[1]), kind: match[2].toLowerCase() }]
      : [];
  });
  const pvRows = (
    site.provider_code === "valueclouds"
      ? Array.from(new Set(sourcePV.map((p) => p.index)))
          .sort((a, b) => a - b)
          .map((i) => ({
            name: "PV" + i,
            voltage: sourcePV.find((p) => p.index === i && p.kind === "voltage")
              ?.point,
            current: sourcePV.find((p) => p.index === i && p.kind === "current")
              ?.point,
            power: sourcePV.find((p) => p.index === i && p.kind === "power")
              ?.point,
          }))
      : pvChannels.map((i) => ({
          name: "PV" + i,
          voltage: find("DCVoltagePV" + i, "PV" + i + "Voltage"),
          current: find("DCCurrentPV" + i, "PV" + i + "Current"),
          power: find("DCPowerPV" + i, "PV" + i + "Power"),
        }))
  ).filter(
    (row) =>
      hasPoint(row.voltage) || hasPoint(row.current) || hasPoint(row.power),
  );
  const acRows = ["RUA", "SVB", "TWC"]
    .map((phase, i) => ({
      name: "L" + (i + 1),
      voltage: find("ACVoltage" + phase, "InverterVoltageL" + (i + 1)),
      current: find("ACCurrent" + phase, "InverterCurrentL" + (i + 1)),
      power: find("InverterOutputPowerL" + (i + 1)),
    }))
    .filter(
      (row) =>
        hasPoint(row.voltage) || hasPoint(row.current) || hasPoint(row.power),
    );
  const visiblePoints = points
    .filter(hasPoint)
    .filter((p) =>
      (p.key + " " + pointLabel(p))
        .toLocaleLowerCase()
        .includes(search.toLocaleLowerCase()),
    );
  const soc = numeric(
    find("SOC", "battery_percent", "bt_battery_capacity")?.value,
  );
  const batteryPower = flowPart(data?.flow || {}, "battery");
  const busy = cloud ? snapshot.isPending : devices.isPending;
  const graphOptions = chartPoints
    .filter(
      (p) =>
        p.unit &&
        numeric(p.value) !== null &&
        (site.provider_code !== "valueclouds" ||
          mode !== "D" ||
          [
            "pv_output_power",
            "grid_active_power",
            "load_active_power",
            "battery_active_power",
          ].includes(p.key)),
    )
    .sort((a, b) => {
      const order = new Map(
        (data?.chart_fields || []).map((field) => [
          field.optional,
          field.order,
        ]),
      );
      return (
        (order.get(a.key) ?? Number.MAX_SAFE_INTEGER) -
        (order.get(b.key) ?? Number.MAX_SAFE_INTEGER)
      );
    });
  const infoEntries: DetailEntry[] = [
    { label: "Название устройства", value: data?.metadata?.name || site.name },
    {
      label: "Серийный номер",
      value: data?.metadata?.serial || data?.device.deviceSn,
    },
    {
      label: "Идентификатор устройства",
      value:
        data?.metadata?.serial && data.metadata.serial !== data.device.deviceSn
          ? data.device.deviceSn
          : undefined,
    },
    { label: "Модель / код устройства", value: data?.device.productId },
    {
      label: "Тип инвертора",
      value:
        find("InverterType")?.value ||
        (
          {
            "0_5412_1": "Трёхфазный гибридный HV",
            "0_5411_1": "Трёхфазный гибридный LV",
            "0_5407_1": "Однофазный гибридный",
          } as Record<string, string>
        )[data?.device.productId || ""],
    },
    { label: "Тип логгера", value: data?.metadata?.logger_type },
    { label: "Коллектор", value: data?.metadata?.pn },
    { label: "Адрес устройства", value: data?.metadata?.address },
    {
      label: "Часовой пояс",
      value: data?.metadata
        ? offsetLabel(data.metadata.utc_offset_seconds)
        : site.timezone,
    },
    { label: "Системное время", value: find("SystemTime")?.value },
    ...groups.basic
      .filter(
        (point) => !["systemtime", "invertertype"].includes(norm(point.key)),
      )
      .map((point) => ({
        label: pointLabel(point),
        value: [point.value, point.unit].filter(Boolean).join(" "),
      })),
  ];
  const versionEntries: DetailEntry[] = [
    { label: "Прошивка устройства", value: data?.metadata?.firmware },
    { label: "Прошивка логгера", value: data?.metadata?.logger_firmware },
    ...groups.version.map((point) => ({
      label: pointLabel(point),
      value: [point.value, point.unit].filter(Boolean).join(" "),
    })),
  ];
  const hasVersions = versionEntries.some((entry) => hasContent(entry.value));
  const activeParameters = (data?.active_parameters || []).filter(hasPoint);
  const summaryPoints = [...points];
  for (const [key, section] of [
    ["PVPower", "pv"],
    ["LoadPower", "load"],
    ["TotalInverterOutputPower", "inverter"],
  ]) {
    const value = flowPart(data?.flow || {}, section);
    if (!metricPoint(summaryPoints, key) && value !== null)
      summaryPoints.push({ key, value: String(value), unit: "W" });
  }
  const hasEnergy =
    pvRows.length > 0 ||
    acRows.length > 0 ||
    energyMetrics.some((key) => metricPoint(summaryPoints, key));
  const hasBattery =
    groups.battery.length > 0 ||
    groups.bms.length > 0 ||
    soc !== null ||
    batteryPower !== null;
  const hasFlow =
    data &&
    ["pv", "grid", "load", "battery", "inverter"].some(
      (key) => flowPart(data.flow, key) !== null,
    );
  const dirty = draftKeys.join(",") !== chartKeys.join(",");
  return (
    <div className="inverter-dashboard">
      <header className="inverter-heading">
        <div>
          <button className="small-link inverter-back" onClick={onClose}>
            <ArrowLeft size={15} /> К устройствам станции
          </button>
          <h1>
            Инвертор <span>{data?.device.deviceSn || deviceId}</span>
          </h1>
          <div className="inverter-meta">
            <span
              className={
                "badge " +
                (data?.reading.deviceState === 2
                  ? "warn"
                  : data?.reading.deviceState === 1
                    ? "good"
                    : "")
              }
            >
              {!data
                ? busy
                  ? "Загрузка"
                  : "Нет данных"
                : data.reading.deviceState === 2
                  ? "Есть аварии"
                  : data?.reading.deviceState === 1
                    ? "В сети"
                    : "Нет связи"}
            </span>
            <span>{site.name}</span>
            {data?.reading.collectionTime ? (
              <time>
                {timestamp(data.reading.collectionTime).toLocaleString(
                  "ru-RU",
                  { timeZone: site.timezone },
                )}{" "}
                · {site.timezone}
              </time>
            ) : null}
            {site.client_id && (
              <Link className="small-link" to={"/clients/" + site.client_id}>
                {site.client_name || "Клиент"}
              </Link>
            )}
          </div>
        </div>
        <div className="row">
          <button className="btn" onClick={update} disabled={refreshBusy}>
            <RefreshCw
              size={15}
              className={refreshBusy ? "inverter-spinning" : ""}
            />
            Обновить
          </button>
          <button
            className="btn"
            disabled={!points.length}
            onClick={() =>
              exportCSV("inverter-" + data?.device.deviceSn + ".csv", [
                ["Параметр", "Значение", "Единица"],
                ...points.map((p) => [p.key, p.value, p.unit]),
              ])
            }
          >
            <Download size={15} />
            Показатели CSV
          </button>
        </div>
      </header>
      <nav className="inverter-tabs" aria-label="Разделы инвертора">
        {[
          ["data", "Данные"],
          ["alerts", "Аварии"],
          ["architecture", "Архитектура"],
        ].map(([key, label]) => (
          <button
            key={key}
            aria-current={tab === key ? "page" : undefined}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
        <span className="muted">Обновление каждые 3 минуты</span>
      </nav>
      {error && <Failure message={error.message} retry={update} />}
      {data?.warnings?.length ? (
        <div role="status">
          {data.warnings.map((warning) => (
            <p key={warning}>{warning}</p>
          ))}
        </div>
      ) : null}
      {busy && <Loading />}
      {!busy && !data && !error && (
        <Empty>Инвертор не найден среди оборудования этой станции</Empty>
      )}
      {data && tab === "data" && (
        <>
          <div className={hasFlow ? "inverter-top-grid" : "inverter-stack"}>
            {hasFlow && (
              <section className="card inverter-section">
                <div className="inverter-card-head">
                  <h2>Потоки энергии</h2>
                  <span className="muted">Текущие измерения</span>
                </div>
                <InverterFlow data={data} />
              </section>
            )}
            <div className="inverter-stack">
              <section className="card inverter-section">
                <h2>Основные сведения</h2>
                <Details entries={infoEntries} />
              </section>
              {hasVersions && (
                <section className="card inverter-section">
                  <h2>Версии</h2>
                  <Details entries={versionEntries} />
                </section>
              )}
            </div>
          </div>
          {activeParameters.length > 0 && (
            <section className="card inverter-section">
              <div className="inverter-card-head">
                <h2>Активные параметры устройства</h2>
                {activeParameters[0].updated && (
                  <small className="muted">
                    Обновлено: {activeParameters[0].updated} ·{" "}
                    {data.metadata
                      ? offsetLabel(data.metadata.utc_offset_seconds)
                      : site.timezone}
                  </small>
                )}
              </div>
              <FieldList points={activeParameters} />
            </section>
          )}
          {hasEnergy && (
            <section className="card inverter-section" id="inverter-pv">
              <h2>Выработка и мощность</h2>
              {(pvRows.length > 0 || acRows.length > 0) && (
                <div
                  className={
                    pvRows.length > 0 && acRows.length > 0
                      ? "inverter-conversion-grid"
                      : "inverter-stack"
                  }
                >
                  {pvRows.length > 0 && (
                    <PhaseTable title="PV · постоянный ток" rows={pvRows} />
                  )}
                  {pvRows.length > 0 && acRows.length > 0 && (
                    <Conversion
                      active={
                        data.reading.deviceState !== 3 &&
                        (Math.abs(flowPart(data.flow, "pv") ?? 0) > 0 ||
                          Math.abs(flowPart(data.flow, "inverter") ?? 0) > 0)
                      }
                    />
                  )}
                  {acRows.length > 0 && (
                    <div id="inverter-ac">
                      <PhaseTable title="AC · переменный ток" rows={acRows} />
                    </div>
                  )}
                </div>
              )}
              <MetricTiles points={summaryPoints} keys={energyMetrics} />
            </section>
          )}
          {(groups.grid.length > 0 || groups.load.length > 0) && (
            <div className="inverter-two-grid">
              {(["grid", "load"] as Group[])
                .filter((group) => groups[group].length > 0)
                .map((group) => (
                  <section
                    className="card inverter-section"
                    id={"inverter-" + group}
                    key={group}
                  >
                    <h2>{groupLabels[group]}</h2>
                    <FieldList points={groups[group]} />
                  </section>
                ))}
            </div>
          )}
          {hasBattery && (
            <section className="card inverter-section" id="inverter-battery">
              <h2>Аккумулятор</h2>
              <div className="inverter-battery-grid">
                {(soc !== null || batteryPower !== null) && (
                  <div className="inverter-battery-visual">
                    {soc !== null && (
                      <>
                        <strong>{format(soc, 0)} %</strong>
                        <div className="inverter-battery-shell">
                          <span
                            style={{
                              width: Math.min(100, Math.max(0, soc)) + "%",
                            }}
                          />
                        </div>
                      </>
                    )}
                    {batteryPower !== null && (
                      <>
                        <span>
                          {batteryPower > 0
                            ? "Заряд"
                            : batteryPower < 0
                              ? "Разряд"
                              : "Ожидание"}
                        </span>
                        <b>{format(Math.abs(batteryPower) / 1000)} кВт</b>
                      </>
                    )}
                  </div>
                )}
                {groups.battery.length > 0 && (
                  <FieldList points={groups.battery} />
                )}
                {groups.bms.length > 0 && (
                  <div>
                    <h3>BMS</h3>
                    <FieldList points={groups.bms} />
                  </div>
                )}
              </div>
              <MetricTiles points={points} keys={batteryMetrics} />
            </section>
          )}
          {(["generator", "temperature", "other"] as Group[]).some(
            (group) => groups[group].length > 0,
          ) && (
            <div className="inverter-two-grid">
              {(["generator", "temperature", "other"] as Group[])
                .filter((group) => groups[group].length > 0)
                .map((group) => (
                  <section
                    className="card inverter-section"
                    id={"inverter-" + group}
                    key={group}
                  >
                    <h2>{groupLabels[group]}</h2>
                    <FieldList points={groups[group]} />
                  </section>
                ))}
            </div>
          )}
          <section className="card inverter-section">
            <div className="inverter-card-head">
              <div>
                <h2>История параметров</h2>
                <small className="muted">Время станции · {site.timezone}</small>
              </div>
              <div className="inverter-history-actions">
                <button
                  className="btn"
                  disabled={!history.data?.dataList.length}
                  onClick={saveHistory}
                >
                  <Download size={14} />
                  Экспорт CSV
                </button>
                <div className="inverter-periods">
                  {[
                    ["D", "День"],
                    ["M", "Месяц"],
                    ["Y", "Год"],
                    ["T", "Всего"],
                  ].map(([key, label]) => (
                    <button
                      key={key}
                      aria-pressed={mode === key}
                      onClick={() => changeMode(key)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <button
                  className="btn"
                  aria-label="Предыдущий период"
                  onClick={() => moveDate(-1)}
                  disabled={mode === "T" || Number(date.slice(0, 4)) <= 1970}
                >
                  ‹
                </button>
                <input
                  aria-label="Период графика инвертора"
                  type={
                    mode === "D" ? "date" : mode === "M" ? "month" : "number"
                  }
                  value={date}
                  max={maxDate}
                  min={
                    mode === "D"
                      ? "1970-01-01"
                      : mode === "M"
                        ? "1970-01"
                        : "1970"
                  }
                  onChange={(e) => {
                    const value = e.target.value;
                    if (
                      value &&
                      value <= maxDate &&
                      Number(value.slice(0, 4)) >= 1970
                    )
                      setDate(value);
                  }}
                />
                <button
                  className="btn"
                  aria-label="Следующий период"
                  onClick={() => moveDate(1)}
                  disabled={mode === "T" || date >= maxDate}
                >
                  ›
                </button>
              </div>
            </div>
            <div className="inverter-history-grid">
              <ParameterChart
                rows={history.data?.dataList || []}
                points={chartPoints}
                keys={chartKeys}
                zone={site.timezone}
                loading={history.isFetching}
                error={history.error}
                retry={() => void history.refetch()}
              />
              <aside className="inverter-parameters">
                <div className="inverter-card-head">
                  <h3>Параметры</h3>
                  <span className="muted">{draftKeys.length}</span>
                </div>
                <div className="inverter-presets">
                  {(mode === "D"
                    ? [
                        ["pv", "DC"],
                        ["ac", "AC"],
                        ["battery", "Батарея"],
                      ]
                    : [
                        ["all", "Все"],
                        ["battery", "Заряд / разряд"],
                        ["grid", "Сеть"],
                      ]
                  ).map(([group, label]) => (
                    <button
                      key={group}
                      onClick={() =>
                        updateDraft(
                          graphOptions
                            .filter(
                              (p) => group === "all" || pointGroup(p) === group,
                            )
                            .map((p) => p.key),
                        )
                      }
                    >
                      {label}
                    </button>
                  ))}
                </div>
                {(Object.keys(groupLabels) as Group[]).map((group) => {
                  const options = graphOptions.filter(
                    (p) => pointGroup(p) === group,
                  );
                  return options.length ? (
                    <details
                      key={group}
                      open={group === "pv" || group === "ac"}
                    >
                      <summary>{groupLabels[group]}</summary>
                      {options.map((point) => (
                        <label className="inverter-checkbox" key={point.key}>
                          <input
                            type="checkbox"
                            checked={draftKeys.includes(point.key)}
                            onChange={(e) =>
                              updateDraft((old) =>
                                e.target.checked
                                  ? [...old, point.key]
                                  : old.filter((k) => k !== point.key),
                              )
                            }
                          />
                          <span>
                            {pointLabel(point)}
                            <small>{point.unit}</small>
                          </span>
                        </label>
                      ))}
                    </details>
                  ) : null;
                })}
                <button
                  className="btn primary"
                  disabled={!dirty}
                  onClick={() =>
                    mode === "D"
                      ? setSelected(draft)
                      : setEnergySelected(energyDraft)
                  }
                >
                  Показать на графике
                </button>
              </aside>
            </div>
          </section>
          {points.some(hasPoint) && (
            <details className="card inverter-section inverter-all-points">
              <summary>
                Все показатели · {points.filter(hasPoint).length}
              </summary>
              <div className="inverter-card-head">
                <div className="row">
                  <input
                    aria-label="Поиск показателя"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="Найти показатель"
                  />
                  {search && (
                    <button
                      className="btn"
                      onClick={() => setSearch("")}
                      aria-label="Очистить поиск"
                    >
                      ×
                    </button>
                  )}
                </div>
              </div>
              {visiblePoints.length ? (
                <FieldList points={visiblePoints} />
              ) : (
                <Empty>Показатели не найдены</Empty>
              )}
            </details>
          )}
        </>
      )}
      {data && tab === "alerts" && (
        <section className="card inverter-section">
          <h2>Аварии инвертора за 30 дней</h2>
          {alerts.isPending ? (
            <Loading />
          ) : alerts.error ? (
            <Failure
              message={alerts.error.message}
              retry={() => void alerts.refetch()}
            />
          ) : alerts.data?.length ? (
            <div className="inverter-alerts">
              {alerts.data.map((a) => (
                <article key={a.id}>
                  <span className={"badge " + (a.open ? "warn" : "")}>
                    {a.open === null
                      ? "Событие"
                      : a.open
                        ? "Активна"
                        : "Закрыта"}
                  </span>
                  <strong>{a.text}</strong>
                  <time>
                    {a.time.toLocaleString("ru-RU", {
                      timeZone: site.timezone,
                    })}
                  </time>
                  {a.detail && <p>{a.detail}</p>}
                </article>
              ))}
            </div>
          ) : (
            <Empty>Аварий за этот период нет</Empty>
          )}
        </section>
      )}
      {data && tab === "architecture" && (
        <section className="card inverter-section">
          <h2>Архитектура подключения</h2>
          <InverterFlow
            data={data}
            onNavigate={(kind) => {
              setTab("data");
              requestAnimationFrame(() =>
                document
                  .getElementById("inverter-" + kind)
                  ?.scrollIntoView({ block: "start", behavior: "instant" }),
              );
            }}
          />
          <div className="inverter-conversion-grid">
            <div>
              <h3>DC · PV-входы</h3>
              <p className="muted">
                {pvRows.length
                  ? pvRows.map((row) => row.name).join(" · ")
                  : "Каналы не переданы источником"}
              </p>
            </div>
            <Conversion
              active={
                data.reading.deviceState !== 3 &&
                (Math.abs(flowPart(data.flow, "pv") ?? 0) > 0 ||
                  Math.abs(flowPart(data.flow, "inverter") ?? 0) > 0)
              }
            />
            <div>
              <h3>AC · фазы</h3>
              <p className="muted">
                {acRows.length
                  ? acRows.map((row) => row.name).join(" · ")
                  : "Фазы не переданы источником"}
              </p>
            </div>
          </div>
        </section>
      )}
    </div>
  );
}
