import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
  Archive,
  ArrowLeftRight,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  FileText,
  Layers3,
  Package,
  Warehouse,
} from "lucide-react";
import { apiBlob, useList } from "./core";
import { Empty, Failure, Loading, PageHead } from "./ui";
import "./service-records.css";

const endpoint = "/crm/service-records";
const pageSize = 25;
const hybrid = {
  parts: "Запчасти и остатки",
  stock_moves: "Движения склада",
  orders: "Заказ-наряды",
  order_lines: "Позиции заказов",
  clients: "Клиенты",
  cars: "Автомобили",
  work_categories: "Категории работ",
  assignments: "Назначения мастеров",
  payments: "Платежи",
  photos: "Фотографии",
  events: "История изменений",
  users: "Сотрудники",
  sessions: "История сеансов",
  settings: "Настройки сервиса",
};
const crm = {
  stock: "Склад",
  inventory: "Инвентарь",
  orders: "Заказы",
  clients: "Клиенты",
  people: "Персонал",
  projects: "Проекты",
  monitors: "Мониторинг",
  users: "Пользователи",
  sessions: "История сеансов",
  meta: "Сведения о базе",
};
const groups = [
  { label: "Hybrid Doctor", prefix: "", tables: hybrid },
  { label: "Архив CRM", prefix: "crm/", tables: crm },
  { label: "Ранняя копия CRM", prefix: "crm-older/", tables: crm },
];
const fields: Record<string, string> = {
  id: "ID",
  name: "Название",
  sku: "Артикул",
  qty: "Количество",
  unit: "Единица",
  cost: "Себестоимость",
  price: "Цена",
  buy: "Закупочная цена",
  sell: "Цена продажи",
  min_qty: "Минимальный остаток",
  active: "Активность",
  created: "Создано",
  part_id: "ID запчасти",
  order_id: "ID заказа",
  client_id: "ID клиента",
  user_id: "ID сотрудника",
  delta: "Изменение количества",
  reason: "Причина",
  note: "Примечание",
  cat: "Категория",
  brand: "Марка",
  status: "Статус",
  caption: "Подпись",
  file: "Файл",
  thumb: "Миниатюра",
  phone: "Телефон",
  first_name: "Имя",
  last_name: "Фамилия",
  fname: "Имя",
  lname: "Фамилия",
  model: "Модель",
  login: "Логин",
  email: "Электронная почта",
  address: "Адрес",
  number: "Номер",
  num: "Номер",
  text: "Текст",
  key: "Ключ",
  value: "Значение",
  k: "Ключ",
  v: "Значение",
  updated: "Обновлено",
  created_at: "Создано",
  updated_at: "Обновлено",
  car_id: "ID автомобиля",
  amount: "Сумма",
  description: "Описание",
  comment: "Комментарий",
  date: "Дата",
  vin: "VIN",
  plate: "Госномер",
  year: "Год",
  title: "Название",
};

type ServiceRow = { id: string; data: Record<string, unknown> };

function Photo({ path, caption }: { path: string; caption: string }) {
  const [url, setUrl] = useState("");
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let imageUrl = "";
    void apiBlob(
      endpoint + "/photo?" + new URLSearchParams({ path }),
      controller.signal,
    )
      .then((blob) => {
        if (controller.signal.aborted) return;
        imageUrl = URL.createObjectURL(blob);
        setUrl(imageUrl);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(true);
      });
    return () => {
      controller.abort();
      if (imageUrl) URL.revokeObjectURL(imageUrl);
    };
  }, [path, attempt]);
  if (error)
    return (
      <Failure
        message="Не удалось загрузить фото."
        retry={() => {
          setError(false);
          setAttempt(attempt + 1);
        }}
      />
    );
  return url ? (
    <img className="service-photo" src={url} alt={caption} />
  ) : (
    <Loading />
  );
}

const warehouseKeys: readonly string[] = ["parts", "stock_moves"];
const defaultArchive = "orders";
const countFormat = new Intl.NumberFormat("ru-RU");
const valueFormat = new Intl.NumberFormat("ru-RU", {
  maximumSignificantDigits: 21,
});

function displayValue(value: unknown) {
  if (value == null || value === "") return "—";
  if (typeof value === "boolean") return value ? "Да" : "Нет";
  if (typeof value === "number") return valueFormat.format(value);
  if (typeof value === "object") return JSON.stringify(value, null, 2);
  return String(value);
}

function rowTitle({ data, id }: ServiceRow) {
  const person = [data.last_name ?? data.lname, data.first_name ?? data.fname]
    .filter(Boolean)
    .join(" ");
  const car = [data.brand, data.model].filter(Boolean).join(" ");
  return (
    [
      data.name,
      data.title,
      data.caption,
      person,
      car,
      data.login,
      data.key,
      data.k,
      data.text,
    ].find(
      (value): value is string =>
        typeof value === "string" && Boolean(value.trim()),
    ) ?? `Запись №${id}`
  );
}

function present(value: unknown) {
  return value != null && value !== "";
}

