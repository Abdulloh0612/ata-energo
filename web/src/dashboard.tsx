import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import {
  Activity,
  AlarmClock,
  ArrowUpRight,
  TriangleAlert,
  RadioTower,
  ClipboardList,
  CloudOff,
  CheckCheck,
  Check,
  Plus,
} from "lucide-react";
import {
  api,
  cache,
  relative,
  toast,
  useLiveDevices,
  useSession,
  useList,
  type Site,
  type AlertItem,
} from "./core";
import { CardHead, Empty, Failure, Loading, PageHead } from "./ui";
import {
  SiteMap,
  TelemetryChart,
  type Period,
  type HistorySample,
} from "./visuals";
export function Generation({ initial = "day" }: { initial?: Period }) {
  const [period, setPeriod] = useState<Period>(initial);
  const q = useList<HistorySample[]>(
    "/crm/generation?period=" + period,
    true,
    120000,
  );
  return (
    <section className="card chartcard">
      <CardHead title="Генерация энергии" />
      <TelemetryChart
        points={q.data ?? []}
        period={period}
        onPeriodChange={setPeriod}
        loading={q.isPending}
        error={q.isError ? "Не удалось загрузить историю." : undefined}
      />
      {q.isError && (
        <button className="btn" onClick={() => q.refetch()}>
          Повторить
        </button>
      )}
    </section>
  );
}
export interface Job {
  id: number;
  title: string;
  due_at: string | null;
  status: string;
  assignee_id: number | null;
  priority: string;
}
function AlertFeed({ items }: { items: AlertItem[] }) {
  return items.length ? (
    <div className="feed">
      {items.map((a) => (
        <Link
          to={"/devices/" + a.device}
          className={
            "feed-line " +
            (Date.now() - Date.parse(a.created_at) < 2200 ? "fresh" : "")
          }
          key={a.id}
        >
          <span
            className={"feed-icon " + (a.kind === "offline" ? "amber" : "red")}
          >
            {a.kind === "offline" ? (
              <CloudOff size={18} />
            ) : (
              <TriangleAlert size={18} />
            )}
          </span>
          <span>
            <strong>
              {a.kind === "offline" ? "Потеря связи" : "Ошибка оборудования"}
            </strong>
            <small>
              {a.client_name} · {a.device_code}
            </small>
          </span>
          <time title={new Date(a.created_at).toLocaleString("ru-RU")}>
            {relative(a.created_at)}
          </time>
        </Link>
      ))}
    </div>
  ) : (
    <Empty>Оповещений пока нет</Empty>
  );
}
export function Dashboard() {
  const { me, access } = useSession(),
    navigate = useNavigate();
  const sites = useList<Site[]>("/crm/sites"),
    devices = useLiveDevices(),
    alerts = useList<AlertItem[]>("/alerts");
  const jobs = useList<Job[]>("/crm/tasks", access.tasks.view),
    requests = useList<Job[]>("/crm/requests", access.requests.view);
  const activeAlerts = useList<AlertItem[]>("/crm/active-alerts", true, 30000);
  const activeSites = sites.data?.filter((s) => !s.archived);
  const stats = [
    {
      name: "Станции в сети",
      value: activeSites?.filter((s) => ["normal", "online"].includes(s.status))
        .length,
      url: "/sites?status=online",
      icon: RadioTower,
      tone: "green",
    },
    {
      name: "Станции без связи",
      value: activeSites?.filter((s) => s.status === "offline").length,
      url: "/sites?status=offline",
      icon: CloudOff,
      tone: "gray",
    },
    {
      name: "Активные аварии",
      value: activeAlerts.data?.length,
      url: "/alerts?read=active",
      icon: TriangleAlert,
      tone: "red",
    },
    ...(access.requests.view
      ? [
          {
            name: "Открытые заявки",
            value: requests.data?.filter((r) => r.status !== "closed").length,
            url: "/requests?status=active",
            icon: ClipboardList,
            tone: "violet",
          },
        ]
      : []),
  ];
  const mine = (jobs.data ?? [])
    .filter((j) => j.assignee_id === me.id && j.status !== "closed")
    .sort((a, b) => (a.due_at ?? "9999").localeCompare(b.due_at ?? "9999"))
    .slice(0, 5);
  const offline = (devices.data ?? [])
    .filter((d) => d.status === "offline")
    .sort((a, b) => (a.last_seen ?? "").localeCompare(b.last_seen ?? ""))
    .slice(0, 5);
  return (
    <>
      <PageHead
        title="Обзор станций"
        subtitle={new Intl.DateTimeFormat("ru-RU", {
          weekday: "long",
          day: "numeric",
          month: "long",
          year: "numeric",
        }).format(new Date())}
        action={
          access.requests.create && (
            <Link className="btn primary" to="/requests?new=1">
              <Plus size={17} />
              Создать заявку
            </Link>
          )
        }
      />
      <div className="stats">
        {stats.map((s) => (
          <Link to={s.url} className={"stat " + s.tone} key={s.name}>
            <span className="stat-icons">
              <s.icon size={18} />
              <ArrowUpRight size={15} />
            </span>
            <span className="stat-number">{s.value ?? "—"}</span>
            <span>{s.name}</span>
          </Link>
        ))}
      </div>
      {[
        sites,
        devices,
        alerts,
        activeAlerts,
        ...(access.tasks.view ? [jobs] : []),
        ...(access.requests.view ? [requests] : []),
      ].some((q) => q.isError) && (
        <Failure retry={() => void cache.invalidateQueries()} />
      )}
      <div className="dashgrid">
        <Generation />
        <section className="card">
          <CardHead title="Последние аварии" href="/alerts" />
          {alerts.isPending ? (
            <Loading />
          ) : (
            <AlertFeed items={(alerts.data ?? []).slice(0, 4)} />
          )}
        </section>
        <section className="card">
          <CardHead title="Карта объектов" href="/sites?view=map" />
          <SiteMap
            sites={activeSites ?? []}
            compact
            onOpen={(id) => navigate("/sites/" + id)}
          />
        </section>
        <section className="card">
          <CardHead
            title="Устройства без связи"
            href="/devices?status=offline"
          />
          {devices.isPending ? (
            <Loading />
          ) : offline.length ? (
            offline.map((d) => (
              <Link className="listing" key={d.id} to={"/devices/" + d.id}>
                <CloudOff size={17} />
                <span>
                  {d.name || d.device_id}
                  <small>{d.client_name}</small>
                </span>
                <time title={d.last_seen ?? ""}>{relative(d.last_seen)}</time>
              </Link>
            ))
          ) : (
            <Empty>Устройств без связи нет</Empty>
          )}
        </section>
        {access.tasks.view && (
          <section className="card">
            <CardHead title="Мои задачи" href="/tasks?mine=1" />
            {jobs.isPending ? (
              <Loading />
            ) : mine.length ? (
              mine.map((j) => (
                <Link key={j.id} className="listing" to={"/tasks?id=" + j.id}>
                  <AlarmClock size={18} />
                  <span>{j.title}</span>
                  <time
                    className={
                      j.due_at && Date.parse(j.due_at) < Date.now()
                        ? "danger"
                        : ""
                    }
                  >
                    {j.due_at
                      ? new Date(j.due_at).toLocaleDateString("ru-RU")
                      : "Без срока"}
                  </time>
                </Link>
              ))
            ) : (
              <Empty>У вас нет открытых задач</Empty>
            )}
          </section>
        )}
      </div>
    </>
  );
}
export function Alerts() {
  const { access, me } = useSession(),
    [params, set] = useSearchParams(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const active = params.get("read") === "active";
  const q = useList<AlertItem[]>(
      active ? "/crm/active-alerts" : "/alerts",
      true,
      active ? 30000 : undefined,
    ),
    d = useLiveDevices(),
    s = useList<Site[]>("/crm/sites");
  const mayRead = ["admin", "it_head", "dispatcher"].includes(me.role);
  const prov = new Map(
    (s.data ?? []).flatMap((site) =>
      (site.modem_ids ?? []).map((id) => [id, site.provider_code] as const),
    ),
  );
  const source = new Map(
    (d.data ?? []).map((dev) => [dev.id, dev.modem ? prov.get(dev.modem) : ""]),
  );
  const items = (q.data ?? []).filter(
    (a) =>
      (!params.get("kind") || a.kind === params.get("kind")) &&
      (!params.get("source") ||
        source.get(a.device) === params.get("source")) &&
      (active ||
        !params.get("read") ||
        a.is_read === (params.get("read") === "read")),
  );
  function filter(key: string, val: string) {
    const next = new URLSearchParams(params);
    if (val) next.set(key, val);
    else next.delete(key);
    set(next, { replace: true });
  }
  async function mark(id?: number) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(id ? "/alerts/" + id + "/read" : "/alerts/read_all", {
        method: "POST",
      });
      await Promise.all([
        q.refetch(),
        cache.invalidateQueries({ queryKey: ["/alerts"] }),
      ]);
      toast("Оповещения отмечены прочитанными");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHead
        title="Аварии"
        subtitle={
          active
            ? "События, для которых проблема сохраняется"
            : "Журнал последних 200 оповещений оборудования"
        }
        action={
          mayRead && (
            <button className="btn" disabled={busy} onClick={() => mark()}>
              <CheckCheck size={17} />
              Прочитать все
            </button>
          )
        }
      />
      <section className="card">
        <div className="toolbar">
          {[
            [
              "kind",
              "Уровень",
              [
                ["error", "Критические · ошибка"],
                ["offline", "Предупреждения · без связи"],
              ],
            ],
            [
              "read",
              "Статус",
              [
                ["active", "Активные"],
                ["unread", "Непрочитанные"],
                ["read", "Прочитанные"],
              ],
            ],
            [
              "source",
              "Источник",
              [
                ["deye", "Deye Cloud"],
                ["local", "Локальный модем"],
                ["eybond", "Eybond"],
              ],
            ],
          ].map((entry) => {
            const [k, label, options] = entry as [string, string, string[][]];
            return (
              <select
                key={k}
                aria-label={label}
                value={params.get(k) ?? ""}
                onChange={(e) => filter(k, e.target.value)}
              >
                <option value="">{label} · все</option>
                {options.map(([v, n]) => (
                  <option key={v} value={v}>
                    {n}
                  </option>
                ))}
              </select>
            );
          })}
        </div>
        {error && <Failure message={error} />}
        {q.isPending ? (
          <Loading />
        ) : q.isError ? (
          <Failure retry={q.refetch} />
        ) : !items.length ? (
          <Empty>По этим условиям оповещений нет</Empty>
        ) : (
          items.map((a) => (
            <article className="alert-row" key={a.id}>
              <span
                className={
                  "feed-icon " + (a.kind === "offline" ? "amber" : "red")
                }
              >
                <Activity size={18} />
              </span>
              <div>
                <b>{a.text}</b>
                <small>
                  <Link to={"/clients/" + a.client_id}>{a.client_name}</Link> ·{" "}
                  {new Date(a.created_at).toLocaleString("ru-RU")} ·{" "}
                  {a.is_read ? "Прочитано" : "Новое"}
                </small>
                <div className="actions">
                  <Link to={"/devices/" + a.device}>Открыть устройство</Link>
                  {access.requests.create && (
                    <Link
                      to={
                        "/requests?new=1&device=" +
                        a.device +
                        "&client=" +
                        a.client_id
                      }
                    >
                      Создать заявку
                    </Link>
                  )}
                  {mayRead && !a.is_read && (
                    <button disabled={busy} onClick={() => mark(a.id)}>
                      <Check size={14} />
                      Прочитано
                    </button>
                  )}
                </div>
              </div>
            </article>
          ))
        )}
      </section>
    </>
  );
}
