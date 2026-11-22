import { validateForm } from "./form";
import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table";
import {
  DndContext,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import * as Dialog from "@radix-ui/react-dialog";
import {
  ArrowDownUp,
  Bookmark,
  CalendarDays,
  Check,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  ClipboardList,
  Columns3,
  Download,
  GripVertical,
  List,
  Plus,
  Trash2,
  X,
} from "lucide-react";
import { api, useList, useSession, type Grant } from "./core";
import {
  PageHead,
  StatusBadge,
  Loading,
  Failure,
  FilterChip,
  ToggleChip,
  SearchField,
} from "./ui";
import "./filters.css";
import "./work.css";

type Kind = "requests" | "tasks";
type Status = "open" | "progress" | "closed";
type Priority = "low" | "normal" | "high";
type FormFields = {
  title: string;
  description: string;
  status: Status;
  priority: Priority;
  client_id: number | null;
  device_id: number | null;
  assignee_id: number | null;
  due_at: string | null;
};
export type WorkItem = FormFields & {
  id: number;
  kind: Kind;
  created_at: string;
  created_by: number | null;
  history?: {
    id: number;
    actor_id: number | null;
    action: string;
    created_at: string;
  }[];
};
type Client = { id: number; name: string };
type Device = { id: number; client: number; name: string; device_id: string };
type Staff = {
  id: number;
  first_name: string;
  last_name: string;
  username: string;
};
type Directory = { clients: Client[]; devices: Device[]; staff: Staff[] };
const statuses: Record<Status, string> = {
  open: "Открыта",
  progress: "В работе",
  closed: "Закрыта",
};
const priorities: Record<Priority, string> = {
  high: "Высокий",
  normal: "Обычный",
  low: "Низкий",
};
const names = {
  requests: {
    title: "Заявки",
    single: "Заявка",
    add: "Создать заявку",
    fresh: "Новая заявка",
    empty: "Заявок пока нет",
    remove: "Удалить заявку",
  },
  tasks: {
    title: "Задачи",
    single: "Задача",
    add: "Создать задачу",
    fresh: "Новая задача",
    empty: "Задач пока нет",
    remove: "Удалить задачу",
  },
};
const statusCodes = Object.keys(statuses) as Status[];
const priorityCodes = Object.keys(priorities) as Priority[];
const fullDate = new Intl.DateTimeFormat("ru-RU", {
  dateStyle: "medium",
  timeStyle: "short",
});
const shortDate = new Intl.DateTimeFormat("ru-RU", {
  day: "numeric",
  month: "short",
});

function staffName(id: number | null, staff: Staff[]) {
  if (id == null) return "Не назначен";
  const person = staff.find((item) => item.id === id);
  return person
    ? [person.first_name, person.last_name].filter(Boolean).join(" ") ||
        person.username
    : "Сотрудник №" + id;
}
function clientName(id: number | null, data: Directory) {
  return (
    data.clients.find((item) => item.id === id)?.name ||
    (id ? "Клиент №" + id : "—")
  );
}
function deviceName(id: number | null, data: Directory) {
  const device = data.devices.find((item) => item.id === id);
  return device?.name || device?.device_id || (id ? "Устройство №" + id : "—");
}
function localDate(value: Date) {
  return [
    String(value.getFullYear()),
    String(value.getMonth() + 1).padStart(2, "0"),
    String(value.getDate()).padStart(2, "0"),
  ].join("-");
}
function inputDate(value: string | null) {
  if (!value) return "";
  const date = new Date(value);
  return (
    localDate(date) +
    "T" +
    [date.getHours(), date.getMinutes()]
      .map((n) => String(n).padStart(2, "0"))
      .join(":")
  );
}
function nullableID(value: string | null) {
  const id = Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}
function fields(item: FormFields): FormFields {
  const {
    title,
    description,
    status,
    priority,
    client_id,
    device_id,
    assignee_id,
    due_at,
  } = item;
  return {
    title,
    description,
    status,
    priority,
    client_id,
    device_id,
    assignee_id,
    due_at,
  };
}
function errorMessage(error: unknown) {
  return error instanceof Error
    ? error.message
    : "Не удалось выполнить действие. Попробуйте ещё раз.";
}
function WorkDue({ item }: { item: WorkItem }) {
  if (!item.due_at) return <span className="work-muted">Без срока</span>;
  const date = new Date(item.due_at);
  const late = item.status !== "closed" && date.valueOf() < Date.now();
  return (
    <time
      dateTime={item.due_at}
      title={fullDate.format(date)}
      className={late ? "work-late" : undefined}
    >
      {late ? "Просрочено · " : ""}
      {shortDate.format(date)}
    </time>
  );
}
function Person({ id, data }: { id: number | null; data: Directory }) {
  const label = staffName(id, data.staff);
  return (
    <span className="work-person">
      <span className="work-avatar" aria-hidden="true">
        {id
          ? label
              .split(" ")
              .slice(0, 2)
              .map((s) => s[0])
              .join("")
              .toUpperCase()
          : "—"}
      </span>
      {label}
    </span>
  );
}

function exportCSV(records: WorkItem[], data: Directory, kind: Kind) {
  const rows = [
    [
      "Номер",
      "Название",
      "Статус",
      "Приоритет",
      "Клиент",
      "Оборудование",
      "Исполнитель",
      "Срок",
    ],
    ...records.map((item) => [
      String(item.id),
      item.title,
      statuses[item.status],
      priorities[item.priority],
      clientName(item.client_id, data),
      deviceName(item.device_id, data),
      staffName(item.assignee_id, data.staff),
      item.due_at ? fullDate.format(new Date(item.due_at)) : "",
    ]),
  ];
  const escape = (value: string) =>
    '"' +
    (/^[\s]*[=+@-]/.test(value) ? "'" : "") +
    value.replaceAll('"', '""') +
    '"';
  const url = URL.createObjectURL(
    new Blob(
      ["\uFEFF" + rows.map((row) => row.map(escape).join(";")).join("\r\n")],
      { type: "text/csv;charset=utf-8" },
    ),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = names[kind].title + "-" + localDate(new Date()) + ".csv";
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 2000);
}

type ViewProps = {
  rows: WorkItem[];
  directory: Directory;
  open: (id: number) => void;
};
function WorkCard({
  item,
  directory,
  open,
  drag,
}: {
  item: WorkItem;
  directory: Directory;
  open: ViewProps["open"];
  drag: boolean;
}) {
  const {
    attributes,
    listeners,
    setNodeRef,
    setActivatorNodeRef,
    isDragging,
    transform,
  } = useDraggable({ id: item.id, disabled: !drag });
  return (
    <article
      ref={setNodeRef}
      className={"work-card work-priority-" + item.priority}
      data-dragging={isDragging}
      style={
        transform
          ? {
              transform:
                "translate3d(" + transform.x + "px," + transform.y + "px,0)",
            }
          : undefined
      }
    >
      <div className="work-card-top">
        <span className="work-id">№ {item.id}</span>
        <span className="work-priority">{priorities[item.priority]}</span>
        {drag && (
          <button
            type="button"
            disabled={!drag}
            ref={setActivatorNodeRef}
            {...attributes}
            {...listeners}
            className="work-drag"
            aria-label={"Переместить «" + item.title + "»"}
          >
            <GripVertical size={15} />
          </button>
        )}
      </div>
      <button
        type="button"
        className="work-card-title"
        onClick={() => open(item.id)}
      >
        {item.title}
      </button>
      <div className="work-relations">
        {item.client_id && (
          <Link to={"/clients/" + item.client_id}>
            {clientName(item.client_id, directory)}
          </Link>
        )}
        {item.device_id && (
          <Link to={"/devices/" + item.device_id}>
            {deviceName(item.device_id, directory)}
          </Link>
        )}
      </div>
      <footer>
        <Person id={item.assignee_id} data={directory} />
        <WorkDue item={item} />
      </footer>
    </article>
  );
}
function WorkColumn({
  status,
  children,
  count,
  active,
}: {
  status: Status;
  count: number;
  active: boolean;
  children: ReactNode;
}) {
  const { isOver, setNodeRef } = useDroppable({ id: status });
  return (
    <section
      ref={setNodeRef}
      className={
        "work-column" +
        (isOver ? " work-column-target" : "") +
        (active ? " is-active" : "")
      }
    >
      <header>
        <StatusBadge status={status} />
        <span className="work-count">{count}</span>
      </header>
      <div className="work-stack">
        {children}
        {count === 0 && <p className="work-column-empty">Записей нет</p>}
      </div>
    </section>
  );
}
function Board({
  rows,
  directory,
  open,
  drag,
  move,
}: ViewProps & {
  drag: boolean;
  move: (item: WorkItem, status: Status) => void;
}) {
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 5 } }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 220, tolerance: 6 },
    }),
    useSensor(KeyboardSensor),
  );
  const [tab, setTab] = useState<Status>(statusCodes[0]);
  function dropped({ active, over }: DragEndEvent) {
    const status = over?.id as Status;
    const item = rows.find((row) => row.id === active.id);
    if (drag && statusCodes.includes(status) && item && item.status !== status)
      move(item, status);
  }
  return (
    <DndContext
      sensors={sensors}
      onDragEnd={dropped}
      accessibility={{
        screenReaderInstructions: {
          draggable:
            "Для смены статуса можно открыть карточку, выбрать «Изменить» и изменить поле «Статус».",
        },
      }}
    >
      <div className="work-tabs" role="tablist" aria-label="Статусы">
        {statusCodes.map((status) => (
          <button
            key={status}
            type="button"
            role="tab"
            aria-selected={tab === status}
            className={tab === status ? "is-active" : ""}
            onClick={() => setTab(status)}
          >
            {statuses[status]}
            <span>{rows.filter((item) => item.status === status).length}</span>
          </button>
        ))}
      </div>
      <div className="work-board">
        {statusCodes.map((status) => {
          const items = rows.filter((item) => item.status === status);
          return (
            <WorkColumn
              key={status}
              status={status}
              count={items.length}
              active={tab === status}
            >
              {items.map((item) => (
                <WorkCard key={item.id} {...{ item, directory, open, drag }} />
              ))}
            </WorkColumn>
          );
        })}
      </div>
    </DndContext>
  );
}
function WorkTable({ rows, directory, open }: ViewProps) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const columns: ColumnDef<WorkItem>[] = [
    {
      header: "Название",
      accessorKey: "title",
      cell: ({ row }) => (
        <button
          className="work-text-action"
          onClick={() => open(row.original.id)}
        >
          {row.original.title}
        </button>
      ),
    },
    {
      header: "Статус",
      accessorKey: "status",
      cell: ({ row }) => <StatusBadge status={row.original.status} />,
    },
    {
      header: "Приоритет",
      accessorKey: "priority",
      cell: ({ row }) => priorities[row.original.priority],
    },
    {
      header: "Клиент",
      id: "client",
      accessorFn: (row) => clientName(row.client_id, directory),
      cell: ({ row }) =>
        row.original.client_id ? (
          <Link to={"/clients/" + row.original.client_id}>
            {clientName(row.original.client_id, directory)}
          </Link>
        ) : (
          "—"
        ),
    },
    {
      header: "Исполнитель",
      id: "staff",
      accessorFn: (row) => staffName(row.assignee_id, directory.staff),
      cell: ({ row }) => (
        <Person id={row.original.assignee_id} data={directory} />
      ),
    },
    {
      header: "Срок",
      accessorKey: "due_at",
      cell: ({ row }) => <WorkDue item={row.original} />,
    },
  ];
  const table = useReactTable({
    data: rows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });
  return (
    <div className="work-table-scroll">
      <table>
        <thead>
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => (
                <th
                  key={header.id}
                  scope="col"
                  aria-sort={
                    header.column.getIsSorted() === "asc"
                      ? "ascending"
                      : header.column.getIsSorted() === "desc"
                        ? "descending"
                        : "none"
                  }
                >
                  <button onClick={header.column.getToggleSortingHandler()}>
                    {flexRender(
                      header.column.columnDef.header,
                      header.getContext(),
                    )}
                    <ArrowDownUp size={13} />
                  </button>
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.original.id}>
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
function WorkCalendar({ rows, open }: Pick<ViewProps, "rows" | "open">) {
  const [month, setMonth] = useState(
    () => new Date(new Date().getFullYear(), new Date().getMonth(), 1),
  );
  const start = (month.getDay() + 6) % 7;
  const count = new Date(
    month.getFullYear(),
    month.getMonth() + 1,
    0,
  ).getDate();
  const shift = (n: number) =>
    setMonth(new Date(month.getFullYear(), month.getMonth() + n, 1));
  const undated = rows.filter((row) => !row.due_at);
  return (
    <section className="work-calendar">
      <header>
        <h2>
          {month.toLocaleDateString("ru-RU", {
            month: "long",
            year: "numeric",
          })}
        </h2>
        <div>
          <button aria-label="Предыдущий месяц" onClick={() => shift(-1)}>
            <ChevronLeft size={17} />
          </button>
          <button
            onClick={() =>
              setMonth(
                new Date(new Date().getFullYear(), new Date().getMonth(), 1),
              )
            }
          >
            Сегодня
          </button>
          <button aria-label="Следующий месяц" onClick={() => shift(1)}>
            <ChevronRight size={17} />
          </button>
        </div>
      </header>
      <div className="work-calendar-scroll">
        <div className="work-calendar-grid">
          {["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"].map((day) => (
            <strong key={day}>{day}</strong>
          ))}
          {Array.from({ length: start + count }, (_, index) => {
            const day = index - start + 1;
            if (day <= 0)
              return <div className="work-calendar-blank" key={index} />;
            const date = localDate(
              new Date(month.getFullYear(), month.getMonth(), day),
            );
            const items = rows.filter(
              (row) => row.due_at && localDate(new Date(row.due_at)) === date,
            );
            return (
              <div key={index} className="work-calendar-day">
                <time
                  dateTime={date}
                  aria-current={
                    localDate(new Date()) === date ? "date" : undefined
                  }
                >
                  {day}
                </time>
                {items.map((row) => (
                  <button
                    key={row.id}
                    onClick={() => open(row.id)}
                    className={"work-priority-" + row.priority}
                    title={row.title}
                  >
                    {row.title}
                  </button>
                ))}
              </div>
            );
          })}
        </div>
      </div>
      {undated.length > 0 && (
        <details className="work-undated">
          <summary>Без срока · {undated.length}</summary>
          {undated.map((row) => (
            <button key={row.id} onClick={() => open(row.id)}>
              {row.title}
            </button>
          ))}
        </details>
      )}
    </section>
  );
}

function WorkDetail({
  item,
  directory,
}: {
  item: WorkItem;
  directory: Directory;
}) {
  const activityNames: Record<string, string> = {
    create: "Создание",
    update: "Изменение",
    delete: "Удаление",
  };
  return (
    <>
      <div className="work-tags">
        <StatusBadge status={item.status} />
        <span className={"work-priority work-priority-" + item.priority}>
          {priorities[item.priority]} приоритет
        </span>
      </div>
      {item.description && (
        <p className="work-description">{item.description}</p>
      )}
      <dl className="work-facts">
        <dt>Клиент</dt>
        <dd>
          {item.client_id ? (
            <Link to={"/clients/" + item.client_id}>
              {clientName(item.client_id, directory)}
            </Link>
          ) : (
            "—"
          )}
        </dd>
        <dt>Оборудование</dt>
        <dd>
          {item.device_id ? (
            <Link to={"/devices/" + item.device_id}>
              {deviceName(item.device_id, directory)}
            </Link>
          ) : (
            "—"
          )}
        </dd>
        <dt>Исполнитель</dt>
        <dd>
          <Person id={item.assignee_id} data={directory} />
        </dd>
        <dt>Срок</dt>
        <dd>
          {item.due_at ? (
            <time
              className={
                item.status !== "closed" &&
                new Date(item.due_at).valueOf() < Date.now()
                  ? "work-late"
                  : undefined
              }
              dateTime={item.due_at}
            >
              {fullDate.format(new Date(item.due_at))}
            </time>
          ) : (
            "Без срока"
          )}
        </dd>
      </dl>
      <h3>История изменений</h3>
      {item.history?.length ? (
        <ol className="work-history">
          {item.history.map((event) => (
            <li key={event.id}>
              <span>{activityNames[event.action] || event.action}</span>
              <small>
                {event.actor_id
                  ? staffName(event.actor_id, directory.staff)
                  : "Система"}{" "}
                ·{" "}
                <time dateTime={event.created_at}>
                  {fullDate.format(new Date(event.created_at))}
                </time>
              </small>
            </li>
          ))}
        </ol>
      ) : (
        <p className="work-muted">История пока пуста.</p>
      )}
    </>
  );
}
type DrawerProps = {
  kind: Kind;
  id: number | null;
  preset: { client: number | null; device: number | null };
  grants: Grant;
  role: string;
  directory: Directory;
  onDismiss: () => void;
  onSave: (id: number) => void;
};
function WorkDrawer({
  kind,
  id,
  preset,
  grants,
  role,
  directory,
  onDismiss,
  onSave,
}: DrawerProps) {
  const base = "/crm/" + kind;
  const detail = useList<WorkItem>(base + "/" + id, id !== null);
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<FormFields | null>(() =>
    !id
      ? {
          title: "",
          description: "",
          status: "open",
          priority: "normal",
          assignee_id: null,
          client_id: preset.client,
          device_id: preset.device,
          due_at: null,
        }
      : null,
  );
  const [dirty, setDirty] = useState(false);
  const [confirm, setConfirm] = useState<"discard" | "remove" | null>(null);
  const [fieldError, setFieldError] = useState("");
  const titleRef = useRef<HTMLInputElement>(null);
  const previousFocus = useRef(
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null,
  );
  const isNew = id === null;
  const name = names[kind];
  const edit = (data: Partial<FormFields>) => {
    setDraft((previous) => (previous ? { ...previous, ...data } : null));
    setDirty(true);
    setFieldError("");
  };
  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, [dirty]);
  const mutation = useMutation({
    mutationFn: (action: "save" | "remove") => {
      if (action === "remove")
        return api<WorkItem>(base + "/" + id, { method: "DELETE" });
      if (!draft) throw new Error("Выберите запись.");
      return api<WorkItem>(isNew ? base : base + "/" + id, {
        method: isNew ? "POST" : "PUT",
        body: JSON.stringify({ ...draft, title: draft.title.trim() }),
      });
    },
    onSuccess: (record, action) => {
      void queryClient.invalidateQueries({ queryKey: [base] });
      void queryClient.invalidateQueries({ queryKey: ["/crm/activity"] });
      setDirty(false);
      if (action === "remove") {
        onDismiss();
        return;
      }
      void queryClient.invalidateQueries({
        queryKey: [base + "/" + record.id],
      });
      setDraft(null);
      onSave(record.id);
    },
  });
  function requestClose() {
    if (mutation.isPending) return;
    if (dirty) setConfirm("discard");
    else onDismiss();
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft || mutation.isPending) return;
    if (!draft.title.trim()) {
      setFieldError("Введите название.");
      titleRef.current?.focus();
      return;
    }
    mutation.mutate("save");
  }
  const canRelate = role !== "technician";
  const title = isNew ? name.fresh : detail.data?.title || name.single;
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) requestClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="work-overlay" />
        <Dialog.Content
          className="work-drawer"
          aria-describedby={undefined}
          onCloseAutoFocus={(event) => {
            if (previousFocus.current?.isConnected) {
              event.preventDefault();
              previousFocus.current.focus();
            }
          }}
          onInteractOutside={(event) => {
            if (dirty || mutation.isPending) {
              event.preventDefault();
              requestClose();
            }
          }}
        >
          <header className="work-drawer-head">
            <div>
              {id && (
                <span className="work-id">
                  {name.single} № {id}
                </span>
              )}
              <Dialog.Title>{title}</Dialog.Title>
            </div>
            <button
              type="button"
              className="work-icon-button"
              aria-label="Закрыть карточку"
              onClick={requestClose}
            >
              <X size={19} />
            </button>
          </header>
          {confirm ? (
            <div className="work-confirm" role="alert">
              <h3>
                {confirm === "remove"
                  ? name.remove + "?"
                  : "Закрыть без сохранения?"}
              </h3>
              <p>
                {confirm === "remove"
                  ? "После удаления восстановить запись из CRM невозможно."
                  : "В карточке есть несохранённые изменения."}
              </p>
              <div>
                <button
                  type="button"
                  disabled={mutation.isPending}
                  onClick={() => {
                    setConfirm(null);
                    mutation.reset();
                  }}
                >
                  Отмена
                </button>
                <button
                  type="button"
                  className="work-danger-action"
                  disabled={mutation.isPending}
                  onClick={() =>
                    confirm === "remove"
                      ? mutation.mutate("remove")
                      : onDismiss()
                  }
                >
                  {confirm === "remove" ? "Удалить" : "Закрыть"}
                </button>
              </div>
              {mutation.isError && (
                <p role="alert">{errorMessage(mutation.error)}</p>
              )}
            </div>
          ) : draft ? (
            <form
              noValidate
              onSubmitCapture={validateForm}
              onSubmit={submit}
              className="work-form"
            >
              <fieldset disabled={mutation.isPending}>
                <label>
                  Название
                  <input
                    ref={titleRef}
                    value={draft.title}
                    maxLength={160}
                    required
                    autoFocus
                    aria-invalid={!!fieldError}
                    aria-describedby={
                      fieldError ? "work-title-error" : undefined
                    }
                    onChange={(e) => edit({ title: e.target.value })}
                  />
                </label>
                {fieldError && (
                  <p id="work-title-error" role="alert">
                    {fieldError}
                  </p>
                )}
                <label>
                  Описание
                  <textarea
                    className="resize-none"
                    value={draft.description}
                    rows={5}
                    maxLength={20000}
                    onChange={(e) => edit({ description: e.target.value })}
                  />
                </label>
                <div className="work-form-grid">
                  <label>
                    Статус
                    <select
                      value={draft.status}
                      onChange={(e) =>
                        edit({ status: e.target.value as Status })
                      }
                    >
                      {statusCodes.map((s) => (
                        <option key={s} value={s}>
                          {statuses[s]}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    Приоритет
                    <select
                      value={draft.priority}
                      onChange={(e) =>
                        edit({ priority: e.target.value as Priority })
                      }
                    >
                      {priorityCodes.map((s) => (
                        <option key={s} value={s}>
                          {priorities[s]}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                {canRelate && (
                  <>
                    <label>
                      Клиент
                      <select
                        value={draft.client_id ?? ""}
                        onChange={(e) =>
                          edit({
                            client_id: nullableID(e.target.value),
                            device_id: null,
                          })
                        }
                      >
                        <option value="">Без привязки</option>
                        {directory.clients.map((client) => (
                          <option key={client.id} value={client.id}>
                            {client.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Оборудование
                      <select
                        disabled={!draft.client_id}
                        value={draft.device_id ?? ""}
                        onChange={(e) =>
                          edit({ device_id: nullableID(e.target.value) })
                        }
                      >
                        <option value="">Без привязки</option>
                        {directory.devices
                          .filter((d) => d.client === draft.client_id)
                          .map((d) => (
                            <option key={d.id} value={d.id}>
                              {d.name || d.device_id} · {d.device_id}
                            </option>
                          ))}
                      </select>
                    </label>
                  </>
                )}
                {grants.assign && (
                  <label>
                    Исполнитель
                    <select
                      value={draft.assignee_id ?? ""}
                      onChange={(e) =>
                        edit({ assignee_id: nullableID(e.target.value) })
                      }
                    >
                      <option value="">Не назначен</option>
                      {directory.staff.map((person) => (
                        <option key={person.id} value={person.id}>
                          {staffName(person.id, directory.staff)}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                <label>
                  Срок
                  <input
                    type="datetime-local"
                    value={inputDate(draft.due_at)}
                    onChange={(e) => {
                      const value = e.target.value;
                      if (!value || Number.isFinite(Date.parse(value)))
                        edit({
                          due_at: value ? new Date(value).toISOString() : null,
                        });
                    }}
                  />
                </label>
              </fieldset>
              {mutation.isError && (
                <p className="work-error" role="alert">
                  {errorMessage(mutation.error)}
                </p>
              )}
              <footer>
                <button
                  type="submit"
                  className="work-primary"
                  disabled={mutation.isPending}
                >
                  <Check size={16} />
                  {mutation.isPending ? "Сохранение…" : "Сохранить"}
                </button>
                <button
                  type="button"
                  disabled={mutation.isPending}
                  onClick={requestClose}
                >
                  Отмена
                </button>
              </footer>
            </form>
          ) : detail.isError ? (
            <div className="work-fallback" role="alert">
              <p>{errorMessage(detail.error)}</p>
              <button onClick={() => detail.refetch()}>Повторить</button>
            </div>
          ) : !detail.data ? (
            <Loading />
          ) : (
            <div className="work-detail-body">
              <WorkDetail item={detail.data} directory={directory} />
              {(grants.edit || grants.delete) && (
                <footer>
                  {grants.edit && (
                    <button
                      className="work-primary"
                      onClick={() => {
                        mutation.reset();
                        setDraft(fields(detail.data!));
                      }}
                    >
                      Изменить
                    </button>
                  )}
                  {grants.delete && (
                    <button
                      className="work-delete"
                      onClick={() => setConfirm("remove")}
                    >
                      <Trash2 size={15} />
                      {name.remove}
                    </button>
                  )}
                </footer>
              )}
            </div>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

type URLPatch = Record<string, string | null>;
export function WorkPage({ kind }: { kind: Kind }) {
  const session = useSession();
  const me = session.me;
  const grants = session.access?.[kind];
  const [params, setParams] = useSearchParams();
  const path = "/crm/" + kind;
  const items = useList<WorkItem[]>(path, !!grants?.view);
  const clients = useList<Client[]>("/clients", !!grants?.view);
  const devices = useList<Device[]>("/devices", !!grants?.view);
  const staff = useList<Staff[]>("/crm/assignees", !!grants?.view);
  const directory = {
    clients: clients.data || [],
    devices: devices.data || [],
    staff: staff.data || [],
  };
  const queryClient = useQueryClient();
  const [message, setMessage] = useState("");
  const name = names[kind];
  const id = nullableID(params.get("id"));
  const creating = params.get("new") === "1" && grants?.create;
  const view = params.get("view") || "board";
  const term = params.get("q") || "";
  const search = term.trim().toLocaleLowerCase("ru");
  const status = params.get("status") || "";
  const mine = params.get("mine") === "1";
  const savedKey = "ata-work-filters-" + me?.id + "-" + kind;
  const update = (patch: URLPatch, replace = false) => {
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        Object.entries(patch).forEach(([key, value]) => {
          if (value) next.set(key, value);
          else next.delete(key);
        });
        return next;
      },
      { replace },
    );
  };
  const open = (itemID: number) => {
    setMessage("");
    update({ id: String(itemID), new: null });
  };
  const close = () =>
    update({ id: null, new: null, client: null, device: null }, true);
  const mutation = useMutation({
    mutationFn: async ({ item, next }: { item: WorkItem; next: Status }) => {
      const latest = await api<WorkItem>(path + "/" + item.id);
      return api<WorkItem>(path + "/" + item.id, {
        method: "PUT",
        body: JSON.stringify({ ...fields(latest), status: next }),
      });
    },
    onSuccess: (item) => {
      void queryClient.invalidateQueries({ queryKey: [path] });
      void queryClient.invalidateQueries({ queryKey: [path + "/" + item.id] });
      void queryClient.invalidateQueries({ queryKey: ["/crm/activity"] });
      setMessage("Статус обновлён");
    },
  });
  if (!grants?.view)
    return (
      <section className="work-page">
        <h1>{name.title}</h1>
        <p>Раздел недоступен.</p>
      </section>
    );
  const rows = (items.data || []).filter(
    (item) =>
      (!status ||
        (status === "active"
          ? item.status !== "closed"
          : status === item.status)) &&
      (!mine || item.assignee_id === me?.id) &&
      (!search ||
        [
          item.title,
          item.description,
          String(item.id),
          clientName(item.client_id, directory),
          deviceName(item.device_id, directory),
          staffName(item.assignee_id, directory.staff),
        ].some((value) => value.toLocaleLowerCase("ru").includes(search))),
  );
  function saveFilter() {
    try {
      localStorage.setItem(
        savedKey,
        JSON.stringify({ q: term, status, mine: mine ? "1" : "" }),
      );
      setMessage("Фильтр сохранён");
    } catch {
      setMessage("Браузер не разрешил сохранить фильтр");
    }
  }
  function restoreFilter() {
    try {
      const stored = localStorage.getItem(savedKey);
      if (!stored) {
        setMessage("Сохранённых фильтров пока нет");
        return;
      }
      const filter = JSON.parse(stored) as Record<string, string>;
      update({ q: filter.q, status: filter.status, mine: filter.mine });
      setMessage("Фильтр применён");
    } catch {
      setMessage("Не удалось прочитать сохранённый фильтр");
    }
  }
  const views = [
    { code: "board", label: "Канбан", icon: Columns3 },
    { code: "table", label: "Таблица", icon: List },
    ...(kind === "tasks"
      ? [{ code: "calendar", label: "Календарь", icon: CalendarDays }]
      : []),
  ];
  return (
    <section className="work-page">
      <PageHead
        title={name.title}
        action={
          grants.create && (
            <button
              className="work-primary"
              onClick={() =>
                update({ new: "1", id: null, client: null, device: null })
              }
            >
              <Plus size={16} />
              {name.add}
            </button>
          )
        }
      />
      <div className="work-tools">
        <div className="work-switch">
          {views.map(({ code, label, icon: Icon }) => (
            <button
              key={code}
              aria-pressed={view === code}
              onClick={() => update({ view: code })}
            >
              <Icon size={16} />
              {label}
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={() => exportCSV(rows, directory, kind)}
          disabled={!rows.length}
        >
          <Download size={15} />
          CSV
        </button>
      </div>
      <div className="filterbar">
        <div className="filterbar-row">
          <SearchField
            label="Поиск по работам"
            placeholder="Название, клиент или исполнитель"
            value={term}
            onChange={(v) => update({ q: v || null }, true)}
          />
          <div className="filter-actions">
            <button
              type="button"
              className="filter-tool"
              aria-label="Сохранить текущий фильтр"
              title="Сохранить текущий фильтр"
              onClick={saveFilter}
            >
              <Bookmark size={16} />
            </button>
          </div>
        </div>
        <div className="chips">
          {(term || status || mine) && (
            <button
              type="button"
              className="filter-reset"
              onClick={() => update({ q: null, status: null, mine: null })}
            >
              <X size={14} />
              Сбросить
            </button>
          )}
          <FilterChip
            title="Статус"
            allLabel="Все статусы"
            value={status}
            onChange={(v) => update({ status: v || null })}
            options={[
              { value: "active", label: "Незакрытые" },
              ...statusCodes.map((s) => ({ value: s, label: statuses[s] })),
            ]}
          />
          <ToggleChip
            on={mine}
            onChange={(on) => update({ mine: on ? "1" : null })}
          >
            Назначены мне
          </ToggleChip>
          <button type="button" className="chip" onClick={restoreFilter}>
            <Bookmark size={14} />
            Мой фильтр
          </button>
        </div>
      </div>
      {message && (
        <p className="work-notice" role="status">
          <CheckCircle2 size={15} />
          {message}
        </p>
      )}
      {mutation.isError && (
        <p className="work-error" role="alert">
          {errorMessage(mutation.error)}
        </p>
      )}
      {[clients, devices, staff].some((query) => query.isError) && (
        <Failure
          message="Не удалось загрузить справочник клиентов, устройств или сотрудников."
          retry={() => {
            for (const query of [clients, devices, staff])
              if (query.isError) void query.refetch();
          }}
        />
      )}
      {items.isError ? (
        <Failure message={errorMessage(items.error)} retry={items.refetch} />
      ) : items.isPending ? (
        <Loading />
      ) : !rows.length ? (
        <div className="work-fallback">
          <ClipboardList size={34} />
          <h2>{items.data?.length ? "Совпадений нет" : name.empty}</h2>
          {items.data?.length ? (
            <button
              onClick={() => update({ q: null, status: null, mine: null })}
            >
              Сбросить фильтры
            </button>
          ) : (
            grants.create && (
              <button onClick={() => update({ new: "1", id: null })}>
                {name.add}
              </button>
            )
          )}
        </div>
      ) : view === "table" ? (
        <WorkTable {...{ rows, directory, open }} />
      ) : view === "calendar" && kind === "tasks" ? (
        <WorkCalendar {...{ rows, open }} />
      ) : (
        <Board
          {...{ rows, directory, open }}
          drag={grants.edit && !mutation.isPending}
          move={(item, next) => mutation.mutate({ item, next })}
        />
      )}
      {(id || creating) && (
        <WorkDrawer
          key={kind + "-" + (id ?? "new")}
          {...{ kind, id, grants, directory }}
          role={me?.role || ""}
          preset={{
            client: nullableID(params.get("client")),
            device: nullableID(params.get("device")),
          }}
          onDismiss={close}
          onSave={(saved) =>
            update(
              { id: String(saved), new: null, client: null, device: null },
              true,
            )
          }
        />
      )}
    </section>
  );
}
