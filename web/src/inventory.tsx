import { FilterChip, LiveValue, SearchField } from "./ui";
import "./filters.css";
import { StationDashboard } from "./station-dashboard";
import { validateForm } from "./form";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
} from "@tanstack/react-table";
import type { ColumnDef, SortingState } from "@tanstack/react-table";
import * as Dialog from "@radix-ui/react-dialog";
import {
  Activity,
  ArrowLeft,
  ArrowUpDown,
  BatteryCharging,
  Bookmark,
  Check,
  Clock3,
  Cpu,
  Download,
  Eye,
  EyeOff,
  Gauge,
  Grid2X2,
  LayoutList,
  Map as MapIcon,
  MapPin,
  Plus,
  Radio,
  Search,
  PanelsTopLeft as SolarPanel,
  Thermometer,
  UserRound,
  Wrench,
  X,
  Zap,
} from "lucide-react";
import { api, useList, useSession, useLiveDevices } from "./core";
import { BatteryRing, PowerFlow, SiteMap, TelemetryChart } from "./visuals";
import type { HistorySample, Period } from "./visuals";
import "./inventory.css";

type Section = "clients" | "sites" | "devices";
type Entity = { id: string | number };
type Modem = {
  id: number;
  name: string;
  serial: string;
  status: string;
  site: string;
  last_seen: string | null;
};
type Client = {
  id: number;
  name: string;
  phone: string;
  address: string;
  user: number | null;
  username: string | null;
  created_at: string;
  devices_count: number;
  online_count: number;
  modems: Modem[] | null;
};
type Site = {
  id: string;
  name: string;
  client_id: number | null;
  client_name: string | null;
  external_id: string;
  status: string;
  provider_code: string;
  provider_name: string;
  mock: boolean;
  archived: boolean;
  latitude: number | null;
  longitude: number | null;
  modem_ids: number[];
  device_count: number;
  address: string;
  timezone: string;
  last_synced_at: string | null;
};
type Device = {
  id: number;
  client: number;
  client_name: string;
  modem: number | null;
  modem_serial: string | null;
  name: string;
  device_id: string;
  device_type: string;
  type_display: string;
  serial_number: string;
  status: string;
  last_seen: string | null;
  last_data: Record<string, number | string | null> | null;
  created_at: string;
};
type Alert = {
  id: number;
  device: number;
  client_id: number;
  kind: string;
  text: string;
  created_at: string;
  is_read: boolean;
};
type Work = {
  id: number;
  title: string;
  status: string;
  client_id: number | null;
  device_id: number | null;
  created_at: string;
  updated_at: string;
};
type Service = {
  model: string | null;
  nominal_watts: number | null;
  installed_on: string | null;
  serviced_on: string | null;
  service_due_on: string | null;
  warranty_until: string | null;
  maintenance: boolean;
  can_edit: boolean;
  condition: string;
};
type QueryState = {
  isLoading: boolean;
  error: Error | null;
  refetch: () => unknown;
};
const names = { clients: "Клиенты", sites: "Объекты", devices: "Оборудование" };
const states: Record<string, string> = {
  online: "В сети",
  normal: "В сети",
  offline: "Без связи",
  empty: "Нет устройств",
  unknown: "Нет данных",
  warning: "Частично в сети",
  problem: "Ошибка",
  maintenance: "На обслуживании",
  low: "Низкая мощность",
  open: "Открыта",
  progress: "В работе",
  closed: "Закрыта",
};
const builtInProviders: Record<string, string> = {
  local: "Локальный модем",
  deye: "Deye Cloud",
  eybond: "Eybond",
};
const units = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 });
const formatValue = (n: unknown) =>
  typeof n === "number" && Number.isFinite(n) ? units.format(n) : "—";
const siteProvider = (site: Site) =>
  (builtInProviders[site.provider_code] || site.provider_name) +
  (site.mock ? " (демо)" : "");
const clientEditor = (role: string) =>
  ["admin", "manager", "it_head"].includes(role);
const deviceName = (d: Device) => d.name || d.type_display || d.device_id;
const siteFor = (d: Device, sites: Site[]) =>
  sites.find((s) => d.modem !== null && s.modem_ids?.includes(d.modem));
const isoDay = (s?: string | null) => (s ? s.slice(0, 10) : "");
const dateText = (s?: string | null, zone = "Asia/Bishkek") =>
  s
    ? new Date(s).toLocaleDateString("ru-RU", {
        timeZone: zone,
        day: "numeric",
        month: "short",
        year: "numeric",
      })
    : "—";
const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "Не удалось выполнить запрос";