function RecordDetails({
  row,
  collection,
}: {
  row: ServiceRow;
  collection: string;
}) {
  const { data } = row;
  const highlights =
    collection === "parts"
      ? [
          ["qty", "Остаток"],
          ["min_qty", "Минимальный остаток"],
          ["price", "Цена"],
        ]
      : collection === "stock_moves"
        ? [
            ["delta", "Изменение остатка"],
            ["reason", "Причина"],
            ["created", "Дата операции"],
          ]
        : [
            ["status", "Статус"],
            ["amount", "Сумма"],
            ["order_id", "ID заказа"],
          ];
  const visible = highlights.filter(([key]) => present(data[key]));
  const entries = Object.entries(data);
  return (
    <div className="service-detail">
      {visible.length > 0 && (
        <dl className="service-highlights">
          {visible.map(([key, title]) => (
            <div key={key}>
              <dt>{title}</dt>
              <dd>
                {displayValue(data[key])}
                {key === "qty" && present(data.unit) && ` ${data.unit}`}
              </dd>
            </div>
          ))}
        </dl>
      )}
      {collection === "photos" &&
        typeof data.file === "string" &&
        data.file && (
          <Photo key={data.file} path={data.file} caption={rowTitle(row)} />
        )}
      {entries.length ? (
        <>
          <h3>Поля записи</h3>
          <dl className="service-fields">
            {entries.map(([key, value]) => (
              <div
                key={key}
                className={
                  typeof value === "object" && value !== null
                    ? "service-field-wide"
                    : undefined
                }
              >
                <dt>{fields[key] ?? key}</dt>
                <dd>{displayValue(value)}</dd>
              </div>
            ))}
          </dl>
        </>
      ) : (
        <Empty>У этой записи нет дополнительных полей.</Empty>
      )}
    </div>
  );
}

