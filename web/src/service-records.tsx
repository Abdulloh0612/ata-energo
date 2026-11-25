import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
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
  id: "ID", name: "Название", sku: "Артикул", qty: "Количество",
  unit: "Единица", cost: "Себестоимость", price: "Цена",
  buy: "Закупочная цена", sell: "Цена продажи", min_qty: "Минимальный остаток",
  active: "Активность", created: "Создано", part_id: "ID запчасти",
  order_id: "ID заказа", client_id: "ID клиента", user_id: "ID сотрудника",
  delta: "Изменение количества", reason: "Причина", note: "Примечание",
  cat: "Категория", brand: "Марка", status: "Статус",
  caption: "Подпись", file: "Файл", thumb: "Миниатюра",
  phone: "Телефон", first_name: "Имя", last_name: "Фамилия",
};

type ServiceRow = { id: string; data: Record<string, unknown> };

function Photo({ path, caption }: { path: string; caption: string }) {
  const [url, setUrl] = useState("");
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let imageUrl = "";
    void apiBlob(endpoint + "/photo?" + new URLSearchParams({ path }), controller.signal)
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
  if (error) return <Failure message="Не удалось загрузить фото." retry={() => {
    setError(false);
    setAttempt(attempt + 1);
  }} />;
  return url ? <img className="service-photo" src={url} alt={caption} /> : <Loading />;
}

function RecordDetails({ row, collection }: { row: ServiceRow; collection: string }) {
  const [open, setOpen] = useState(false);
  const { data } = row;
  const name = [data.name, data.caption, data.model, data.text, data.key, data.login,
    [data.last_name ?? data.lname, data.first_name ?? data.fname].filter(Boolean).join(" ")]
    .find((value) => typeof value === "string" && value.trim());
  const quantity = data.qty ?? data.delta;
  return (
    <details className="service-record" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>
        <span>{String(name ?? "Запись " + row.id)}</span>
        {quantity != null && <span>Количество {String(quantity)}</span>}
      </summary>
      {open && <>
        <dl className="service-fields">
          {Object.entries(data).map(([key, value]) => (
            <div key={key}>
              <dt>{fields[key] ?? key}</dt>
              <dd>{value == null || value === "" ? "—" :
                typeof value === "object" ? JSON.stringify(value) : String(value)}</dd>
            </div>
          ))}
        </dl>
        {collection === "photos" && typeof data.file === "string" && data.file &&
          <Photo path={data.file} caption={String(data.caption || "Фото из заказ-наряда")} />}
      </>}
    </details>
  );
}

export default function ServiceRecords() {
  const [params, setParams] = useSearchParams();
  const selected = params.get("collection") ?? "parts";
  const offset = Math.max(0, Number.parseInt(params.get("offset") ?? "", 10) || 0);
  const collections = useList<{ key: string; count: number }[]>(endpoint + "/collections");
  const rows = useList<ServiceRow[]>(endpoint + "/rows?" +
    new URLSearchParams({ collection: selected, offset: String(offset) }));
  const counts = new Map(collections.data?.map(({ key, count }) => [key, count]));
  const count = counts.get(selected) ?? 0;
  const known = groups.flatMap(({ prefix, tables }) => Object.keys(tables).map((key) => prefix + key));
  const extra = [...counts.keys()].filter((key) => !known.includes(key));
  const changePage = (next: number) => setParams({ collection: selected, offset: String(next) });
  return (
    <>
      <PageHead title="Склад и архив" />
      <section className="card service-archive" aria-label="Архивные записи">
        <label className="service-select">
          <span>Раздел</span>
          <select value={selected} onChange={(event) => setParams({ collection: event.target.value })}>
            {groups.map(({ label, prefix, tables }) => (
              <optgroup key={label} label={label}>
                {Object.entries(tables).map(([key, title]) => (
                  <option key={key} value={prefix + key}>
                    {title}{collections.data ? " (" + (counts.get(prefix + key) ?? 0) + ")" : ""}
                  </option>
                ))}
              </optgroup>
            ))}
            {extra.map((key) => <option key={key} value={key}>{key} ({counts.get(key)})</option>)}
          </select>
        </label>
        {collections.isError && <Failure retry={() => void collections.refetch()} />}
        {rows.isPending ? <Loading /> : rows.isError ? (
          <Failure retry={() => void rows.refetch()} />
        ) : rows.data.length ? (
          <div>
            {rows.data.map((row) => <RecordDetails key={selected + "/" + row.id} row={row} collection={selected} />)}
          </div>
        ) : <Empty>В этом разделе нет записей.</Empty>}
        {(offset > 0 || count > pageSize) && (
          <nav className="service-pagination" aria-label="Страницы записей">
            <button type="button" className="btn" disabled={offset === 0}
              onClick={() => changePage(Math.max(0, offset - pageSize))}>Назад</button>
            <span>Страница {Math.floor(offset / pageSize) + 1}</span>
            <button type="button" className="btn" disabled={offset + pageSize >= count}
              onClick={() => changePage(offset + pageSize)}>Далее</button>
          </nav>
        )}
      </section>
    </>
  );
}