function Age({ value, zone }: { value: string | null; zone?: string }) {
  if (!value) return <span className="muted">Нет данных</span>;
  const n = Math.max(
    0,
    Math.floor((Date.now() - new Date(value).getTime()) / 1000),
  );
  let label = n + " сек";
  if (n >= 60) label = Math.floor(n / 60) + " мин";
  if (n >= 3600)
    label = Math.floor(n / 3600) + " ч " + Math.floor((n % 3600) / 60) + " мин";
  if (n >= 86400) label = Math.floor(n / 86400) + " дн";
  return (
    <time
      dateTime={value}
      title={new Date(value).toLocaleString("ru-RU", {
        timeZone: zone || "Asia/Bishkek",
      })}
    >
      {label} назад
    </time>
  );
}
function Badge({ status }: { status: string }) {
  return (
    <span className={"inv-badge inv-" + status}>
      <i />
      {states[status] || status || "Нет данных"}
    </span>
  );
}
function Person({ name }: { name: string }) {
  return (
    <span className="inv-initials" aria-hidden="true">
      {name
        .trim()
        .split(/\s+/)
        .slice(0, 2)
        .map((p) => p[0])
        .join("")
        .toUpperCase()}
    </span>
  );
}
function EntityLink({
  section,
  id,
  label,
  preview,
}: {
  section: Section;
  id: string | number;
  label: string;
  preview?: string;
}) {
  return (
    <Link
      className="inv-entity"
      to={"/" + section + "/" + id}
      title={preview || label}
    >
      {label}
    </Link>
  );
}
function Empty({ children }: { children: ReactNode }) {
  return (
    <div className="inv-empty">
      <Search size={23} strokeWidth={1.5} />
      <span>{children}</span>
    </div>
  );
}
function Wait({ query }: { query: QueryState }) {
  if (query.error)
    return (
      <div className="inv-error" role="alert">
        {query.error.message}{" "}
        <button type="button" onClick={() => query.refetch()}>
          Повторить
        </button>
      </div>
    );
  if (query.isLoading)
    return (
      <div className="inv-loading" role="status" aria-label="Загрузка">
        <i />
        <i />
        <i />
      </div>
    );
  return null;
}
function Sheet({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="inv-scrim" />
        <Dialog.Content className="inv-sheet" aria-describedby={undefined}>
          <div className="inv-sheet-title">
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Close className="inv-icon" aria-label="Закрыть">
              <X size={18} />
            </Dialog.Close>
          </div>
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
function Password({
  value,
  onChange,
  required,
}: {
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
}) {
  const [show, setShow] = useState(false);
  const id = useId();
  return (
    <div className="inv-field">
      <label htmlFor={id}>Пароль</label>
      <div className="inv-password">
        <input
          id={id}
          type={show ? "text" : "password"}
          autoComplete="new-password"
          minLength={6}
          required={required}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        <button
          type="button"
          aria-label={show ? "Скрыть пароль" : "Показать пароль"}
          onClick={() => setShow(!show)}
        >
          {show ? <EyeOff size={16} /> : <Eye size={16} />}
        </button>
      </div>
    </div>
  );
}
function Subhead({
  section,
  title,
  meta,
  action,
}: {
  section: Section;
  title: string;
  meta?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <header className="inv-page-head">
      <div>
        <Link className="inv-back" to={"/" + section}>
          <ArrowLeft size={14} />
          {names[section]}
        </Link>
        <h1>{title}</h1>
        {meta && <div className="inv-head-meta">{meta}</div>}
      </div>
      {action}
    </header>
  );
}
function Tabs({
  tabs,
  tab,
  set,
}: {
  tabs: string[];
  tab: string;
  set: (tab: string) => void;
}) {
  return (
    <nav aria-label="Разделы карточки" className="inv-tabs">
      {tabs.map((t) => (
        <button
          className={t === tab ? "selected" : ""}
          aria-current={t === tab ? "page" : undefined}
          key={t}
          onClick={() => set(t)}
        >
          {t}
        </button>
      ))}
    </nav>
  );
}
type Column<T> = {
  name: string;
  text: (row: T) => string | number;
  render?: (row: T) => ReactNode;
};
function Table<T extends Entity>({
  rows,
  columns,
}: {
  rows: T[];
  columns: Column<T>[];
}) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const defs = useMemo<ColumnDef<T>[]>(
    () =>
      columns.map((c) => ({
        id: c.name,
        header: c.name,
        accessorFn: c.text,
        cell: (info) =>
          c.render
            ? c.render(info.row.original)
            : (info.getValue() as ReactNode),
      })),
    [columns],
  );
  const table = useReactTable({
    data: rows,
    columns: defs,
    state: { sorting },
    onSortingChange: setSorting,
    getRowId: (r) => String(r.id),
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });
  if (!rows.length) return <Empty>Записей по выбранным условиям нет</Empty>;
  return (
    <div className="inv-scroll">
      <table className="inv-table">
        <thead>
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((h) => (
                <th
                  key={h.id}
                  aria-sort={
                    h.column.getIsSorted() === "asc"
                      ? "ascending"
                      : h.column.getIsSorted() === "desc"
                        ? "descending"
                        : undefined
                  }
                >
                  <button onClick={h.column.getToggleSortingHandler()}>
                    {flexRender(h.column.columnDef.header, h.getContext())}
                    <ArrowUpDown size={11} />
                  </button>
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id}>
              {row.getVisibleCells().map((cell) => (
                <td
                  key={cell.id}
                  data-label={String(cell.column.columnDef.header)}
                >
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
function csv<T>(rows: T[], columns: Column<T>[], file: string) {
  const escape = (value: string | number) => {
    let text = String(value);
    if (/^[\s]*[=+@-]/.test(text)) text = "'" + text;
    return '"' + text.replace(/"/g, '""') + '"';
  };
  const lines = [
    columns.map((c) => c.name),
    ...rows.map((row) => columns.map((c) => c.text(row))),
  ];
  const url = URL.createObjectURL(
    new Blob(
      ["\ufeff", lines.map((line) => line.map(escape).join(";")).join("\r\n")],
      { type: "text/csv;charset=utf-8" },
    ),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = file + ".csv";
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
type Filter = { key: string; title: string; options: [string, string][] };
function Filters({
  filters,
  query,
  change,
  saveKey,
  exportCSV,
}: {
  filters: Filter[];
  query: URLSearchParams;
  change: (q: URLSearchParams) => void;
  saveKey: string;
  exportCSV: () => void;
}) {
  const [saved, setSaved] = useState(() => {
    try {
      return localStorage.getItem(saveKey);
    } catch {
      return null;
    }
  });
  const [notice, setNotice] = useState("");
  function set(key: string, value: string) {
    const next = new URLSearchParams(query);
    if (value) next.set(key, value);
    else next.delete(key);
    change(next);
    setNotice("");
  }
  function save() {
    try {
      localStorage.setItem(saveKey, query.toString());
      setSaved(query.toString());
      setNotice("Фильтр сохранён");
    } catch {
      setNotice("Браузер не разрешил сохранить фильтр");
    }
  }
  const active = filters.filter((f) => query.get(f.key)).length;
  const hasQuery = !!query.get("q");
  return (
    <div className="filterbar inv-filters">
      <div className="filterbar-row">
        <SearchField
          label="Искать в списке"
          placeholder="Поиск в списке"
          value={query.get("q") || ""}
          onChange={(v) => set("q", v)}
        />
        <div className="filter-actions">
          <button
            type="button"
            className="filter-tool"
            aria-label="Сохранить текущие фильтры"
            title="Сохранить текущие фильтры"
            onClick={save}
          >
            <Bookmark size={16} />
          </button>
          <button
            type="button"
            className="filter-tool"
            aria-label="Скачать CSV"
            title="Скачать CSV"
            onClick={exportCSV}
          >
            <Download size={16} />
            <span className="tool-label">CSV</span>
          </button>
        </div>
      </div>
      <div className="chips">
        {(active > 0 || hasQuery) && (
          <button
            type="button"
            className="filter-reset"
            onClick={() => {
              const next = new URLSearchParams(query);
              next.delete("q");
              for (const f of filters) next.delete(f.key);
              change(next);
              setNotice("");
            }}
          >
            <X size={14} />
            Сбросить{active ? " (" + active + ")" : ""}
          </button>
        )}
        {filters.map((f) => (
          <FilterChip
            key={f.key}
            title={f.title
              .replace(/^Все /, "")
              .replace(/^./, (c) => c.toUpperCase())}
            allLabel={/^Все /.test(f.title) ? f.title : "Любое"}
            value={query.get(f.key) || ""}
            options={f.options.map(([value, label]) => ({ value, label }))}
            onChange={(v) => set(f.key, v)}
          />
        ))}
        {saved !== null && (
          <button
            type="button"
            className="chip"
            onClick={() => {
              change(new URLSearchParams(saved));
              setNotice("Фильтр применён");
            }}
          >
            <Bookmark size={14} />
            Мой фильтр
          </button>
        )}
      </div>
      {notice && (
        <small className="filter-note" role="status">
          {notice}
        </small>
      )}
    </div>
  );
}
function RequestLink({ client, device }: { client?: number; device?: number }) {
  return (
    <Link
      className="btn primary"
      to={
        "/requests?new=1" +
        (client ? "&client=" + client : "") +
        (device ? "&device=" + device : "")
      }
    >
      <Plus size={16} />
      Создать заявку
    </Link>
  );
}
function Notes({
  items,
}: {
  items: { id: string; title: string; meta: string; to?: string }[];
}) {
  return items.length ? (
    <ul className="inv-notes">
      {items.map((item) => (
        <li key={item.id}>
          <div>
            {item.to ? <Link to={item.to}>{item.title}</Link> : item.title}
          </div>
          <small>{item.meta}</small>
        </li>
      ))}
    </ul>
  ) : (
    <Empty>Пока нет записей</Empty>
  );
}
function ApiError({ err }: { err: string }) {
  return err ? (
    <p className="inv-error" role="alert">
      {err}
    </p>
  ) : null;
}

function useSave(done: () => void, paths: string[]) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const lock = useRef(false);
  const cache = useQueryClient();
  async function save(work: () => Promise<unknown>) {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      await work();
      await Promise.all(
        paths.map((path) => cache.invalidateQueries({ queryKey: [path] })),
      );
      done();
    } catch (e) {
      setError(errorText(e));
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  return { busy, error, save };
}
function matches(needle: string | null, values: (string | null | undefined)[]) {
  const clean = needle?.trim().toLocaleLowerCase("ru") || "";
  return (
    !clean || values.some((s) => s?.toLocaleLowerCase("ru").includes(clean))
  );
}
function sourceOptions(sites: Site[]): [string, string][] {
  return Array.from(
    new Map(sites.map((s) => [s.provider_code, siteProvider(s)])),
  );
}
function ListHead({
  title,
  count,
  action,
}: {
  title: string;
  count?: number;
  action?: ReactNode;
}) {
  return (
    <header className="inv-page-head">
      <div>
        <h1>{title}</h1>
        {count !== undefined && (
          <small className="muted">Записей в каталоге: {count}</small>
        )}
      </div>
      {action}
    </header>
  );
}
function siteColumns(): Column<Site>[] {
  return [
    {
      name: "Объект",
      text: (s) => s.name,
      render: (s) => (
        <div className="inv-cell">
          <span className="inv-small-icon">
            <SolarPanel size={18} />
          </span>
          <div>
            <EntityLink
              section="sites"
              id={s.id}
              label={s.name}
              preview={s.address}
            />
            <small>{s.address || s.external_id}</small>
          </div>
        </div>
      ),
    },
    {
      name: "Статус",
      text: (s) => states[s.status] || s.status,
      render: (s) => <Badge status={s.status} />,
    },
    {
      name: "Клиент",
      text: (s) => s.client_name || "Ждёт назначения",
      render: (s) =>
        s.client_id ? (
          <EntityLink
            section="clients"
            id={s.client_id}
            label={s.client_name || "Клиент"}
          />
        ) : (
          <span className="muted">Ждёт назначения</span>
        ),
    },
    {
      name: "Источник",
      text: siteProvider,
      render: (s) => <span className="inv-tag">{siteProvider(s)}</span>,
    },
    { name: "Устройства", text: (s) => s.device_count },
  ];
}
function deviceColumns(sites: Site[]): Column<Device>[] {
  return [
    {
      name: "Устройство",
      text: deviceName,
      render: (d) => (
        <div className="inv-cell">
          <span className="inv-small-icon">
            <Cpu size={17} />
          </span>
          <div>
            <EntityLink
              section="devices"
              id={d.id}
              label={deviceName(d)}
              preview={d.serial_number || d.device_id}
            />
            <small className="inv-mono">{d.serial_number || d.device_id}</small>
          </div>
        </div>
      ),
    },
    { name: "Тип", text: (d) => d.type_display },
    {
      name: "Связь",
      text: (d) => states[d.status] || d.status,
      render: (d) => <Badge status={d.status} />,
    },
    {
      name: "Клиент",
      text: (d) => d.client_name,
      render: (d) => (
        <EntityLink section="clients" id={d.client} label={d.client_name} />
      ),
    },
    {
      name: "Объект",
      text: (d) => siteFor(d, sites)?.name || "—",
      render: (d) => {
        const s = siteFor(d, sites);
        return s ? (
          <EntityLink section="sites" id={s.id} label={s.name} />
        ) : (
          "—"
        );
      },
    },
    {
      name: "Последняя связь",
      text: (d) => d.last_seen || "",
      render: (d) => <Age value={d.last_seen} />,
    },
  ];
}
function ClientList() {
  const { me } = useSession();
  const [params, setParams] = useSearchParams();
  const [creating, setCreating] = useState(false);
  const clients = useList<Client[]>("/clients");
  const sites = useList<Site[]>("/crm/sites");
  const alerts = useList<Alert[]>("/crm/active-alerts", true, 30000);
  const allSites = sites.data || [];
  const alerted = new Set((alerts.data || []).map((a) => a.client_id));
  const ownSites = (client: Client) =>
    allSites.filter((s) => s.client_id === client.id);
  const rows = (clients.data || []).filter(
    (c) =>
      matches(params.get("q"), [c.name, c.phone, c.username, c.address]) &&
      (!params.get("provider") ||
        ownSites(c).some((s) => s.provider_code === params.get("provider"))) &&
      (params.get("state") !== "alert" || alerted.has(c.id)) &&
      (params.get("state") !== "offline" || c.online_count < c.devices_count),
  );
  const columns: Column<Client>[] = [
    {
      name: "Клиент",
      text: (c) => c.name,
      render: (c) => (
        <div className="inv-cell">
          <Person name={c.name} />
          <div>
            <EntityLink
              section="clients"
              id={c.id}
              label={c.name}
              preview={c.address}
            />
            <small>{c.address || "Адрес не указан"}</small>
          </div>
        </div>
      ),
    },
    {
      name: "Телефон",
      text: (c) => c.phone || "—",
      render: (c) => (c.phone ? <a href={"tel:" + c.phone}>{c.phone}</a> : "—"),
    },
    {
      name: "Устройства в сети",
      text: (c) => c.online_count + " / " + c.devices_count,
      render: (c) => (
        <span className="inv-mono">
          {c.online_count} / {c.devices_count}
        </span>
      ),
    },
    {
      name: "Источники",
      text: (c) =>
        [...new Set(ownSites(c).map(siteProvider))].join(", ") || "—",
    },
    {
      name: "Аварии",
      text: (c) => (alerted.has(c.id) ? "Есть активные аварии" : "—"),
      render: (c) =>
        alerted.has(c.id) ? (
          <span className="inv-badge inv-problem">
            <i />
            Есть аварии
          </span>
        ) : (
          "—"
        ),
    },
  ];
  return (
    <div className="inv-page">
      <ListHead
        title="Клиенты"
        count={clients.data?.length}
        action={
          clientEditor(me.role) && (
            <button className="btn primary" onClick={() => setCreating(true)}>
              <Plus size={16} />
              Добавить клиента
            </button>
          )
        }
      />
      <section className="card inv-card">
        <Filters
          filters={[
            {
              key: "state",
              title: "Состояние",
              options: [
                ["alert", "Есть аварии"],
                ["offline", "Есть без связи"],
              ],
            },
            {
              key: "provider",
              title: "Все источники",
              options: sourceOptions(allSites),
            },
          ]}
          query={params}
          change={setParams}
          saveKey={"ata-clients-filter-" + me.id}
          exportCSV={() => csv(rows, columns, "Клиенты")}
        />
        <Wait query={clients} />
        <Wait query={sites} />
        <Wait query={alerts} />
        {clients.data && <Table rows={rows} columns={columns} />}
      </section>
      {creating && <ClientWizard onClose={() => setCreating(false)} />}
    </div>
  );
}
function SiteList() {
  const { me } = useSession();
  const [params, setParams] = useSearchParams();
  const [assign, setAssign] = useState<Site | null>(null);
  const sites = useList<Site[]>("/crm/sites");
  const navigate = useNavigate();
  const view = params.get("view") || "table";
  const rows = (sites.data || []).filter(
    (s) =>
      matches(params.get("q"), [
        s.name,
        s.address,
        s.client_name,
        s.external_id,
      ]) &&
      (!params.get("provider") || s.provider_code === params.get("provider")) &&
      (!params.get("status") ||
        s.status ===
          (params.get("status") === "online"
            ? "normal"
            : params.get("status"))),
  );
  const waiting = rows.filter(canAssignSite);
  const columns = siteColumns();
  return (
    <div className="inv-page">
      <ListHead
        title="Объекты"
        count={sites.data?.length}
        action={
          <div className="inv-switch">
            {[
              ["table", "Таблица", LayoutList],
              ["tiles", "Плитки", Grid2X2],
              ["map", "Карта", MapIcon],
            ].map((entry) => {
              const [key, label, icon] = entry;
              const Icon = icon as typeof MapIcon;
              return (
                <button
                  key={String(key)}
                  aria-pressed={view === key}
                  onClick={() => {
                    const next = new URLSearchParams(params);
                    next.set("view", String(key));
                    setParams(next);
                  }}
                >
                  <Icon size={15} />
                  {String(label)}
                </button>
              );
            })}
          </div>
        }
      />
      {waiting.length > 0 && (
        <details className="inv-queue">
          <summary>
            <Clock3 size={16} />
            Ждут назначения <b>{waiting.length}</b>
          </summary>
          <ul>
            {waiting.map((s) => (
              <li key={s.id}>
                <EntityLink section="sites" id={s.id} label={s.name} />
                <span>{siteProvider(s)}</span>
                {clientEditor(me.role) && (
                  <button className="inv-mini" onClick={() => setAssign(s)}>
                    Привязать к клиенту
                  </button>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
      <section className="card inv-card">
        <Filters
          query={params}
          change={setParams}
          filters={[
            {
              key: "status",
              title: "Все статусы",
              options: [
                ["online", "В сети"],
                ["warning", "Частично в сети"],
                ["offline", "Без связи"],
                ["empty", "Нет устройств"],
                ["unknown", "Нет данных"],
              ],
            },
            {
              key: "provider",
              title: "Все источники",
              options: sourceOptions(sites.data || []),
            },
          ]}
          saveKey={"ata-sites-filter-" + me.id}
          exportCSV={() => csv(rows, columns, "Объекты")}
        />
        <Wait query={sites} />
        {sites.data &&
          (view === "map" ? (
            <SiteMap sites={rows} onOpen={(id) => navigate("/sites/" + id)} />
          ) : view === "tiles" && rows.length ? (
            <div className="inv-tile-grid">
              {rows.map((s) => (
                <article className="inv-tile" key={s.id}>
                  <div className="inv-tile-top">
                    <SolarPanel size={25} />
                    <Badge status={s.status} />
                  </div>
                  <EntityLink section="sites" id={s.id} label={s.name} />
                  <p>{s.address || "Адрес не указан"}</p>
                  <span className="inv-tag">{siteProvider(s)}</span>
                  <small>Устройства: {s.device_count}</small>
                </article>
              ))}
            </div>
          ) : (
            <Table rows={rows} columns={columns} />
          ))}
      </section>
      {assign && <Assignment site={assign} onClose={() => setAssign(null)} />}
    </div>
  );
}
function DeviceList() {
  const { me } = useSession();
  const [params, setParams] = useSearchParams();
  const devices = useLiveDevices();
  const sites = useList<Site[]>("/crm/sites");
  const all = (devices.data || []) as Device[];
  const cols = deviceColumns(sites.data || []);
  const rows = all.filter(
    (d) =>
      matches(params.get("q"), [
        deviceName(d),
        d.client_name,
        d.serial_number,
        d.device_id,
        d.modem_serial,
      ]) &&
      (!params.get("type") || params.get("type") === d.device_type) &&
      (!params.get("status") || params.get("status") === d.status) &&
      (!params.get("provider") ||
        siteFor(d, sites.data || [])?.provider_code === params.get("provider")),
  );
  if (params.get("status") === "offline")
    rows.sort((a, b) => (a.last_seen || "").localeCompare(b.last_seen || ""));
  return (
    <div className="inv-page">
      <ListHead title="Оборудование" count={devices.data?.length} />
      <section className="card inv-card">
        <Filters
          query={params}
          change={setParams}
          filters={[
            {
              key: "type",
              title: "Все типы",
              options: Array.from(
                new Map(all.map((d) => [d.device_type, d.type_display])),
              ),
            },
            {
              key: "status",
              title: "Любая связь",
              options: [
                ["online", "В сети"],
                ["offline", "Без связи"],
              ],
            },
            {
              key: "provider",
              title: "Все источники",
              options: sourceOptions(sites.data || []),
            },
          ]}
          saveKey={"ata-devices-filter-" + me.id}
          exportCSV={() => csv(rows, cols, "Оборудование")}
        />
        <Wait query={devices} />
        <Wait query={sites} />
        {devices.data && <Table rows={rows} columns={cols} />}
      </section>
    </div>
  );
}
const canAssignSite = (s: Site) =>
  !s.client_id && !s.archived && s.id.startsWith("s") && !s.modem_ids?.length;
function Assignment({ site, onClose }: { site: Site; onClose: () => void }) {
  const clients = useList<Client[]>("/clients");
  const [id, setId] = useState("");
  const action = useSave(onClose, [
    "/crm/sites",
    "/crm/sites/" + site.id,
    "/crm/providers",
  ]);
  function submit(e: FormEvent) {
    e.preventDefault();
    void action.save(() =>
      api("/crm/sites/" + site.id + "/client", {
        method: "PUT",
        body: JSON.stringify({ client_id: Number(id) }),
      }),
    );
  }
  return (
    <Sheet title="Привязать к клиенту" onClose={onClose}>
      <p>{site.name}</p>
      <Wait query={clients} />
      <form
        noValidate
        onSubmitCapture={validateForm}
        className="inv-form"
        onSubmit={submit}
      >
        <label>
          Клиент
          <select required value={id} onChange={(e) => setId(e.target.value)}>
            <option value="">Выберите клиента</option>
            {(clients.data || []).map((c) => (
              <option value={c.id} key={c.id}>
                {c.name} · #{c.id}
              </option>
            ))}
          </select>
        </label>
        <ApiError err={action.error} />
        <button className="btn primary" disabled={action.busy || !id}>
          Привязать
        </button>
      </form>
    </Sheet>
  );
}

function ClientWizard({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [data, setData] = useState({
    name: "",
    phone: "",
    address: "",
    login_username: "",
    login_password: "",
    modem_serial: "",
    modem_name: "",
    has_generator: false,
  });
  const [newID, setNewID] = useState<number>();
  const [login, setLogin] = useState(false);
  const [modem, setModem] = useState(false);
  const action = useSave(() => {}, [
    "/clients",
    "/devices",
    "/modems",
    "/crm/sites",
  ]);
  const field = (key: keyof typeof data, value: string | boolean) =>
    setData((old) => ({ ...old, [key]: value }));
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (step < 2) {
      setStep(step + 1);
      return;
    }
    await action.save(async () => {
      const created = await api<Client>("/clients", {
        method: "POST",
        body: JSON.stringify({
          name: data.name.trim(),
          phone: data.phone.trim(),
          address: data.address.trim(),
          ...(login && {
            login_username: data.login_username.trim(),
            login_password: data.login_password,
          }),
          ...(modem && {
            modem_serial: data.modem_serial.trim(),
            modem_name: data.modem_name.trim(),
            has_generator: data.has_generator,
          }),
        }),
      });
      setNewID(created.id);
    });
  }
  useEffect(() => {
    if (newID && !action.busy) {
      onClose();
      navigate("/clients/" + newID);
    }
  }, [newID, action.busy, navigate, onClose]);
  const steps = ["Данные", "Логин", "Модем"];
  return (
    <Sheet title="Новый клиент" onClose={onClose}>
      <ol className="inv-steps">
        {steps.map((s, i) => (
          <li
            className={i <= step ? "active" : ""}
            aria-current={i === step ? "step" : undefined}
            key={s}
          >
            <b>{i < step ? <Check size={13} /> : i + 1}</b>
            {s}
          </li>
        ))}
      </ol>
      <form
        noValidate
        onSubmitCapture={validateForm}
        className="inv-form"
        onSubmit={submit}
      >
        <h3>
          {["Данные клиента", "Доступ в кабинет", "Регистрация модема"][step]}
        </h3>
        {step === 0 && (
          <>
            <label>
              Имя или название
              <input
                required
                autoFocus
                maxLength={255}
                value={data.name}
                onChange={(e) => field("name", e.target.value)}
              />
            </label>
            <label>
              Телефон
              <input
                type="tel"
                value={data.phone}
                onChange={(e) => field("phone", e.target.value)}
              />
            </label>
            <label>
              Адрес
              <input
                value={data.address}
                onChange={(e) => field("address", e.target.value)}
              />
            </label>
          </>
        )}
        {step === 1 && (
          <>
            <label className="inv-check">
              <input
                type="checkbox"
                checked={login}
                onChange={(e) => setLogin(e.target.checked)}
              />
              Выдать логин сейчас
            </label>
            {login && (
              <>
                <label>
                  Логин
                  <input
                    required
                    autoComplete="off"
                    value={data.login_username}
                    onChange={(e) => field("login_username", e.target.value)}
                  />
                </label>
                <Password
                  value={data.login_password}
                  required
                  onChange={(value) => field("login_password", value)}
                />
              </>
            )}
          </>
        )}
        {step === 2 && (
          <>
            <label className="inv-check">
              <input
                type="checkbox"
                checked={modem}
                onChange={(e) => setModem(e.target.checked)}
              />
              Зарегистрировать модем
            </label>
            {modem && (
              <>
                <label>
                  Серийный номер
                  <input
                    required
                    value={data.modem_serial}
                    onChange={(e) => field("modem_serial", e.target.value)}
                  />
                </label>
                <label>
                  Название модема
                  <input
                    value={data.modem_name}
                    onChange={(e) => field("modem_name", e.target.value)}
                  />
                </label>
                <label className="inv-check">
                  <input
                    type="checkbox"
                    checked={data.has_generator}
                    onChange={(e) => field("has_generator", e.target.checked)}
                  />
                  Есть генератор
                </label>
              </>
            )}
          </>
        )}
        <ApiError err={action.error} />
        <div className="inv-actions">
          {step > 0 && (
            <button
              type="button"
              className="btn"
              disabled={action.busy}
              onClick={() => setStep(step - 1)}
            >
              Назад
            </button>
          )}
          <button className="btn primary" disabled={action.busy}>
            {action.busy
              ? "Сохранение…"
              : step === 2
                ? "Создать клиента"
                : "Далее"}
          </button>
        </div>
      </form>
    </Sheet>
  );
}
function LoginEditor({
  client,
  onClose,
}: {
  client: Client;
  onClose: () => void;
}) {
  const [username, setUsername] = useState(client.username || "");
  const [password, setPassword] = useState("");
  const action = useSave(onClose, ["/clients", "/clients/" + client.id]);
  function submit(e: FormEvent) {
    e.preventDefault();
    void action.save(() =>
      api("/clients/" + client.id, {
        method: "PATCH",
        body: JSON.stringify({
          login_username: username.trim(),
          login_password: password,
        }),
      }),
    );
  }
  return (
    <Sheet
      title={client.user ? "Сбросить пароль" : "Выдать логин"}
      onClose={onClose}
    >
      <form
        noValidate
        onSubmitCapture={validateForm}
        className="inv-form"
        onSubmit={submit}
      >
        <label>
          Логин
          <input
            required
            readOnly={!!client.user}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
        <Password required value={password} onChange={setPassword} />
        <small>Минимум 6 символов</small>
        <ApiError err={action.error} />
        <button className="btn primary" disabled={action.busy}>
          {action.busy ? "Сохранение…" : "Сохранить доступ"}
        </button>
      </form>
    </Sheet>
  );
}
const workNotes = (work: Work[]) =>
  work.map((w) => ({
    id: "r" + w.id,
    title: w.title,
    meta: (states[w.status] || w.status) + " · " + dateText(w.updated_at),
    to: "/requests?id=" + w.id,
  }));
const alertNotes = (alerts: Alert[]) =>
  alerts.map((a) => ({
    id: "a" + a.id,
    title: a.text,
    meta:
      (a.is_read ? "Прочитано" : "Новое оповещение") +
      " · " +
      dateText(a.created_at),
    to: "/devices/" + a.device,
  }));
function ClientDetails({ id }: { id: string }) {
  const { me, access } = useSession();
  const [tab, setTab] = useState("Объекты");
  const [editLogin, setEditLogin] = useState(false);
  const client = useList<Client>("/clients/" + id);
  const sites = useList<Site[]>("/crm/sites");
  const devices = useLiveDevices();
  const alerts = useList<Alert[]>("/alerts?client=" + id);
  const work = useList<Work[]>("/crm/requests", !!access.requests?.view);
  const ownSites = (sites.data || []).filter((s) => s.client_id === Number(id));
  const ownDevices = ((devices.data || []) as Device[]).filter(
    (d) => d.client === Number(id),
  );
  const ownWork = (work.data || []).filter((w) => w.client_id === Number(id));
  const c = client.data;
  if (!c) return <Wait query={client} />;
  const history = [
    {
      id: "new",
      title: "Клиент добавлен",
      meta: dateText(c.created_at),
      ts: c.created_at,
    },
    ...ownWork.map((w) => ({
      id: "w" + w.id,
      title: "Создана заявка «" + w.title + "»",
      meta: dateText(w.created_at),
      ts: w.created_at,
      to: "/requests?id=" + w.id,
    })),
    ...(alerts.data || []).map((a) => ({
      id: "a" + a.id,
      title: a.text,
      meta: dateText(a.created_at),
      ts: a.created_at,
      to: "/devices/" + a.device,
    })),
  ].sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts));
  const sections = [
    "Объекты",
    "Оборудование",
    "Аварии",
    ...(access.requests?.view ? ["Заявки"] : []),
    "История",
  ];
  return (
    <div className="inv-page">
      <Subhead
        section="clients"
        title={c.name}
        meta={
          <>
            <span>Станций: {ownSites.length}</span>
            <span>
              В сети: {ownSites.filter((s) => s.status === "normal").length}
            </span>
            <span>
              С проблемами:{" "}
              {
                ownSites.filter((s) =>
                  ["warning", "offline", "problem"].includes(s.status),
                ).length
              }
            </span>
          </>
        }
        action={access.requests?.create && <RequestLink client={c.id} />}
      />
      <div className="inv-columns">
        <aside className="inv-side card">
          <Person name={c.name} />
          <h2>Контакты</h2>
          <dl className="inv-dl">
            <dt>Телефон</dt>
            <dd>
              {c.phone ? <a href={"tel:" + c.phone}>{c.phone}</a> : "Не указан"}
            </dd>
            <dt>Адрес</dt>
            <dd>{c.address || "Не указан"}</dd>
            <dt>Логин</dt>
            <dd>{c.username || "Не выдан"}</dd>
          </dl>
          {clientEditor(me.role) && (
            <button className="inv-mini" onClick={() => setEditLogin(true)}>
              {c.user ? "Сбросить пароль" : "Выдать логин"}
            </button>
          )}
          <h3>Модемы</h3>
          {(c.modems || []).length ? (
            (c.modems || []).map((m) => {
              const site = ownSites.find((s) => s.modem_ids?.includes(m.id));
              return (
                <div key={m.id} className="inv-modem">
                  <Radio size={16} />
                  <div>
                    {site ? (
                      <EntityLink
                        section="sites"
                        id={site.id}
                        label={m.name || m.serial}
                      />
                    ) : (
                      <strong>{m.name || m.serial}</strong>
                    )}
                    <small className="inv-mono">{m.serial}</small>
                    <Badge status={m.status} />
                  </div>
                </div>
              );
            })
          ) : (
            <small>Пока нет модемов</small>
          )}
        </aside>
        <section className="card inv-card">
          <Tabs tabs={sections} tab={tab} set={setTab} />
          {tab === "Объекты" && (
            <>
              <Wait query={sites} />
              {sites.data && <Table columns={siteColumns()} rows={ownSites} />}
            </>
          )}
          {tab === "Оборудование" && (
            <>
              <Wait query={devices} />
              {devices.data && (
                <Table
                  columns={deviceColumns(ownSites).filter(
                    (col) => col.name !== "Клиент",
                  )}
                  rows={ownDevices}
                />
              )}
            </>
          )}
          {tab === "Аварии" && (
            <>
              <Wait query={alerts} />
              {!alerts.isLoading && !alerts.error && (
                <Notes items={alertNotes(alerts.data || [])} />
              )}
            </>
          )}
          {tab === "Заявки" && (
            <>
              <Wait query={work} />
              {!work.isLoading && !work.error && (
                <Notes items={workNotes(ownWork)} />
              )}
            </>
          )}
          {tab === "История" && <Notes items={history} />}
        </section>
      </div>
      {editLogin && (
        <LoginEditor client={c} onClose={() => setEditLogin(false)} />
      )}
    </div>
  );
}
function SiteDetails({ id }: { id: string }) {
  const { me } = useSession();
  const site = useList<Site>("/crm/sites/" + encodeURIComponent(id));
  const liveSites = useList<Site[]>("/crm/sites");
  const devices = useLiveDevices();
  const navigate = useNavigate();
  const [assign, setAssign] = useState(false);
  if (!site.data) return <Wait query={site} />;
  const s = liveSites.data?.find((v) => v.id === id) ?? site.data;
  const attached = ((devices.data || []) as Device[]).filter(
    (d) => d.modem !== null && s.modem_ids?.includes(d.modem),
  );
  const cloud = s.provider_code === "deye" && s.id.startsWith("s");
  return (
    <div className="inv-page">
      {cloud ? (
        <div className="row between">
          <Link className="small-link" to="/sites?provider=deye">
            ← Объекты Deye
          </Link>
          {canAssignSite(s) && clientEditor(me.role) && (
            <button className="btn primary" onClick={() => setAssign(true)}>
              Привязать к клиенту
            </button>
          )}
        </div>
      ) : (
        <Subhead
          title={s.name}
          section="sites"
          meta={
            <>
              <Badge status={s.status} />
              <span className="inv-tag">{siteProvider(s)}</span>
              {s.client_id && (
                <EntityLink
                  section="clients"
                  id={s.client_id}
                  label={s.client_name || "Клиент"}
                />
              )}
            </>
          }
          action={
            canAssignSite(s) &&
            clientEditor(me.role) && (
              <button className="btn primary" onClick={() => setAssign(true)}>
                Привязать к клиенту
              </button>
            )
          }
        />
      )}
      {cloud ? (
        <StationDashboard site={s} />
      ) : (
        <div className="inv-columns reverse">
          <section className="card inv-pad">
            <h2>Поток энергии</h2>
            <Wait query={devices} />
            {devices.data && (
              <PowerFlow
                devices={attached}
                onOpen={(deviceID) => navigate("/devices/" + deviceID)}
              />
            )}
          </section>
          <aside className="card inv-side">
            <h2>Паспорт объекта</h2>
            <dl className="inv-dl">
              <dt>Адрес</dt>
              <dd>{s.address || "Не указан"}</dd>
              <dt>Часовой пояс</dt>
              <dd>{s.timezone}</dd>
              <dt>Идентификатор у провайдера</dt>
              <dd className="inv-mono">{s.external_id}</dd>
              <dt>Обновлено</dt>
              <dd>
                <Age value={s.last_synced_at} zone={s.timezone} />
              </dd>
            </dl>
          </aside>
        </div>
      )}
      {!cloud && (
        <section className="card inv-card">
          <div className="inv-section-title">
            <h2>Оборудование объекта</h2>
            <span>{attached.length}</span>
          </div>
          <Wait query={devices} />
          {devices.data && (
            <Table
              columns={deviceColumns([s]).filter(
                (c) => !["Объект", "Клиент"].includes(c.name),
              )}
              rows={attached}
            />
          )}
        </section>
      )}
      {assign && <Assignment site={s} onClose={() => setAssign(false)} />}
    </div>
  );
}
const serviceFields = [
  ["installed_on", "Дата установки"],
  ["serviced_on", "Последнее ТО"],
  ["service_due_on", "Следующее ТО"],
  ["warranty_until", "Гарантия до"],
] as const;
function ServiceEditor({
  service,
  id,
  onClose,
}: {
  service: Service;
  id: number;
  onClose: () => void;
}) {
  const [form, setForm] = useState({
    model: service.model || "",
    nominal_watts: service.nominal_watts?.toString() || "",
    installed_on: isoDay(service.installed_on),
    serviced_on: isoDay(service.serviced_on),
    service_due_on: isoDay(service.service_due_on),
    warranty_until: isoDay(service.warranty_until),
    maintenance: !!service.maintenance,
  });
  const action = useSave(onClose, ["/crm/equipment", "/crm/equipment/" + id]);
  function submit(e: FormEvent) {
    e.preventDefault();
    void action.save(() =>
      api("/crm/equipment/" + id, {
        method: "PUT",
        body: JSON.stringify({
          ...form,
          nominal_watts: form.nominal_watts ? Number(form.nominal_watts) : null,
          ...Object.fromEntries(
            serviceFields.map(([k]) => [k, form[k] || null]),
          ),
        }),
      }),
    );
  }
  return (
    <Sheet title="Сервисная карточка" onClose={onClose}>
      <form
        noValidate
        onSubmitCapture={validateForm}
        className="inv-form"
        onSubmit={submit}
      >
        <label>
          Модель
          <input
            maxLength={120}
            value={form.model}
            onChange={(e) => setForm({ ...form, model: e.target.value })}
          />
        </label>
        <label>
          Номинальная мощность, Вт
          <input
            type="number"
            step="any"
            min="0.000001"
            max="1000000000"
            value={form.nominal_watts}
            onChange={(e) =>
              setForm({ ...form, nominal_watts: e.target.value })
            }
          />
        </label>
        {serviceFields.map(([k, title]) => (
          <label key={k}>
            {title}
            <input
              type="date"
              value={form[k]}
              min={k === "service_due_on" ? form.serviced_on : undefined}
              onChange={(e) => setForm({ ...form, [k]: e.target.value })}
            />
          </label>
        ))}
        <label className="inv-check">
          <input
            type="checkbox"
            checked={form.maintenance}
            onChange={(e) =>
              setForm({ ...form, maintenance: e.target.checked })
            }
          />
          На обслуживании
        </label>
        <ApiError err={action.error} />
        <button className="btn primary" disabled={action.busy}>
          {action.busy ? "Сохранение…" : "Сохранить"}
        </button>
      </form>
    </Sheet>
  );
}
function ServicePanel({
  service,
  zone,
  edit,
}: {
  service: Service;
  zone?: string;
  edit: () => void;
}) {
  const start = Date.parse(service.installed_on || "");
  const end = Date.parse(service.warranty_until || "");
  const remaining =
    end > start
      ? Math.round(
          Math.min(
            100,
            Math.max(0, ((end - Date.now()) / (end - start)) * 100),
          ),
        )
      : null;
  return (
    <div className="inv-pad">
      <div className="inv-title-action">
        <h2>Сервисная карточка</h2>
        {service.can_edit && (
          <button className="inv-mini" onClick={edit}>
            <Wrench size={15} />
            Изменить
          </button>
        )}
      </div>
      <dl className="inv-service">
        <div>
          <dt>Модель</dt>
          <dd>{service.model || "—"}</dd>
        </div>
        <div>
          <dt>Номинальная мощность</dt>
          <dd>
            {service.nominal_watts === null
              ? "—"
              : formatValue(service.nominal_watts) + " Вт"}
          </dd>
        </div>
        {serviceFields.map(([k, title]) => (
          <div key={k}>
            <dt>{title}</dt>
            <dd>{dateText(service[k], zone)}</dd>
            {k === "warranty_until" && remaining !== null && (
              <progress
                value={remaining}
                max={100}
                aria-label={"Остаток гарантии " + remaining + "%"}
              />
            )}
          </div>
        ))}
        <div>
          <dt>Состояние</dt>
          <dd>
            <Badge
              status={
                service.condition === "normal" ? "normal" : service.condition
              }
            />
          </dd>
        </div>
      </dl>
    </div>
  );
}
function DeviceHistory({ id, zone }: { id: number; zone?: string }) {
  const [period, setPeriod] = useState<Period>("day");
  const path = useMemo(() => {
    const from = new Date(
      Date.now() - { day: 1, week: 7, month: 30 }[period] * 86400000,
    );
    return (
      "/devices/" +
      id +
      "/history?from=" +
      encodeURIComponent(from.toISOString()) +
      "&limit=700"
    );
  }, [id, period]);
  const query = useList<{ points: HistorySample[] }>(path);
  return (
    <section className="card inv-pad">
      <h2>История показаний</h2>
      <TelemetryChart
        points={query.data?.points || []}
        period={period}
        onPeriodChange={setPeriod}
        multiMetric
        timezone={zone}
        loading={query.isLoading}
        error={query.error?.message}
      />
      {query.error && (
        <button className="inv-mini" onClick={() => query.refetch()}>
          Повторить
        </button>
      )}
    </section>
  );
}
const orders: Record<number, string> = {
  0: "В очереди",
  100: "Отправляется",
  300: "Применяется",
  400: "Остановлена",
  500: "Ошибка",
  666: "Исполнена",
};
function DeyeCommandsPanel({ device }: { device: Device }) {
  const path = "/modems/" + device.modem + "/deye";
  const [pattern, setPattern] = useState("");
  const [review, setReview] = useState(false);
  const [order, setOrder] = useState<number>();
  const command = useSave(() => setReview(false), []);
  const result = useList<{ status: number; error: string }>(
    path + "/orders/" + order,
    !!order,
  );
  const label = pattern === "LOAD_FIRST" ? "Потребление дома" : "Заряд батареи";
  return (
    <div className="inv-pad">
      <h2>Команды Deye Cloud</h2>
      <form
        noValidate
        onSubmitCapture={validateForm}
        className="inv-form"
        onSubmit={(e) => {
          e.preventDefault();
          setReview(true);
        }}
      >
        <label>
          Приоритет энергии
          <select
            required
            value={pattern}
            onChange={(e) => setPattern(e.target.value)}
          >
            <option value="">Выберите режим</option>
            <option value="LOAD_FIRST">Потребление дома</option>
            <option value="BATTERY_FIRST">Заряд батареи</option>
          </select>
        </label>
        <button className="btn" disabled={!pattern}>
          Изменить режим
        </button>
      </form>
      {review && (
        <Sheet
          title="Изменить режим инвертора?"
          onClose={() => setReview(false)}
        >
          <p>
            Инвертор {device.serial_number || device.device_id} получит команду
            установить приоритет «{label}».
          </p>
          <ApiError err={command.error} />
          <div className="inv-actions">
            <button className="btn" onClick={() => setReview(false)}>
              Отмена
            </button>
            <button
              className="btn primary"
              disabled={command.busy}
              onClick={() =>
                command.save(async () => {
                  const data = await api<{ orderId: number }>(
                    path + "/control/energy-pattern",
                    {
                      method: "POST",
                      body: JSON.stringify({ energyPattern: pattern }),
                    },
                  );
                  setOrder(data.orderId);
                })
              }
            >
              {command.busy ? "Отправка…" : "Отправить команду"}
            </button>
          </div>
        </Sheet>
      )}
      {order && (
        <div className="inv-order">
          <strong>Команда № {order}</strong>
          <Wait query={result} />
          {result.data && (
            <span>
              {orders[result.data.status] || "Неизвестный статус"}{" "}
              {result.data.error}
            </span>
          )}
          <button className="inv-mini" onClick={() => result.refetch()}>
            Проверить выполнение
          </button>
        </div>
      )}
    </div>
  );
}
function DeviceDetails({ id }: { id: string }) {
  const { me, access } = useSession();
  const [tab, setTab] = useState("Сервис");
  const [edit, setEdit] = useState(false);
  const original = useList<Device>("/devices/" + id);
  const live = useLiveDevices();
  const d =
    ((live.data || []) as Device[]).find((v) => String(v.id) === id) ??
    original.data;
  const sites = useList<Site[]>("/crm/sites");
  const service = useList<Service>("/crm/equipment/" + id);
  const alerts = useList<Alert[]>(
    "/alerts?client=" + d?.client,
    !!d && tab === "Аварии",
  );
  const work = useList<Work[]>(
    "/crm/requests",
    !!access.requests?.view && tab === "Заявки",
  );
  const modems = useList<
    { id: number; connection: { protocol?: string } | null }[]
  >("/modems", me.role === "admin");
  if (!d) return <Wait query={original} />;
  const site = siteFor(d, sites.data || []);
  const isCloud =
    d.device_type === "inverter" &&
    modems.data?.some(
      (m) => m.id === d.modem && m.connection?.protocol === "deye_cloud",
    );
  const tabs = [
    "Сервис",
    "Аварии",
    ...(access.requests?.view ? ["Заявки"] : []),
    ...(isCloud && me.role === "admin" ? ["Команды"] : []),
  ];
  const readings = [
    { key: "power", title: "Мощность", unit: "Вт", icon: Zap },
    { key: "voltage", title: "Напряжение", unit: "В", icon: Activity },
    { key: "current", title: "Ток", unit: "А", icon: Gauge },
    { key: "temperature", title: "Температура", unit: "°C", icon: Thermometer },
  ];
  return (
    <div className="inv-page">
      <Subhead
        section="devices"
        title={deviceName(d)}
        meta={
          <>
            <Badge status={d.status} />
            <span>{d.type_display}</span>
            {service.data?.model && <span>{service.data.model}</span>}
            <span className="inv-mono">{d.serial_number || d.device_id}</span>
            <span>
              Связь: <Age value={d.last_seen} zone={site?.timezone} />
            </span>
          </>
        }
        action={
          access.requests?.create && (
            <RequestLink client={d.client} device={d.id} />
          )
        }
      />
      <div className="inv-relations">
        <UserRound size={15} />
        <EntityLink
          section="clients"
          id={d.client}
          label={d.client_name || "Клиент"}
        />
        {site && (
          <>
            <MapPin size={15} />
            <EntityLink
              section="sites"
              id={site.id}
              label={site.name}
              preview={site.address}
            />
            <span className="inv-tag">{siteProvider(site)}</span>
          </>
        )}
      </div>
      <div className="inv-readings">
        {readings.map((m) => {
          const v = d.last_data?.[m.key];
          const Icon = m.icon;
          return (
            <div key={m.key} className="card inv-reading">
              <div>
                <Icon size={17} />
                {m.title}
              </div>
              <strong>
                <LiveValue value={v} />{" "}
                {typeof v === "number" && <small>{m.unit}</small>}
              </strong>
              {typeof v !== "number" ? (
                <small>Нет данных</small>
              ) : d.status === "offline" ? (
                <small>Последнее показание</small>
              ) : null}
            </div>
          );
        })}
        {d.device_type === "battery" && (
          <div className="card inv-reading">
            <div>
              <BatteryCharging size={17} />
              Заряд
            </div>
            <BatteryRing
              value={
                typeof d.last_data?.battery_percent === "number"
                  ? d.last_data.battery_percent
                  : null
              }
            />
          </div>
        )}
      </div>
      <DeviceHistory id={d.id} zone={site?.timezone} />
      <section className="card inv-card">
        <Tabs tabs={tabs} tab={tab} set={setTab} />
        {tab === "Сервис" && (
          <>
            <Wait query={service} />
            {service.data && (
              <ServicePanel
                service={service.data}
                zone={site?.timezone}
                edit={() => setEdit(true)}
              />
            )}
          </>
        )}
        {tab === "Аварии" && (
          <>
            <Wait query={alerts} />
            {!alerts.error && !alerts.isLoading && (
              <Notes
                items={alertNotes(
                  (alerts.data || []).filter((a) => a.device === d.id),
                )}
              />
            )}
          </>
        )}
        {tab === "Заявки" && (
          <>
            <Wait query={work} />
            {!work.error && !work.isLoading && (
              <Notes
                items={workNotes(
                  (work.data || []).filter((w) => w.device_id === d.id),
                )}
              />
            )}
          </>
        )}
        {tab === "Команды" && isCloud && me.role === "admin" && (
          <DeyeCommandsPanel device={d} />
        )}
      </section>
      {edit && service.data?.can_edit && (
        <ServiceEditor
          service={service.data}
          id={d.id}
          onClose={() => setEdit(false)}
        />
      )}
    </div>
  );
}
export function InventoryPage({ section }: { section: Section }) {
  const { id } = useParams();
  if (section === "clients")
    return id ? <ClientDetails key={id} id={id} /> : <ClientList />;
  if (section === "sites")
    return id ? <SiteDetails key={id} id={id} /> : <SiteList />;
  return id ? <DeviceDetails key={id} id={id} /> : <DeviceList />;
}
