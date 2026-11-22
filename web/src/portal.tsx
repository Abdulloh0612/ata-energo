import { validateForm } from "./form";
import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { Sun, MessageSquarePlus, MapPin } from "lucide-react";
import {
  api,
  fmtNumber,
  useList,
  useLiveDevices,
  useSession,
  toast,
  type Site,
  type AlertItem,
} from "./core";
import {
  CardHead,
  Empty,
  Failure,
  Loading,
  Modal,
  PageHead,
  StatusBadge,
} from "./ui";
import { Generation } from "./dashboard";
interface Appeal {
  id: number;
  title: string;
  status: string;
  created_at: string;
}
export function Portal() {
  const { me } = useSession(),
    tab = useLocation().pathname;
  const sites = useList<Site[]>("/my/sites"),
    devices = useLiveDevices(),
    alerts = useList<AlertItem[]>("/alerts");
  const requests = useList<Appeal[]>("/my/requests", tab === "/appeals"),
    [open, setOpen] = useState(false);
  const add = useMutation({
    mutationFn: (f: FormData) =>
      api("/my/requests", {
        method: "POST",
        body: JSON.stringify(Object.fromEntries(f)),
      }),
    onSuccess: () => {
      setOpen(false);
      void requests.refetch();
      toast("Обращение отправлено");
    },
  });
  const online = (devices.data ?? []).filter(
    (d) =>
      d.device_type === "panel" &&
      d.status === "online" &&
      typeof d.last_data?.power === "number",
  );
  const problem = devices.data?.some(
    (d) => d.status === "offline" || d.last_data?.error,
  );
  return (
    <>
      <nav aria-label="Кабинет клиента" className="portal-tabs">
        {[
          ["/", "Мои станции"],
          ["/readings", "Показания"],
          ["/appeals", "Обращения"],
        ].map(([p, label]) => (
          <Link aria-current={tab === p ? "page" : undefined} key={p} to={p}>
            {label}
          </Link>
        ))}
      </nav>
      {tab === "/appeals" ? (
        <>
          <PageHead
            title="Мои обращения"
            action={
              me.client_id && (
                <button
                  className="btn primary"
                  onClick={() => {
                    add.reset();
                    setOpen(true);
                  }}
                >
                  <MessageSquarePlus size={17} />
                  Новое обращение
                </button>
              )
            }
          />
          <section className="card">
            {requests.isPending ? (
              <Loading />
            ) : requests.isError ? (
              <Failure retry={requests.refetch} />
            ) : requests.data?.length ? (
              requests.data.map((r) => (
                <div className="listing" key={r.id}>
                  <div>
                    <b>{r.title}</b>
                    <small>
                      {new Date(r.created_at).toLocaleDateString("ru-RU")}
                    </small>
                  </div>
                  <StatusBadge status={r.status} />
                </div>
              ))
            ) : (
              <Empty>У вас пока нет обращений</Empty>
            )}
          </section>
          <Modal
            title="Новое обращение"
            open={open}
            onClose={() => {
              if (!add.isPending) setOpen(false);
            }}
          >
            <form
              noValidate
              onSubmitCapture={validateForm}
              className="stack"
              onSubmit={(e) => {
                e.preventDefault();
                if (!add.isPending) add.mutate(new FormData(e.currentTarget));
              }}
            >
              <label>
                Тема
                <input name="title" maxLength={160} required />
              </label>
              <label>
                Что произошло?
                <textarea
                  className="resize-none"
                  name="description"
                  rows={5}
                  required
                  maxLength={5000}
                />
              </label>
              {add.isError && <Failure message={add.error.message} />}
              <button className="btn primary" disabled={add.isPending}>
                Отправить
              </button>
            </form>
          </Modal>
        </>
      ) : (
        <>
          <PageHead
            title={tab === "/readings" ? "Показания" : "Мои станции"}
            subtitle={me.client?.name}
          />
          <div className="portal-power">
            <Sun size={30} />
            <span>Сейчас вырабатывается</span>
            <strong>
              {online.length
                ? fmtNumber(
                    online.reduce((s, d) => s + Number(d.last_data.power), 0) /
                      1000,
                  )
                : "—"}{" "}
              <small>кВт</small>
            </strong>
            <p>
              {!devices.data?.length
                ? "Ожидаем данные оборудования"
                : problem
                  ? "Есть проблема, она зафиксирована системой"
                  : "Оборудование на связи"}
            </p>
          </div>
          {devices.isError && <Failure retry={devices.refetch} />}
          <Generation />
          {tab === "/readings" ? (
            <section className="card">
              <CardHead title="Последние показания" />
              {devices.isPending ? (
                <Loading />
              ) : devices.data?.length ? (
                devices.data.map((d) => (
                  <div className="listing" key={d.id}>
                    <span>{d.name || d.type_display}</span>
                    <strong>
                      {d.status === "online" &&
                      typeof d.last_data?.power === "number"
                        ? fmtNumber(d.last_data.power / 1000) + " кВт"
                        : "Нет показаний"}
                    </strong>
                  </div>
                ))
              ) : (
                <Empty>Показаний пока нет</Empty>
              )}
            </section>
          ) : (
            <section className="card">
              <CardHead title="Станции" />
              {sites.isPending ? (
                <Loading />
              ) : sites.isError ? (
                <Failure retry={sites.refetch} />
              ) : sites.data?.length ? (
                sites.data.map((s) => (
                  <div className="listing" key={s.id}>
                    <MapPin size={18} />
                    <div>
                      {s.name}
                      <small>{s.address}</small>
                    </div>
                    <StatusBadge status={s.status} />
                  </div>
                ))
              ) : (
                <Empty>У вашей учётной записи пока нет станций</Empty>
              )}
            </section>
          )}
          {alerts.data?.length ? (
            <section className="card">
              <CardHead title="Последние события" />
              {alerts.data.slice(0, 3).map((a) => (
                <div className="listing" key={a.id}>
                  <span>{a.text}</span>
                  <small>
                    {new Date(a.created_at).toLocaleDateString("ru-RU")}
                  </small>
                </div>
              ))}
            </section>
          ) : null}
        </>
      )}
    </>
  );
}
