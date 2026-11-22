import { useEffect, useRef, useState, type ReactNode } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import * as Menu from "@radix-ui/react-dropdown-menu";
import {
  X,
  RadioTower,
  RotateCw,
  Layers3,
  Sun,
  Search,
  Check,
  ChevronDown,
} from "lucide-react";
import { Link } from "react-router-dom";
export function Brand() {
  return (
    <span className="brand">
      <span className="brand-mark">
        <Sun size={27} strokeWidth={2.7} />
      </span>
      <span>
        Ата <b>Энерго</b>
      </span>
    </span>
  );
}
export function Modal({
  title,
  open,
  onClose,
  children,
}: {
  title: string;
  open: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="veil" />
        <Dialog.Content className="dialog" aria-describedby={undefined}>
          <div className="row between">
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Close className="iconbtn" aria-label="Закрыть">
              <X size={19} />
            </Dialog.Close>
          </div>
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
export function Toasts() {
  const [message, set] = useState("");
  useEffect(() => {
    let id = 0;
    const cb = (e: Event) => {
      set((e as CustomEvent).detail);
      clearTimeout(id);
      id = window.setTimeout(() => set(""), 5000);
    };
    window.addEventListener("ata:toast", cb);
    return () => {
      window.removeEventListener("ata:toast", cb);
      clearTimeout(id);
    };
  }, []);
  return (
    <div role="status" className={message ? "toast" : ""}>
      {message}
    </div>
  );
}
export function Loading() {
  return (
    <div className="skeleton" role="status" aria-label="Загрузка">
      <i />
      <i />
      <i />
    </div>
  );
}
export function Empty({ children }: { children: ReactNode }) {
  return (
    <div className="empty">
      <Layers3 size={24} />
      <span>{children}</span>
    </div>
  );
}
export function Failure({
  retry,
  message = "Не удалось загрузить данные.",
}: {
  retry?: () => void;
  message?: string;
}) {
  return (
    <div role="alert" className="failure">
      {message}{" "}
      {retry && (
        <button className="btn" onClick={retry}>
          <RotateCw size={15} /> Повторить
        </button>
      )}
    </div>
  );
}
export function PageHead({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: string;
  action?: ReactNode;
}) {
  return (
    <div className="page-head">
      <div>
        <h1>{title}</h1>
        {subtitle && <p className="muted">{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}
export function CardHead({ title, href }: { title: string; href?: string }) {
  return (
    <header className="cardhead">
      <h2>{title}</h2>
      {href && (
        <Link className="small-link" to={href}>
          Все <span aria-hidden="true">›</span>
        </Link>
      )}
    </header>
  );
}
const statusWords: Record<string, string> = {
  online: "В сети",
  normal: "В сети",
  offline: "Без связи",
  warning: "Внимание",
  alarm: "Авария",
  fault: "Авария",
  error: "Ошибка",
  closed: "Закрыта",
  open: "Открыта",
  progress: "В работе",
  unknown: "Нет данных",
  nodata: "Нет данных",
};
export function StatusBadge({ status }: { status: string }) {
  const tone = ["online", "normal", "closed"].includes(status)
    ? "good"
    : ["error", "alarm", "fault", "offline"].includes(status)
      ? "bad"
      : ["progress", "warning"].includes(status)
        ? "warn"
        : "neutral";
  return (
    <span className={"badge " + tone}>
      <i />
      {statusWords[status] ?? status}
    </span>
  );
}
export function OfflineBanner() {
  return (
    <aside role="status" className="offline">
      <RadioTower size={16} />
      Связь с сервером потеряна. Показаны последние полученные данные.
    </aside>
  );
}

export function LiveValue({ value }: { value: unknown }) {
  const target =
    typeof value === "number" && Number.isFinite(value) ? value : null;
  const [shown, setShown] = useState(target);
  const current = useRef(target);
  useEffect(() => {
    const start = current.current;
    if (
      start === null ||
      target === null ||
      start === target ||
      matchMedia("(prefers-reduced-motion: reduce)").matches
    ) {
      current.current = target;
      setShown(target);
      return;
    }
    const at = performance.now();
    let handle = 0;
    function frame(now: number) {
      const t = Math.min(1, (now - at) / 250);
      const n = start! + (target! - start!) * (1 - (1 - t) ** 2);
      current.current = n;
      setShown(n);
      if (t < 1) handle = requestAnimationFrame(frame);
    }
    handle = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(handle);
  }, [target]);
  return (
    <>
      {target === null
        ? "—"
        : (shown ?? target).toLocaleString("ru-RU", {
            maximumFractionDigits: 1,
          })}
    </>
  );
}

export type ChipOption = { value: string; label: string };
/** Чип-фильтр: кнопка-«таблетка» с выпадающим списком вариантов. */
export function FilterChip({
  title,
  value,
  options,
  onChange,
  allLabel,
}: {
  title: string;
  value: string;
  options: ChipOption[];
  onChange: (value: string) => void;
  allLabel?: string;
}) {
  const current = options.find((o) => o.value === value);
  return (
    <Menu.Root>
      <Menu.Trigger
        className={"chip" + (current ? " is-set" : "")}
        aria-label={title + (current ? ": " + current.label : "")}
      >
        <span className="chip-title">{title}</span>
        {current && <b className="chip-value">{current.label}</b>}
        <ChevronDown size={14} aria-hidden="true" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content
          className="popover chip-menu"
          sideOffset={6}
          align="start"
        >
          <Menu.RadioGroup value={value} onValueChange={onChange}>
            <Menu.RadioItem value="">
              {allLabel ?? "Все"}
              <Menu.ItemIndicator>
                <Check size={14} />
              </Menu.ItemIndicator>
            </Menu.RadioItem>
            {options.map((o) => (
              <Menu.RadioItem key={o.value} value={o.value}>
                {o.label}
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
/** Чип-переключатель (вкл/выкл). */
export function ToggleChip({
  on,
  onChange,
  children,
}: {
  on: boolean;
  onChange: (on: boolean) => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      className={"chip" + (on ? " is-set" : "")}
      aria-pressed={on}
      onClick={() => onChange(!on)}
    >
      {on && <Check size={14} aria-hidden="true" />}
      {children}
    </button>
  );
}
/** Поле поиска для панелей фильтров. */
export function SearchField({
  value,
  onChange,
  placeholder,
  label,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  label: string;
}) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <label className="filter-search">
      <Search size={16} aria-hidden="true" />
      <input
        ref={input}
        type="search"
        aria-label={label}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {value && (
        <button
          type="button"
          aria-label="Очистить поиск"
          onClick={() => {
            onChange("");
            input.current?.focus();
          }}
        >
          <X size={15} />
        </button>
      )}
    </label>
  );
}
