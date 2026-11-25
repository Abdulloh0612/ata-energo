import { createRoot } from "react-dom/client";
import { lazy, Suspense, useEffect, useRef, useState } from "react";
import {
  BrowserRouter,
  Link,
  NavLink,
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { QueryClientProvider } from "@tanstack/react-query";
import { Command } from "cmdk";
import * as Menu from "@radix-ui/react-dropdown-menu";
import {
  Bell,
  Building2,
  Check,
  ChevronDown,
  ClipboardCheck,
  ClipboardList,
  Component,
  LayoutDashboard,
  LogOut,
  Menu as MenuIcon,
  PanelLeftClose,
  PanelLeftOpen,
  Plug,
  Search,
  ShieldCheck,
  Sun,
  TriangleAlert,
  Users,
  X,
} from "lucide-react";
import {
  api,
  beep,
  loginTelegram,
  cache,
  logout,
  Session,
  themes,
  useAuth,
  useList,
  useLiveDevices,
  usePrefs,
  useSession,
  useStream,
  type Access,
  type AlertItem,
  type Me,
  type Site,
  type Theme,
} from "./core";
import {
  Brand,
  Empty,
  Failure,
  Loading,
  Modal,
  OfflineBanner,
  Toasts,
} from "./ui";
import { Login } from "./login";
import { telegram } from "./telegram";
import "./styles.css";
import "./mobile.css";
const Dashboard = lazy(() =>
  import("./dashboard").then((m) => ({ default: m.Dashboard })),
);
const Alerts = lazy(() =>
  import("./dashboard").then((m) => ({ default: m.Alerts })),
);
const AlertDetail = lazy(() =>
  import("./dashboard").then((m) => ({ default: m.AlertDetail })),
);
const Inventory = lazy(() =>
  import("./inventory").then((m) => ({ default: m.InventoryPage })),
);
const Work = lazy(() =>
  import("./work").then((m) => ({ default: m.WorkPage })),
);
const Integrations = lazy(() =>
  import("./management").then((m) => ({ default: m.Integrations })),
);
const Admin = lazy(() =>
  import("./management").then((m) => ({ default: m.Admin })),
);
const Portal = lazy(() =>
  import("./portal").then((m) => ({ default: m.Portal })),
);
const nav = [
  { key: "overview", to: "/", label: "Обзор", icon: LayoutDashboard },
  { key: "clients", to: "/clients", label: "Клиенты", icon: Users },
  { key: "sites", to: "/sites", label: "Объекты", icon: Building2 },
  { key: "devices", to: "/devices", label: "Оборудование", icon: Component },
  { key: "alerts", to: "/alerts", label: "Аварии", icon: TriangleAlert },
  { key: "requests", to: "/requests", label: "Заявки", icon: ClipboardList },
  { key: "tasks", to: "/tasks", label: "Задачи", icon: ClipboardCheck },
  { key: "integrations", to: "/integrations", label: "Интеграции", icon: Plug },
  { key: "admin", to: "/admin", label: "Администрирование", icon: ShieldCheck },
];
function Palette({ close }: { close: () => void }) {
  const navigate = useNavigate(),
    { access } = useSession(),
    [query, setQuery] = useState("");
  const customers =
      useList<{ id: number; name: string; phone: string }[]>("/clients"),
    sites = useList<Site[]>("/crm/sites"),
    dev = useLiveDevices(),
    requests = useList<{ id: number; title: string }[]>(
      "/crm/requests",
      access.requests.view,
    );
  const groups: [string, { text: string; href: string; hint?: string }[]][] = [
    [
      "Разделы",
      nav
        .filter((n) => access.sections[n.key])
        .map((n) => ({ text: n.label, href: n.to })),
    ],
    [
      "Действия",
      access.requests.create
        ? [{ text: "Создать заявку", href: "/requests?new=1" }]
        : [],
    ],
    [
      "Клиенты",
      (customers.data ?? []).map((c) => ({
        text: c.name,
        hint: c.phone,
        href: "/clients/" + c.id,
      })),
    ],
    [
      "Объекты",
      (sites.data ?? []).map((s) => ({ text: s.name, href: "/sites/" + s.id })),
    ],
    [
      "Оборудование",
      (dev.data ?? []).map((d) => ({
        text: d.name || d.device_id,
        hint: d.serial_number + " " + d.device_id,
        href: "/devices/" + d.id,
      })),
    ],
    [
      "Заявки",
      (requests.data ?? []).map((r) => ({
        text: r.title,
        hint: "#" + r.id,
        href: "/requests?id=" + r.id,
      })),
    ],
  ];
  return (
    <Modal title="Поиск по системе" open onClose={close}>
      <Command label="Поиск" shouldFilter={false}>
        <div className="palette-input">
          <Search size={17} />
          <Command.Input
            placeholder="Клиент, телефон, серийный номер…"
            value={query}
            onValueChange={setQuery}
          />
          {query && (
            <button
              className="iconbtn"
              aria-label="Очистить поиск"
              onClick={() => setQuery("")}
            >
              <X size={15} />
            </button>
          )}
        </div>
        <Command.List>
          {groups.map(([title, entries], i) => {
            const matches = entries
              .filter((e) =>
                (e.text + " " + (e.hint ?? ""))
                  .toLocaleLowerCase("ru")
                  .includes(query.trim().toLocaleLowerCase("ru")),
              )
              .slice(0, i < 2 ? 10 : 5);
            return matches.length && (query || i < 2) ? (
              <Command.Group heading={title} key={title}>
                {matches.map((e) => (
                  <Command.Item
                    key={e.href}
                    value={title + e.href}
                    onSelect={() => {
                      close();
                      navigate(e.href);
                    }}
                  >
                    {e.text}
                    <small>{e.hint}</small>
                  </Command.Item>
                ))}
              </Command.Group>
            ) : null;
          })}
          <Command.Empty>Совпадений нет</Command.Empty>
        </Command.List>
      </Command>
    </Modal>
  );
}
function Profile() {
  const telegramStatus = useList<{
    linked: boolean;
    notifications_enabled: boolean;
    notification_preferences: Record<string, boolean>;
  }>("/me/telegram");
  async function updateTelegramSettings(body: object) {
    setLinkPending(true);
    setLinkError("");
    try {
      const settings = await api("/me/telegram", {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      cache.setQueryData(["/me/telegram"], settings);
    } catch (error) {
      setLinkError(
        error instanceof Error
          ? error.message
          : "Не удалось сохранить настройки.",
      );
    } finally {
      setLinkPending(false);
    }
  }
  const [linkOpen, setLinkOpen] = useState(false);
  const [link, setLink] = useState("");
  const [linkError, setLinkError] = useState("");
  const [linkPending, setLinkPending] = useState(false);
  async function createLink() {
    setLinkOpen(true);
    setLink("");
    setLinkError("");
    setLinkPending(true);
    try {
      setLink(
        (await api<{ url: string }>("/me/telegram/link", { method: "POST" }))
          .url,
      );
    } catch (error) {
      setLinkError(
        error instanceof Error ? error.message : "Не удалось создать ссылку.",
      );
    } finally {
      setLinkPending(false);
    }
  }
  const { me } = useSession(),
    prefs = usePrefs();
  return (
    <>
      <Menu.Root>
        <Menu.Trigger className="profile" aria-label="Профиль и настройки">
          <span className="avatar">
            {(me.first_name || me.username).slice(0, 1).toUpperCase()}
          </span>
          <span>
            <b>{me.first_name || me.username}</b>
            <small>{me.role_display}</small>
          </span>
          <ChevronDown size={14} />
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Content sideOffset={9} className="popover">
            <Menu.Label>{me.username}</Menu.Label>
            <Menu.CheckboxItem
              checked={prefs.notifications}
              onCheckedChange={(notifications) =>
                usePrefs.setState({ notifications })
              }
            >
              Уведомления {prefs.notifications && <Check size={14} />}
            </Menu.CheckboxItem>
            <Menu.CheckboxItem
              checked={prefs.sound}
              onCheckedChange={(sound) => {
                usePrefs.setState({ sound });
                if (sound) beep();
              }}
            >
              Звук аварий {prefs.sound && <Check size={14} />}
            </Menu.CheckboxItem>
            {telegramStatus.data && (
              <Menu.Item
                disabled={linkPending}
                onSelect={() => {
                  if (telegramStatus.data.linked) {
                    setLinkError("");
                    setLinkOpen(true);
                  } else void createLink();
                }}
              >
                {telegramStatus.data.linked
                  ? "Уведомления Telegram"
                  : "Привязать Telegram"}
              </Menu.Item>
            )}
            <Menu.Separator />
            <Menu.Item onSelect={logout}>
              <LogOut size={15} />
              Выйти
            </Menu.Item>
          </Menu.Content>
        </Menu.Portal>
      </Menu.Root>
      <Modal
        title={
          telegramStatus.data?.linked
            ? "Уведомления Telegram"
            : "Привязка Telegram"
        }
        open={linkOpen}
        onClose={() => {
          setLinkOpen(false);
          void telegramStatus.refetch();
        }}
      >
        {linkPending && !telegramStatus.data?.linked && <Loading />}
        {telegramStatus.data?.linked && (
          <fieldset disabled={linkPending} aria-label="Уведомления Telegram">
            <p>
              <label>
                <input
                  type="checkbox"
                  checked={telegramStatus.data.notifications_enabled}
                  onChange={(e) =>
                    void updateTelegramSettings({
                      notifications_enabled: e.target.checked,
                    })
                  }
                />{" "}
                Получать уведомления
              </label>
            </p>
            {Object.entries({
              incidents: "Аварии",
              work: "Выезды и задачи",
              inventory: "Склад",
              attendance: "Посещаемость",
            }).map(([key, title]) => (
              <p key={key}>
                <label>
                  <input
                    type="checkbox"
                    checked={
                      telegramStatus.data.notification_preferences[key] ?? false
                    }
                    onChange={(e) =>
                      void updateTelegramSettings({
                        notification_preferences: { [key]: e.target.checked },
                      })
                    }
                  />{" "}
                  {title}
                </label>
              </p>
            ))}
          </fieldset>
        )}
        {linkError && <p role="alert">{linkError}</p>}
        {link && (
          <div>
            <p>
              Откройте Telegram и нажмите «Запустить». Ссылка действует 10
              минут.
            </p>
            <a className="btn" href={link} target="_blank" rel="noreferrer">
              Открыть бота
            </a>
          </div>
        )}
      </Modal>
    </>
  );
}
function ThemePicker() {
  const theme = usePrefs((x) => x.theme);
  return (
    <Menu.Root>
      <Menu.Trigger
        className="iconbtn"
        title="Тема оформления"
        aria-label="Тема оформления"
      >
        <Sun size={19} />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content className="popover" sideOffset={8}>
          <Menu.Label>Тема оформления</Menu.Label>
          <Menu.RadioGroup
            value={theme}
            onValueChange={(t) => usePrefs.setState({ theme: t as Theme })}
          >
            {Object.entries(themes).map(([k, name]) => (
              <Menu.RadioItem value={k} key={k}>
                <i className={"theme-dot " + k} />
                {name}
                <Menu.ItemIndicator>
                  <Check size={14} />
                </Menu.ItemIndicator>
              </Menu.RadioItem>
            ))}
          </Menu.RadioGroup>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}
function Shell() {
  const { me, access } = useSession(),
    customer = me.role === "user",
    loc = useLocation(),
    navigate = useNavigate(),
    compact = usePrefs((s) => s.compact);
  const stream = useStream(me.role),
    [palette, setPalette] = useState(false),
    [mobile, setMobile] = useState(false),
    [bell, setBell] = useState(false);
  const alerts = useList<AlertItem[]>("/alerts"),
    seq = useRef(0),
    links = nav.filter((n) => access.sections[n.key]);
  useEffect(() => {
    setMobile(false);
    setBell(false);
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  useEffect(() => {
    function key(e: KeyboardEvent) {
      if (customer || e.isComposing) return;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((x) => !x);
        return;
      }
      if (
        e.altKey ||
        e.ctrlKey ||
        e.metaKey ||
        e.shiftKey ||
        document.querySelector("[role=dialog]") ||
        (e.target as HTMLElement).closest(
          "input,select,textarea,[contenteditable=true]",
        )
      )
        return;
      if (e.key.toLowerCase() === "g") seq.current = Date.now();
      if (
        e.key.toLowerCase() === "c" &&
        Date.now() - seq.current < 1200 &&
        access.sections.clients
      )
        navigate("/clients");
      if (e.key.toLowerCase() === "n" && access.requests.create)
        navigate("/requests?new=1");
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [access, customer, navigate]);
  const page =
    nav.find((n) => n.to !== "/" && loc.pathname.startsWith(n.to)) ?? nav[0];
  useEffect(() => {
    document.title =
      (customer ? "Личный кабинет" : page.label) + " · Ата Энерго";
  }, [customer, page.label]);
  const navItems = links.map((n) => (
    <NavLink key={n.key} to={n.to} end={n.to === "/"} title={n.label}>
      <n.icon size={19} />
      <span>{n.label}</span>
    </NavLink>
  ));
  return (
    <div
      className={
        "workspace " +
        (compact ? "compact " : "") +
        (customer ? "customer" : "")
      }
    >
      <a className="skip-link" href="#main-content">
        К содержимому
      </a>
      {!customer && (
        <aside className="sidebar">
          <Link to="/" aria-label="Ата Энерго, обзор">
            <Brand />
          </Link>
          <nav aria-label="Главная навигация">{navItems}</nav>
          <button
            className="collapse"
            onClick={() => usePrefs.setState({ compact: !compact })}
            aria-label={compact ? "Развернуть меню" : "Свернуть меню"}
          >
            {compact ? (
              <PanelLeftOpen size={18} />
            ) : (
              <PanelLeftClose size={18} />
            )}
            <span>Свернуть меню</span>
          </button>
        </aside>
      )}
      <div className="maincol">
        <header className="topbar">
          {customer ? (
            <Brand />
          ) : (
            <>
              <button
                className="mobilemenu iconbtn"
                aria-label="Открыть меню"
                onClick={() => setMobile(true)}
              >
                <MenuIcon />
              </button>
              <span className="crumb">
                Рабочее пространство <i>/</i> <b>{page.label}</b>
              </span>
            </>
          )}
          <div className="top-actions">
            {!customer && (
              <button
                onClick={() => setPalette(true)}
                className="search-trigger"
                aria-label="Поиск по системе"
              >
                <Search size={16} />
                <span>Поиск</span>
                <kbd>Ctrl K</kbd>
              </button>
            )}
            <ThemePicker />
            <button
              className="iconbtn alarmbell"
              onClick={() => setBell(true)}
              aria-label="Оповещения"
            >
              <Bell size={19} />
              {alerts.data?.some((a) => !a.is_read) && <i />}
            </button>
            <Profile />
          </div>
        </header>
        {stream === "offline" && <OfflineBanner />}
        <main className="content" id="main-content">
          <Suspense fallback={<Loading />}>
            {customer ? (
              <Routes>
                {["/", "/readings", "/appeals"].map((p) => (
                  <Route path={p} key={p} element={<Portal />} />
                ))}
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            ) : (
              <Routes>
                <Route path="/" element={<Dashboard />} />
                {(["clients", "sites", "devices"] as const).map(
                  (s) =>
                    access.sections[s] && (
                      <Route
                        key={s}
                        path={"/" + s + "/:id?"}
                        element={<Inventory key={s} section={s} />}
                      />
                    ),
                )}
                {access.sections.alerts && (
                  <>
                    <Route path="/alerts" element={<Alerts />} />
                    <Route path="/alerts/:id" element={<AlertDetail />} />
                  </>
                )}
                {(["requests", "tasks"] as const).map(
                  (k) =>
                    access.sections[k] && (
                      <Route
                        key={k}
                        path={"/" + k}
                        element={<Work key={k} kind={k} />}
                      />
                    ),
                )}
                {access.sections.integrations && (
                  <Route path="/integrations" element={<Integrations />} />
                )}
                {access.sections.admin && (
                  <Route path="/admin" element={<Admin />} />
                )}
                <Route
                  path="*"
                  element={
                    <section className="card">
                      <h1>Раздел недоступен</h1>
                      <Link to="/">Вернуться к обзору</Link>
                    </section>
                  }
                />
              </Routes>
            )}
          </Suspense>
        </main>
      </div>
      {!customer && (
        <nav className="bottom-nav" aria-label="Быстрая навигация">
          {links
            .filter((n) =>
              ["overview", "clients", "alerts", "tasks"].includes(n.key),
            )
            .map((n) => (
              <NavLink key={n.key} to={n.to} end>
                <n.icon size={19} />
                {n.label}
              </NavLink>
            ))}
          <button onClick={() => setMobile(true)}>
            <MenuIcon size={19} />
            Меню
          </button>
        </nav>
      )}
      {palette && <Palette close={() => setPalette(false)} />}
      <Modal title="Разделы" open={mobile} onClose={() => setMobile(false)}>
        <nav className="mobile-links" onClick={() => setMobile(false)}>
          {navItems}
        </nav>
      </Modal>
      <Modal title="Оповещения" open={bell} onClose={() => setBell(false)}>
        {alerts.isPending ? (
          <Loading />
        ) : alerts.isError ? (
          <Failure retry={alerts.refetch} />
        ) : alerts.data?.length ? (
          alerts.data.slice(0, 8).map((a) => (
            <Link
              className="listing"
              key={a.id}
              to={customer ? "/readings" : "/devices/" + a.device}
              onClick={() => setBell(false)}
            >
              <TriangleAlert size={16} />
              {a.text}
            </Link>
          ))
        ) : (
          <Empty>Новых оповещений нет</Empty>
        )}
        {!customer && (
          <Link to="/alerts" onClick={() => setBell(false)} className="btn">
            Открыть журнал
          </Link>
        )}
      </Modal>
    </div>
  );
}
function TelegramNavigation() {
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    const button = telegram?.BackButton;
    if (!button || (location.pathname === "/" && !location.search)) return;
    const back = () => {
      if (!window.history.state?.idx) void navigate("/", { replace: true });
      else void navigate(-1);
    };
    button.onClick(back);
    button.show();
    return () => {
      button.offClick(back);
      button.hide();
    };
  }, [location.key, location.pathname, location.search, navigate]);
  return null;
}

function Root() {
  const token = useAuth((s) => !!s.access),
    theme = usePrefs((s) => s.theme);
  const [loginError, setLoginError] = useState("");
  const telegramAttempt = useRef(token);
  const [telegramPending, setTelegramPending] = useState(!!telegram && !token);
  useEffect(() => {
    if (!telegram || telegramAttempt.current) return;
    telegramAttempt.current = true;
    void loginTelegram(telegram.initData)
      .catch((error: unknown) =>
        setLoginError(
          error instanceof Error
            ? error.message
            : "Вход через Telegram недоступен.",
        ),
      )
      .finally(() => setTelegramPending(false));
  }, []);
  useEffect(() => {
    telegram?.ready();
    telegram?.expand();
    if (telegram) document.documentElement.dataset.telegram = "true";
    const updateTheme = () => {
      document.documentElement.dataset.theme = telegram
        ? telegram.colorScheme === "dark"
          ? "night"
          : "solar"
        : theme;
      const color = getComputedStyle(document.documentElement)
        .getPropertyValue("--bg")
        .trim();
      if (color)
        document
          .querySelector('meta[name="theme-color"]')
          ?.setAttribute("content", color);
    };
    updateTheme();
    telegram?.onEvent("themeChanged", updateTheme);
    return () => telegram?.offEvent("themeChanged", updateTheme);
  }, [theme]);
  const me = useList<Me>("/me", token),
    access = useList<Access>("/crm/access", token && !!me.data);
  if (!token) {
    if (telegramPending) {
      return <Loading />;
    }
    return (
      <>
        {loginError && (
          <p className="card session-error" role="alert">
            {loginError}
          </p>
        )}
        <Login />
      </>
    );
  }
  if (me.isError || access.isError)
    return (
      <div className="card session-error">
        <Failure
          retry={() => {
            void me.refetch();
            void access.refetch();
          }}
        />
        <button onClick={logout} className="btn">
          Вернуться ко входу
        </button>
      </div>
    );
  if (!me.data || !access.data) return <Loading />;
  return (
    <Session.Provider value={{ me: me.data, access: access.data }}>
      <TelegramNavigation />
      <Shell key={me.data.id} />
    </Session.Provider>
  );
}
createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={cache}>
    <BrowserRouter>
      <Root />
      <Toasts />
    </BrowserRouter>
  </QueryClientProvider>,
);