function RecordList({
  rows,
  collection,
}: {
  rows: ServiceRow[];
  collection: string;
}) {
  const [openId, setOpenId] = useState<string>();
  const Icon =
    collection === "parts"
      ? Package
      : collection === "stock_moves"
        ? ArrowLeftRight
        : FileText;
  return (
    <ul className="service-list">
      {rows.map((row, index) => {
        const { data } = row;
        const title = rowTitle(row);
        const open = row.id === openId;
        const metric =
          collection === "parts"
            ? data.qty
            : collection === "stock_moves"
              ? data.delta
              : data.status;
        const metricTitle =
          collection === "parts"
            ? "Остаток"
            : collection === "stock_moves"
              ? "Изменение"
              : "Статус";
        const reference = present(data.sku)
          ? `Артикул ${data.sku}`
          : `№ ${row.id}`;
        return (
          <li
            key={row.id}
            className={open ? "service-record is-open" : "service-record"}
          >
            <button
              type="button"
              className="service-record-trigger"
              aria-expanded={open}
              aria-controls={open ? `service-record-${index}` : undefined}
              onClick={() => setOpenId(open ? undefined : row.id)}
            >
              <span className="service-record-icon">
                <Icon size={19} aria-hidden="true" />
              </span>
              <span className="service-record-name">
                <strong>{title}</strong>
                <small>{reference}</small>
              </span>
              {present(metric) && (
                <span className="service-record-value">
                  <small>{metricTitle}</small>
                  <strong>
                    {displayValue(metric)}
                    {collection === "parts" &&
                      present(data.unit) &&
                      ` ${data.unit}`}
                  </strong>
                </span>
              )}
              <ChevronDown
                className="service-chevron"
                size={17}
                aria-hidden="true"
              />
            </button>
            {open && (
              <div id={`service-record-${index}`}>
                <RecordDetails row={row} collection={collection} />
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

function collectionLink(collection: string, offset = 0) {
  const params = new URLSearchParams({ collection });
  if (offset > 0) params.set("offset", String(offset));
  return `?${params}`;
}

export default function ServiceRecords() {
  const [params, setParams] = useSearchParams();
  const selected = params.get("collection") ?? "parts";
  const offset = Math.max(
    0,
    Number.parseInt(params.get("offset") ?? "", 10) || 0,
  );
  const panelTitle = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    const title = panelTitle.current;
    if (title && title.getBoundingClientRect().top < 0) {
      title.focus({ preventScroll: true });
      title.scrollIntoView({ block: "start" });
    }
  }, [selected, offset]);
  const isWarehouse = warehouseKeys.includes(selected);
  const collections = useList<{ key: string; count: number }[]>(
    endpoint + "/collections",
  );
  const rows = useList<ServiceRow[]>(
    endpoint +
      "/rows?" +
      new URLSearchParams({ collection: selected, offset: String(offset) }),
  );
  const counts = new Map(
    collections.data?.map(({ key, count }) => [key, count]),
  );
  const count = collections.data ? (counts.get(selected) ?? 0) : undefined;
  const archiveGroups = groups.map(({ label, prefix, tables }) => ({
    label,
    items: Object.entries(tables)
      .map(([key, title]) => ({ key: prefix + key, title }))
      .filter(({ key }) => !warehouseKeys.includes(key)),
  }));
  const known = new Set([
    ...warehouseKeys,
    ...archiveGroups.flatMap(({ items }) => items.map(({ key }) => key)),
  ]);
  const extra = [...new Set([...counts.keys(), selected])]
    .filter((key) => !known.has(key))
    .map((key) => ({ key, title: key }));
  if (extra.length)
    archiveGroups.push({ label: "Другие разделы", items: extra });
  const sections = isWarehouse
    ? [
        {
          label: "Hybrid Doctor",
          items: warehouseKeys.map((key) => ({
            key,
            title: hybrid[key as keyof typeof hybrid],
          })),
        },
      ]
    : archiveGroups;
  const section = sections.find(({ items }) =>
    items.some(({ key }) => key === selected),
  );
  const title =
    section?.items.find(({ key }) => key === selected)?.title ?? selected;
  const hasNext =
    count === undefined
      ? rows.data?.length === pageSize
      : offset + pageSize < count;
  const visibleCount = rows.data?.length ?? 0;
  const collectionTotal = countFormat.format(count ?? 0);
  const HeadingIcon = isWarehouse ? Warehouse : Archive;

  return (
    <>
      <PageHead title="Склад и архив" />
      <div className="service-workspace">
        <nav className="service-modes" aria-label="Склад и архив">
          <Link
            to={collectionLink("parts")}
            aria-current={isWarehouse ? "true" : undefined}
          >
            <Warehouse size={19} aria-hidden="true" /> Склад
          </Link>
          <Link
            to={collectionLink(defaultArchive)}
            aria-current={!isWarehouse ? "true" : undefined}
          >
            <Archive size={19} aria-hidden="true" /> Архив
          </Link>
        </nav>
        <div className="service-layout">
          <label className="card service-compact-index">
            <span>Раздел</span>
            <select
              value={selected}
              onChange={(event) =>
                setParams({ collection: event.target.value })
              }
            >
              {sections.map(({ label, items }) => (
                <optgroup key={label} label={label}>
                  {items.map(({ key, title }) => (
                    <option key={key} value={key}>
                      {title}
                      {collections.data
                        ? ` (${countFormat.format(counts.get(key) ?? 0)})`
                        : ""}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </label>
          <nav className="card service-index" aria-label="Разделы">
            {sections.map(({ label, items }) => (
              <div className="service-index-group" key={label}>
                <h2>{label}</h2>
                <ul>
                  {items.map(({ key, title }) => (
                    <li key={key}>
                      <Link
                        to={collectionLink(key)}
                        aria-current={selected === key ? "page" : undefined}
                      >
                        <span>{title}</span>
                        {collections.data && (
                          <small>
                            {countFormat.format(counts.get(key) ?? 0)}
                          </small>
                        )}
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </nav>
          <section
            className="card service-panel"
            aria-labelledby="service-section-title"
          >
            <header className="service-panel-head">
              <div className="service-panel-symbol">
                <HeadingIcon size={29} aria-hidden="true" />
              </div>
              <div className="service-panel-title">
                <div>{section?.label}</div>
                <h2 id="service-section-title" ref={panelTitle} tabIndex={-1}>
                  {title}
                </h2>
              </div>
              {count !== undefined && (
                <div className="service-total">
                  <span>{collectionTotal}</span>
                  <small>Всего записей</small>
                </div>
              )}
            </header>
            <div className="service-panel-body">
              {collections.isError && (
                <Failure
                  message="Не удалось загрузить число записей."
                  retry={() => void collections.refetch()}
                />
              )}
              {rows.isPending ? (
                <Loading />
              ) : rows.isError ? (
                <Failure
                  message="Не удалось загрузить записи."
                  retry={() => void rows.refetch()}
                />
              ) : visibleCount ? (
                <>
                  <div className="service-list-label">
                    <Layers3 size={15} aria-hidden="true" /> Записи раздела
                  </div>
                  <RecordList
                    key={JSON.stringify([selected, offset])}
                    rows={rows.data}
                    collection={selected}
                  />
                </>
              ) : (
                <Empty>
                  {offset > 0
                    ? "На этой странице нет записей."
                    : "В этом разделе пока нет записей."}
                </Empty>
              )}
              {(offset > 0 || hasNext) && (
                <nav
                  className="service-pagination"
                  aria-label="Страницы записей"
                >
                  {offset > 0 && (
                    <Link
                      className="btn"
                      to={collectionLink(
                        selected,
                        Math.max(0, offset - pageSize),
                      )}
                    >
                      <ChevronLeft size={16} aria-hidden="true" /> Назад
                    </Link>
                  )}
                  <span>
                    {visibleCount > 0
                      ? `${countFormat.format(offset + 1)}–${countFormat.format(offset + visibleCount)}${count === undefined ? "" : ` из ${collectionTotal}`}`
                      : `Страница ${Math.floor(offset / pageSize) + 1}`}
                  </span>
                  {hasNext && (
                    <Link
                      className="btn"
                      to={collectionLink(selected, offset + pageSize)}
                    >
                      Далее <ChevronRight size={16} aria-hidden="true" />
                    </Link>
                  )}
                </nav>
              )}
            </div>
          </section>
        </div>
      </div>
    </>
  );
}
