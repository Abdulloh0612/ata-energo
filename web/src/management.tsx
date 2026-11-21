import { validateForm } from "./form";
import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  RefreshCw,
  Plug,
  RadioTower,
  UserPlus,
  Plus,
  LockKeyhole,
} from "lucide-react";
import { api, cache, toast, useList, useSession, type Grant } from "./core";
import {
  PageHead,
  CardHead,
  Empty,
  Failure,
  Loading,
  Modal,
  StatusBadge,
} from "./ui";

interface Agent {
  id: number;
  name: string;
  version: string;
  online: boolean;
  hostname: string;
}
interface Account {
  id: number;
  name: string;
  is_active: boolean;
  last_success_at: string | null;
  catalog_error: string;
  last_error: string;
}
interface Provider {
  code: string;
  name: string;
  mock: boolean;
  devices_count: number;
  sites_count: number;
}
interface Found {
  id: number;
  name: string;
  connection_type: string;
  model: string;
}
interface Task {
  id: number;
  status: string;
  method: string;
  log: string;
  error: string;
}
function AgentLog({ agent }: { agent: Agent }) {
  const paths = [
    "/discovery/devices?agent=" + agent.id,
    "/crm/agent-tasks?agent=" + agent.id,
  ];
  const found = useQuery({
    queryKey: [paths[0]],
    queryFn: ({ signal }) => api<Found[]>(paths[0], { signal }),
    refetchInterval: 5000,
  });
  const tasks = useQuery({
    queryKey: [paths[1]],
    queryFn: ({ signal }) => api<Task[]>(paths[1], { signal }),
    refetchInterval: 3000,
  });
  return (
    <div className="stack">
      <h3>Найденное оборудование</h3>
      {found.isPending ? (
        <Loading />
      ) : found.isError ? (
        <Failure retry={found.refetch} />
      ) : found.data?.length ? (
        found.data.map((f) => (
          <p key={f.id}>
            {f.name || f.model} · {f.connection_type}
          </p>
        ))
      ) : (
        <Empty>Обнаруженных устройств пока нет</Empty>
      )}
      <h3>Журнал поиска</h3>
      {tasks.isPending ? (
        <Loading />
      ) : tasks.isError ? (
        <Failure retry={tasks.refetch} />
      ) : tasks.data?.length ? (
        tasks.data.map((t) => (
          <details key={t.id} open={t.status === "running"}>
            <summary>
              #{t.id} · {t.method} ·{" "}
              {
                (
                  {
                    done: "Завершён",
                    error: "Ошибка",
                    pending: "В очереди",
                    running: "Выполняется",
                  } as Record<string, string>
                )[t.status]
              }
            </summary>
            <pre>{t.error || t.log || "Записей пока нет"}</pre>
          </details>
        ))
      ) : (
        <Empty>Поиски ещё не выполнялись</Empty>
      )}
    </div>
  );
}
export function Integrations() {
  const admin = useSession().me.role === "admin",
    providers = useList<Provider[]>("/crm/providers");
  const ac = useList<Account[]>("/crm/integrations/accounts", admin),
    agents = useList<Agent[]>("/agents");
  const [agent, setAgent] = useState<Agent | null>(null),
    [add, setAdd] = useState(false);
  const sync = useMutation({
    mutationFn: (id: number) =>
      api("/crm/integrations/accounts/" + id + "/sync", { method: "POST" }),
    onSuccess: () => {
      toast("Каталог синхронизирован");
      void cache.invalidateQueries();
    },
  });
  const save = useMutation({
    mutationFn: (data: FormData) =>
      api("/deye/accounts", {
        method: "POST",
        body: JSON.stringify(Object.fromEntries(data)),
      }),
    onSuccess: () => {
      setAdd(false);
      toast("Аккаунт подключён");
      void cache.invalidateQueries();
    },
  });
  return (
    <>
      <PageHead
        title="Интеграции"
        subtitle="Источники данных и связь с оборудованием"
        action={
          admin && (
            <button
              className="btn primary"
              onClick={() => {
                save.reset();
                setAdd(true);
              }}
            >
              <Plus size={16} />
              Подключить Deye
            </button>
          )
        }
      />
      <div className="provider-grid">
        {providers.isError ? (
          <Failure retry={providers.refetch} />
        ) : providers.isPending ? (
          <Loading />
        ) : (
          (providers.data ?? []).map((p) => (
            <section className="card provider" key={p.code}>
              <div className="row between">
                <span className="feed-icon amber">
                  {p.code === "local" ? (
                    <RadioTower size={20} />
                  ) : (
                    <Plug size={20} />
                  )}
                </span>
                {p.mock && <span className="badge warn">Демо</span>}
              </div>
              <h2>{p.code === "deye" ? "Deye Cloud" : p.name}</h2>
              <p className="muted">
                Объекты · {p.sites_count}　Устройства · {p.devices_count}
              </p>
            </section>
          ))
        )}
      </div>
      {admin && (
        <section className="card">
          <CardHead title="Облачные аккаунты" />
          {sync.isError && <Failure message={sync.error.message} />}{" "}
          {ac.isPending ? (
            <Loading />
          ) : ac.isError ? (
            <Failure retry={ac.refetch} />
          ) : ac.data?.length ? (
            ac.data.map((a) => (
              <div className="listing" key={a.id}>
                <Plug size={19} />
                <div>
                  <strong>{a.name}</strong>
                  <small>
                    {a.is_active ? "Активен" : "Отключён"} ·{" "}
                    {a.last_success_at
                      ? "Синхронизация " +
                        new Date(a.last_success_at).toLocaleString("ru-RU")
                      : "Синхронизации ещё не было"}
                  </small>
                  {(a.catalog_error || a.last_error) && (
                    <small className="danger">
                      {a.catalog_error || a.last_error}
                    </small>
                  )}
                </div>
                <button
                  className="btn"
                  disabled={!a.is_active || sync.isPending}
                  onClick={() => sync.mutate(a.id)}
                >
                  <RefreshCw size={15} />
                  Синхронизировать
                </button>
              </div>
            ))
          ) : (
            <Empty>Облачных аккаунтов пока нет</Empty>
          )}
        </section>
      )}
      <section className="card">
        <CardHead title="Локальные агенты" />
        {agents.isPending ? (
          <Loading />
        ) : agents.isError ? (
          <Failure retry={agents.refetch} />
        ) : agents.data?.length ? (
          agents.data.map((a) => (
            <button
              className="listing full"
              key={a.id}
              onClick={() => setAgent(a)}
            >
              <RadioTower size={18} />
              <span>
                {a.name}
                <small>
                  {a.hostname} {a.version && "· " + a.version}
                </small>
              </span>
              <StatusBadge status={a.online ? "online" : "offline"} />
            </button>
          ))
        ) : (
          <Empty>Зарегистрированных агентов пока нет</Empty>
        )}
      </section>
      <Modal
        title={agent?.name ?? "Агент"}
        open={!!agent}
        onClose={() => setAgent(null)}
      >
        {agent && <AgentLog agent={agent} />}
      </Modal>
      <Modal
        title="Подключить Deye Cloud"
        open={add}
        onClose={() => {
          if (!save.isPending) setAdd(false);
        }}
      >
        <form
          noValidate
          onSubmitCapture={validateForm}
          className="stack"
          onSubmit={(e) => {
            e.preventDefault();
            if (!save.isPending) save.mutate(new FormData(e.currentTarget));
          }}
        >
          {[
            ["name", "Название", "text"],
            ["email", "Почта аккаунта", "email"],
            ["app_id", "App ID", "text"],
            ["app_secret", "App Secret", "password"],
            ["password", "Пароль Deye Cloud", "password"],
          ].map(([k, n, t]) => (
            <label key={k}>
              {n}
              <input type={t} name={k} required autoComplete="off" />
            </label>
          ))}
          {save.isError && <Failure message={save.error.message} />}
          <button className="btn primary" disabled={save.isPending}>
            Подключить
          </button>
        </form>
      </Modal>
    </>
  );
}
interface Role {
  code: string;
  name: string;
  system: boolean;
  grants: Record<"requests" | "tasks", Grant>;
}
interface Employee {
  id: number;
  username: string;
  first_name: string;
  last_name: string;
  role: string;
  role_display: string;
  position: string;
  is_active: boolean;
}
const rights: Record<keyof Grant, string> = {
  view: "Просмотр",
  create: "Создание",
  edit: "Изменение",
  assign: "Назначение",
  delete: "Удаление",
  own: "Только свои",
};
const blankGrant = (): Grant => ({
  view: false,
  create: false,
  edit: false,
  assign: false,
  delete: false,
  own: false,
});
function RoleMatrix({ roles }: { roles: Role[] }) {
  const [draft, setDraft] = useState(roles),
    [newRole, setNewRole] = useState(false),
    [dirty, setDirty] = useState("");
  useEffect(() => {
    if (!dirty) setDraft(roles);
  }, [roles, dirty]);
  const save = useMutation({
    mutationFn: ({ role, create }: { role: Role; create: boolean }) =>
      api("/crm/roles" + (create ? "" : "/" + role.code), {
        method: create ? "POST" : "PUT",
        body: JSON.stringify(role),
      }),
    onSuccess: () => {
      setDirty("");
      setNewRole(false);
      void cache.invalidateQueries({ queryKey: ["/crm/roles"] });
      void cache.invalidateQueries({ queryKey: ["/crm/access"] });
      toast("Роль сохранена");
    },
  });
  const [kind, setKind] = useState<"requests" | "tasks">("requests");
  function toggle(r: Role, k: keyof Grant) {
    const g = { ...r.grants[kind], [k]: !r.grants[kind][k] };
    if (k === "view" && !g.view) Object.assign(g, blankGrant());
    if (k !== "view" && g[k]) g.view = true;
    if (g.assign && !g.edit && !g.create) {
      if (k === "assign") g.edit = true;
      else g.assign = false;
    }
    setDirty(r.code);
    setDraft(
      draft.map((x) =>
        x.code === r.code ? { ...r, grants: { ...r.grants, [kind]: g } } : x,
      ),
    );
  }
  return (
    <section className="card">
      <div className="cardhead">
        <h2>Матрица прав</h2>
        <button
          className="btn"
          onClick={() => {
            save.reset();
            setNewRole(true);
          }}
        >
          <Plus size={15} />
          Новая роль
        </button>
      </div>
      <div className="toolbar">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as typeof kind)}
          aria-label="Раздел матрицы"
        >
          <option value="requests">Заявки</option>
          <option value="tasks">Задачи</option>
        </select>
        {dirty && (
          <>
            <button
              className="btn primary"
              disabled={save.isPending}
              onClick={() =>
                save.mutate({
                  role: draft.find((r) => r.code === dirty)!,
                  create: false,
                })
              }
            >
              Сохранить
            </button>
            <button
              className="btn"
              onClick={() => {
                setDraft(roles);
                setDirty("");
              }}
            >
              Отменить
            </button>
          </>
        )}
      </div>
      <div className="scroll-table">
        <table>
          <thead>
            <tr>
              <th>Операция</th>
              {draft.map((r) => (
                <th key={r.code}>
                  {r.name}{" "}
                  {r.system && (
                    <LockKeyhole size={12} aria-label="Встроенная роль" />
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(Object.keys(rights) as (keyof Grant)[]).map((k) => (
              <tr key={k}>
                <td>{rights[k]}</td>
                {draft.map((r) => (
                  <td key={r.code}>
                    <input
                      type="checkbox"
                      aria-label={r.name + " · " + rights[k]}
                      checked={!!r.grants[kind]?.[k]}
                      disabled={
                        r.system ||
                        save.isPending ||
                        (!!dirty && dirty !== r.code)
                      }
                      onChange={() => toggle(r, k)}
                    />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {save.isError && <Failure message={save.error.message} />}
      <Modal
        title="Новая CRM-роль"
        open={newRole}
        onClose={() => {
          if (!save.isPending) setNewRole(false);
        }}
      >
        <form
          noValidate
          onSubmitCapture={validateForm}
          className="stack"
          onSubmit={(e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            save.mutate({
              role: {
                code: "crm_" + f.get("code"),
                name: String(f.get("name")),
                system: false,
                grants: { tasks: blankGrant(), requests: blankGrant() },
              },
              create: true,
            });
          }}
        >
          <label>
            Название
            <input name="name" maxLength={80} required />
          </label>
          <label>
            Код после crm_
            <input
              name="code"
              required
              pattern="[a-z][a-z0-9_]{1,15}"
              placeholder="service"
            />
          </label>
          {save.isError && <Failure message={save.error.message} />}
          <button disabled={save.isPending} className="btn primary">
            Создать роль
          </button>
        </form>
      </Modal>
    </section>
  );
}
export function Admin() {
  const admin = useSession().me.role === "admin",
    list = useList<Employee[]>("/users?staff=1"),
    roles = useList<Role[]>("/crm/roles", admin);
  const [member, setMember] = useState<Employee | "new" | null>(null);
  const save = useMutation({
    mutationFn: async (f: FormData) => {
      const data: Record<string, unknown> = Object.fromEntries(f);
      data.is_active = f.has("is_active");
      if (!data.password) delete data.password;
      return api(
        member === "new" ? "/users" : "/users/" + (member as Employee).id,
        {
          method: member === "new" ? "POST" : "PATCH",
          body: JSON.stringify(data),
        },
      );
    },
    onSuccess: () => {
      setMember(null);
      void list.refetch();
      toast("Сотрудник сохранён");
    },
  });
  const current = typeof member === "object" ? member : null;
  return (
    <>
      <PageHead
        title="Администрирование"
        subtitle="Сотрудники и права доступа"
        action={
          admin && (
            <button
              className="btn primary"
              onClick={() => {
                save.reset();
                setMember("new");
              }}
            >
              <UserPlus size={16} />
              Добавить сотрудника
            </button>
          )
        }
      />
      <section className="card">
        <CardHead title="Сотрудники" />
        {list.isPending ? (
          <Loading />
        ) : list.isError ? (
          <Failure retry={list.refetch} />
        ) : (
          <div className="scroll-table">
            <table>
              <thead>
                <tr>
                  <th>Сотрудник</th>
                  <th>Логин</th>
                  <th>Роль</th>
                  <th>Доступ</th>
                  {admin && <th />}
                </tr>
              </thead>
              <tbody>
                {(list.data ?? []).map((u) => (
                  <tr key={u.id}>
                    <td>
                      {[u.first_name, u.last_name].filter(Boolean).join(" ") ||
                        u.username}
                    </td>
                    <td>{u.username}</td>
                    <td>{u.role_display}</td>
                    <td>{u.is_active ? "Активен" : "Отключён"}</td>
                    {admin && (
                      <td>
                        <button
                          className="small-link"
                          onClick={() => {
                            save.reset();
                            setMember(u);
                          }}
                        >
                          Изменить
                        </button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      {admin &&
        (roles.isPending ? (
          <Loading />
        ) : roles.isError ? (
          <Failure retry={roles.refetch} />
        ) : (
          <RoleMatrix roles={roles.data ?? []} />
        ))}
      <Modal
        title={member === "new" ? "Новый сотрудник" : "Изменить сотрудника"}
        open={member !== null}
        onClose={() => {
          if (!save.isPending) setMember(null);
        }}
      >
        {member && (
          <form
            noValidate
            onSubmitCapture={validateForm}
            className="stack"
            key={current?.id ?? "new"}
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate(new FormData(e.currentTarget));
            }}
          >
            {[
              ["username", "Логин"],
              ["first_name", "Имя"],
              ["last_name", "Фамилия"],
              ["position", "Должность"],
            ].map(([k, n]) => (
              <label key={k}>
                {n}
                <input
                  name={k}
                  required={k === "username"}
                  defaultValue={String(current?.[k as keyof Employee] ?? "")}
                />
              </label>
            ))}
            <label>
              Роль
              <select name="role" defaultValue={current?.role ?? "observer"}>
                {(roles.data ?? []).map((r) => (
                  <option value={r.code} key={r.code}>
                    {r.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {current ? "Новый пароль (по желанию)" : "Пароль"}
              <input
                name="password"
                type="password"
                autoComplete="new-password"
                required={!current}
              />
            </label>
            <label className="row">
              <input
                type="checkbox"
                name="is_active"
                defaultChecked={current?.is_active ?? true}
              />
              Доступ активен
            </label>
            {save.isError && <Failure message={save.error.message} />}
            <button
              className="btn primary"
              disabled={save.isPending || !roles.data}
            >
              Сохранить
            </button>
          </form>
        )}
      </Modal>
    </>
  );
}
