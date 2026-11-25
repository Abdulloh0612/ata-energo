# -*- coding: utf-8 -*-
"""Главная, заказы, работы и запчасти в заказе, мастера, оплата, фото."""
import os
import re
import secrets
from datetime import datetime

from . import db, logic
from .web import (route, h, money, qty_fmt, to_int, to_float, clean, fdate, Redirect, HTTPError,
                  json_response, Response)
from .ui import page, form, field, select, status_chip, order_no, client_no, client_name, empty

ACTIVE = ("new", "work", "ready")


def norm_plate(p):
    return re.sub(r"[^0-9A-ZА-ЯЁ]", "", (p or "").upper())


def get_order(req, oid):
    o = req.con.execute(
        "SELECT o.*, c.last_name, c.first_name, c.phone AS cphone, c.id AS cid, "
        "car.plate, car.model, car.vin, car.year "
        "FROM orders o JOIN clients c ON c.id = o.client_id LEFT JOIN cars car ON car.id = o.car_id "
        "WHERE o.id = ?", (oid,)).fetchone()
    if not o:
        raise HTTPError(404, "Заказ не найден")
    return o


def staff(req):
    return req.con.execute("SELECT id, name, role FROM users WHERE active = 1 ORDER BY "
                           "CASE role WHEN 'diag' THEN 1 WHEN 'mech' THEN 2 WHEN 'hvb' THEN 3 "
                           "WHEN 'chief' THEN 4 ELSE 5 END, name").fetchall()


def wa_link(phone, text=""):
    digits = re.sub(r"\D", "", phone or "")
    if digits.startswith("0") and len(digits) == 10:
        digits = "996" + digits[1:]
    if len(digits) == 9:
        digits = "996" + digits
    if not digits:
        return ""
    from urllib.parse import quote
    return "https://wa.me/{}{}".format(digits, "?text=" + quote(text) if text else "")


def order_row(req, o, totals=None):
    car = " ".join(x for x in (o["model"], o["plate"]) if x) or "Авто не указано"
    right = ""
    if req.is_boss() and totals is not None:
        right = '<b>{}</b>'.format(money(totals["total"]))
        if totals["due"] > 0 and o["status"] in ("ready", "closed"):
            right += '<small class="red">долг {}</small>'.format(money(totals["due"]))
    return ('<a class="row" href="/orders/{id}"><div class="main"><b>{no}</b> {chip}'
            '<div class="sub">{car}</div><div class="sub muted">{client} · {date}</div></div>'
            '<div class="side">{right}</div></a>').format(
        id=o["id"], no=order_no(o["id"]), chip=status_chip(o["status"]), car=h(car),
        client=h(client_name(o)), date=fdate(o["created"]), right=right)


ORDER_LIST_SQL = ("SELECT o.*, c.last_name, c.first_name, car.plate, car.model FROM orders o "
                  "JOIN clients c ON c.id = o.client_id LEFT JOIN cars car ON car.id = o.car_id ")


# ================================ ГЛАВНАЯ ================================
@route("/")
def home(req):
    u = req.user
    parts = []
    pend = req.con.execute(
        "SELECT a.*, o.status AS ost, car.model, car.plate FROM assignments a JOIN orders o ON o.id = a.order_id "
        "LEFT JOIN cars car ON car.id = o.car_id WHERE a.user_id = ? AND a.status = 'pending' "
        "AND o.status != 'cancelled' ORDER BY a.id DESC", (u["id"],)).fetchall()
    if pend:
        cards = []
        for a in pend:
            what = "диагностику" if a["kind"] == "diag" else "ремонт"
            car = " ".join(x for x in (a["model"], a["plate"]) if x)
            back = '<input type="hidden" name="back" value="/">'
            btns = (form(req, "/assign/{}/confirm".format(a["id"]), back + '<button class="btn ok">Подтверждаю</button>') +
                    form(req, "/assign/{}/decline".format(a["id"]), back + '<button class="btn ghost">Не участвую</button>'))
            cards.append('<div class="card notice"><b>Вас назначили на {}</b><div><a href="/orders/{}">{} · {}</a></div>'
                         '<div class="btnrow">{}</div></div>'.format(what, a["order_id"], order_no(a["order_id"]), h(car), btns))
        parts.append('<h2>Ждут подтверждения</h2>' + "".join(cards))

    if req.is_boss():
        today = db.now()[:10]
        s = db.get_settings(req.con)
        pay = {r["wallet"]: r["s"] for r in req.con.execute(
            "SELECT wallet, SUM(amount) AS s FROM payments WHERE substr(created,1,10) = ? GROUP BY wallet", (today,))}
        cnt = {r["status"]: r["n"] for r in req.con.execute(
            "SELECT status, COUNT(*) AS n FROM orders WHERE status IN ('new','work','ready') GROUP BY status")}
        low = req.con.execute("SELECT COUNT(*) FROM parts WHERE active = 1 AND qty <= min_qty AND min_qty > 0").fetchone()[0]
        wallets = "".join('<div><span>{}</span><b>{}</b></div>'.format(h(s["wallets"][k]), money(pay.get(k, 0)))
                          for k in ("cash", "b1", "b2", "b3"))
        parts.append(
            '<a class="btn big main" href="/orders/new">+ Новый заказ</a>'
            '<div class="stats"><a href="/orders?st=new"><b>{}</b><span>Принято</span></a>'
            '<a href="/orders?st=work"><b>{}</b><span>В ремонте</span></a>'
            '<a href="/orders?st=ready"><b>{}</b><span>Готовы</span></a></div>'
            '<div class="card"><div class="cardhead"><b>Касса сегодня</b><b>{}</b></div><div class="wallets">{}</div></div>'
            .format(cnt.get("new", 0), cnt.get("work", 0), cnt.get("ready", 0), money(sum(pay.values())), wallets))
        if low:
            parts.append('<a class="card warn" href="/stock?low=1">Заканчиваются запчасти: {} поз. →</a>'.format(low))
    else:
        parts.append('<a class="btn big main" href="/orders/new">+ Новый заказ</a>')

    mine = req.con.execute(
        ORDER_LIST_SQL + "WHERE o.status IN ('new','work','ready') AND o.id IN "
        "(SELECT order_id FROM assignments WHERE user_id = ? AND status != 'declined') ORDER BY o.id DESC",
        (u["id"],)).fetchall()
    if mine:
        parts.append('<h2>Мои машины</h2><div class="list">{}</div>'.format("".join(order_row(req, o) for o in mine)))
    act = req.con.execute(ORDER_LIST_SQL + "WHERE o.status IN ('new','work','ready') ORDER BY o.id DESC LIMIT 30").fetchall()
    parts.append('<h2>Машины в сервисе</h2>' + ('<div class="list">{}</div>'.format(
        "".join(order_row(req, o, logic.order_totals(req.con, o["id"])) for o in act)) if act else empty("Сейчас машин нет.")))
    return page(req, "Здравствуйте, " + u["name"].split()[0], "".join(parts), nav="home")


@route("/api/pending")
def api_pending(req):
    from .ui import pending_count
    return json_response({"count": pending_count(req)})


# ================================ СПИСОК ЗАКАЗОВ ================================
@route("/orders")
def orders(req):
    st = req.query.get("st", "active")
    q = clean(req.query.get("q", ""), 60)
    where, args = [], []
    if st == "active":
        where.append("o.status IN ('new','work','ready')")
    elif st in db.STATUSES:
        where.append("o.status = ?")
        args.append(st)
    if q:
        nq = norm_plate(q)
        num = re.sub(r"\D", "", q)
        conds = ["c.last_name LIKE ?", "c.first_name LIKE ?", "c.phone LIKE ?", "car.model LIKE ?", "car.vin LIKE ?"]
        args += ["%" + q + "%"] * 5
        if nq:
            conds.append("REPLACE(REPLACE(UPPER(car.plate),' ',''),'-','') LIKE ?")
            args.append("%" + nq + "%")
        if num:
            conds.append("o.id = ?")
            args.append(int(num) - 1000 if int(num) > 1000 else -1)
        where.append("(" + " OR ".join(conds) + ")")
    sql = ORDER_LIST_SQL + ("WHERE " + " AND ".join(where) if where else "") + " ORDER BY o.id DESC LIMIT 200"
    rows = req.con.execute(sql, args).fetchall()
    chips = "".join('<a class="{}" href="/orders?st={}{}">{}</a>'.format(
        "on" if st == k else "", k, "&q=" + h(q) if q else "", v)
        for k, v in [("active", "В работе"), ("new", "Принят"), ("work", "В ремонте"), ("ready", "Готов"),
                     ("closed", "Выданы"), ("all", "Все")])
    body = ('<form class="search" method="get"><input type="hidden" name="st" value="{}">'
            '<input type="search" name="q" value="{}" placeholder="Номер авто, имя, телефон, № заказа"></form>'
            '<div class="chips">{}</div>').format(h(st), h(q), chips)
    body += '<div class="list">{}</div>'.format("".join(
        order_row(req, o, logic.order_totals(req.con, o["id"])) for o in rows)) if rows else empty("Заказов не найдено.")
    return page(req, "Заказы", body, nav="orders", actions='<a class="hbtn" href="/orders/new">+ Новый</a>')


# ================================ НОВЫЙ ЗАКАЗ ================================
@route("/api/plate")
def api_plate(req):
    nq = norm_plate(req.query.get("q", ""))
    if len(nq) < 3:
        return json_response({"found": False})
    r = req.con.execute(
        "SELECT car.*, c.last_name, c.first_name, c.phone FROM cars car JOIN clients c ON c.id = car.client_id "
        "WHERE REPLACE(REPLACE(UPPER(car.plate),' ',''),'-','') = ? ORDER BY car.id DESC LIMIT 1", (nq,)).fetchone()
    if not r:
        return json_response({"found": False})
    visits = req.con.execute("SELECT COUNT(*) FROM orders WHERE car_id = ?", (r["id"],)).fetchone()[0]
    return json_response({"found": True, "car_id": r["id"], "client": client_name(r), "client_no": client_no(r["client_id"]),
                          "last_name": r["last_name"], "first_name": r["first_name"], "phone": r["phone"],
                          "model": r["model"], "vin": r["vin"], "year": r["year"], "visits": visits})


@route("/orders/new", ("GET", "POST"))
def order_new(req):
    if req.method == "POST":
        plate = clean(req.form.get("plate"), 20).upper()
        car_id = to_int(req.form.get("car_id"), 0)
        car = None
        if car_id:
            car = req.con.execute("SELECT * FROM cars WHERE id = ?", (car_id,)).fetchone()
        if not car and plate:
            car = req.con.execute("SELECT * FROM cars WHERE REPLACE(REPLACE(UPPER(plate),' ',''),'-','') = ? "
                                  "ORDER BY id DESC LIMIT 1", (norm_plate(plate),)).fetchone()
        last, first, phone = req.f("last_name", 60), req.f("first_name", 60), req.f("phone", 30)
        model, vin, year = req.f("model", 80), req.f("vin", 30).upper(), req.f("year", 4)
        if not car and not (last or first):
            raise Redirect("/orders/new", "!Укажите хотя бы фамилию или имя клиента")
        with db.write_lock():
            req.con.execute("BEGIN IMMEDIATE")
            try:
                ts = db.now()
                if car:
                    cid = car["client_id"]
                    # дополняем то, что раньше не знали
                    req.con.execute("UPDATE clients SET phone = CASE WHEN phone = '' THEN ? ELSE phone END, "
                                    "last_name = CASE WHEN last_name = '' THEN ? ELSE last_name END, "
                                    "first_name = CASE WHEN first_name = '' THEN ? ELSE first_name END WHERE id = ?",
                                    (phone, last, first, cid))
                    req.con.execute("UPDATE cars SET model = CASE WHEN model = '' THEN ? ELSE model END, "
                                    "vin = CASE WHEN vin = '' THEN ? ELSE vin END, "
                                    "year = CASE WHEN year = '' THEN ? ELSE year END WHERE id = ?",
                                    (model, vin, year, car["id"]))
                    car_id = car["id"]
                else:
                    cid = req.con.execute("INSERT INTO clients(last_name, first_name, phone, created) VALUES (?, ?, ?, ?)",
                                          (last, first, phone, ts)).lastrowid
                    car_id = req.con.execute("INSERT INTO cars(client_id, plate, model, vin, year, created) "
                                             "VALUES (?, ?, ?, ?, ?, ?)", (cid, plate, model, vin, year, ts)).lastrowid
                oid = req.con.execute(
                    "INSERT INTO orders(client_id, car_id, status, mileage, complaint, created, created_by, share_token) "
                    "VALUES (?, ?, 'new', ?, ?, ?, ?, ?)",
                    (cid, car_id, req.f("mileage", 20), req.ftext("complaint", 2000), ts, req.user["id"],
                     secrets.token_urlsafe(12))).lastrowid
                logic.log(req.con, oid, req.user["id"], "Заказ создан")
                diag = to_int(req.form.get("diag_user"), 0)
                if diag and req.con.execute("SELECT 1 FROM users WHERE id = ? AND active = 1", (diag,)).fetchone():
                    req.con.execute("INSERT INTO assignments(order_id, user_id, kind, created) VALUES (?, ?, 'diag', ?)",
                                    (oid, diag, ts))
                    logic.log(req.con, oid, req.user["id"], "Назначен диагност")
                req.con.execute("COMMIT")
            except Exception:
                req.con.execute("ROLLBACK")
                raise
        raise Redirect("/orders/{}".format(oid), "Заказ {} создан. Сфотографируйте машину ↓".format(order_no(oid)))

    diag_opts = [("", "— не назначать —")] + [(u["id"], "{} ({})".format(u["name"], db.ROLES.get(u["role"], "")))
                                             for u in staff(req)]
    def_diag = next((u["id"] for u in staff(req) if u["role"] == "diag"), "")
    body = form(req, "/orders/new", (
        '<div class="card"><h3>Автомобиль</h3>'
        + field("Госномер", "plate", "", attrs='id="plate" autocomplete="off" autocapitalize="characters" required')
        + '<div id="plateInfo" class="hint-box" hidden></div><input type="hidden" name="car_id" id="car_id">'
        + field("Марка и модель", "model", "", attrs='id="model" placeholder="Toyota Prius 30"')
        + '<div class="two">' + field("VIN", "vin", "", attrs='id="vin" autocapitalize="characters"')
        + field("Год", "year", "", attrs='id="year" inputmode="numeric"') + '</div>'
        + field("Пробег, км", "mileage", "", attrs='inputmode="numeric"') + '</div>'
        '<div class="card"><h3>Клиент</h3><div class="two">'
        + field("Фамилия", "last_name", "", attrs='id="last_name"') + field("Имя", "first_name", "", attrs='id="first_name"')
        + '</div>' + field("Телефон (WhatsApp)", "phone", "", "tel", 'id="phone" placeholder="0700 123 456"') + '</div>'
        '<div class="card"><h3>С чем приехал</h3>'
        + field("Жалоба клиента", "complaint", "", "textarea", 'placeholder="Горит Check Hybrid, не заводится…"')
        + select("Диагност", "diag_user", diag_opts, def_diag) + '</div>'
        '<button class="btn big main">Создать заказ</button>'), attrs='id="newOrder"')
    return page(req, "Новый заказ", body, nav="orders", back="/orders")


# ================================ КАРТОЧКА ЗАКАЗА ================================
@route(r"/orders/(\d+)")
def order_view(req, oid):
    o = get_order(req, int(oid))
    oid = o["id"]
    boss = req.is_boss()
    s = db.get_settings(req.con)
    t = logic.order_totals(req.con, oid)
    closed = o["status"] in ("closed", "cancelled")
    me = req.user["id"]
    out = []

    # --- шапка ---
    car = " ".join(x for x in (o["model"], o["year"]) if x) or "Авто"
    phone = o["cphone"]
    wa = wa_link(phone)
    contact = ""
    if phone:
        contact = '<div class="btnrow"><a class="btn small" href="tel:{}">Позвонить</a>{}</div>'.format(
            h(re.sub(r"[^\d+]", "", phone)), '<a class="btn small wa" href="{}" target="_blank">WhatsApp</a>'.format(h(wa)) if wa else "")
    out.append(
        '<div class="card head"><div class="cardhead"><b class="big">{plate}</b>{chip}</div>'
        '<div>{car}{vin}{mil}</div><div class="sub"><a href="/clients/{cid}">{client} · {cno}</a>{ph}</div>'
        '<div class="sub muted">Принят {date}{cl}</div>{contact}</div>'.format(
            plate=h(o["plate"] or "без номера"), chip=status_chip(o["status"]), car=h(car),
            vin=' · VIN {}'.format(h(o["vin"])) if o["vin"] else "", mil=' · {} км'.format(h(o["mileage"])) if o["mileage"] else "",
            cid=o["cid"], client=h(client_name(o)), cno=client_no(o["cid"]), ph=" · " + h(phone) if phone else "",
            date=fdate(o["created"]), cl=", выдан " + fdate(o["closed_at"]) if o["closed_at"] else "", contact=contact))

    # --- статус ---
    btns = []
    def sbtn(to, label, cls="btn"):
        return form(req, "/orders/{}/status".format(oid), '<input type="hidden" name="to" value="{}"><button class="{}">{}</button>'.format(to, cls, label))
    if o["status"] == "new":
        btns.append(sbtn("work", "Начать ремонт →", "btn main"))
    if o["status"] == "work":
        btns.append(sbtn("ready", "Ремонт готов ✓", "btn main"))
    if o["status"] == "ready" and boss:
        if t["due"] > 0:
            btns.append(form(req, "/orders/{}/status".format(oid),
                             '<input type="hidden" name="to" value="closed"><label class="chk"><input type="checkbox" name="debt" value="1" required> '
                             'Выдать с долгом {}</label><button class="btn warnbtn">Выдать машину</button>'.format(money(t["due"]))))
        else:
            btns.append(sbtn("closed", "Выдать машину и закрыть", "btn main"))
    if o["status"] == "new" and t["total"] == 0 and boss:
        btns.append(sbtn("closed", "Только диагностика — закрыть", "btn"))
    if o["status"] in ("work", "ready"):
        btns.append(sbtn("new" if o["status"] == "work" else "work", "← Назад", "btn ghost"))
    if o["status"] == "closed" and boss:
        btns.append(sbtn("reopen", "Открыть заново", "btn ghost"))
    if o["status"] in ACTIVE and boss:
        btns.append(sbtn("cancelled", "Отменить заказ", "btn ghost danger"))
    if o["status"] == "cancelled" and boss:
        btns.append(sbtn("new", "Вернуть из отмены", "btn ghost"))
    if btns:
        out.append('<div class="btnrow wrap">{}</div>'.format("".join(btns)))

    # --- жалоба и диагноз ---
    out.append('<div class="card"><h3>Жалоба и диагноз</h3>' + form(req, "/orders/{}/info".format(oid),
               field("Жалоба клиента", "complaint", o["complaint"], "textarea")
               + field("Результат диагностики / что нашли", "diagnosis", o["diagnosis"], "textarea",
                       'rows="4" placeholder="Коды ошибок, замеры, состояние ВВБ…"')
               + field("Пробег, км", "mileage", o["mileage"], attrs='inputmode="numeric"')
               + '<button class="btn small">Сохранить</button>') + '</div>')

    # --- фото ---
    photos = req.con.execute("SELECT * FROM photos WHERE order_id = ? ORDER BY id", (oid,)).fetchall()
    gal = []
    for p in photos:
        tools = ""
        if boss:
            tools = form(req, "/photos/{}/share".format(p["id"]),
                         '<button class="mini{}" title="Показывать клиенту">{}</button>'.format(
                             " on" if p["shared"] else "", "👁 клиенту" if p["shared"] else "скрыто"))
        if boss or p["user_id"] == me:
            tools += form(req, "/photos/{}/delete".format(p["id"]), '<button class="mini danger" data-confirm="Удалить фото?">✕</button>')
        kind = {"client": "Клиент", "car": "Авто"}.get(p["kind"], "")
        gal.append('<figure><a href="/photo/{id}" target="_blank"><img src="/photo/{id}/t" loading="lazy" alt=""></a>'
                   '<figcaption>{kind}{cap}<small>{date}</small></figcaption><div class="ptools">{tools}</div></figure>'.format(
                       id=p["id"], kind='<b>{}</b> '.format(kind) if kind else "", cap=h(p["caption"]),
                       date=fdate(p["created"]), tools=tools))
    upl = form(req, "/orders/{}/photo".format(oid), (
        '<div class="two">' + select("Что на фото", "kind", [("work", "Ремонт / замер / номера"), ("car", "Автомобиль"), ("client", "Клиент")])
        + field("Подпись", "caption", "", attrs='placeholder="Напр.: ВВБ до ремонта, 201.6 В"') + '</div>'
        '<label class="btn main file">📷 Сделать / выбрать фото<input type="file" name="photo" accept="image/*" multiple hidden></label>'
        '<noscript><button class="btn">Загрузить</button></noscript><div class="upl-status"></div>'),
        cls="photo-form", attrs='enctype="multipart/form-data" data-upload')
    out.append('<div class="card"><h3>Фото <small class="muted">{}</small></h3><div class="gallery">{}</div>{}</div>'.format(
        len(photos) or "", "".join(gal) or '<p class="muted">Фото пока нет. Снимите машину при приёме, замеры и серийные номера.</p>', upl))

    # --- мастера ---
    asg = req.con.execute("SELECT a.*, u.name, u.role FROM assignments a JOIN users u ON u.id = a.user_id "
                          "WHERE a.order_id = ? ORDER BY a.kind DESC, a.id", (oid,)).fetchall()
    pay = logic.order_pay(req.con, oid, s)
    rows = []
    for a in asg:
        st = {"pending": '<span class="chip st-new">ждёт</span>', "confirmed": '<span class="chip st-closed">подтвердил</span>',
              "declined": '<span class="chip st-cancelled">отказался</span>'}[a["status"]]
        earn = ""
        if (boss or a["user_id"] == me) and a["status"] == "confirmed":
            v = pay["by_user"].get(a["user_id"], {})
            earn = '<small class="muted">{} {}</small>'.format(money(v.get("diag" if a["kind"] == "diag" else "work", 0)),
                                                             "" if o["status"] == "closed" else "(ожидается)")
        acts = ""
        if a["user_id"] == me and a["status"] != "confirmed" and not closed:
            acts += form(req, "/assign/{}/confirm".format(a["id"]), '<button class="mini ok">Подтверждаю</button>')
        if a["user_id"] == me and a["status"] == "pending" and not closed:
            acts += form(req, "/assign/{}/decline".format(a["id"]), '<button class="mini">Нет</button>')
        if boss and o["status"] != "closed":
            acts += form(req, "/assign/{}/delete".format(a["id"]), '<button class="mini danger" data-confirm="Убрать мастера из заказа?">✕</button>')
        share = ' · доля {}'.format(qty_fmt(a["share"])) if a["kind"] == "work" and boss else ""
        rows.append('<div class="arow"><div><b>{}</b> <small class="muted">{}{}</small> {}<div>{}</div></div><div class="ptools">{}</div></div>'.format(
            h(a["name"]), "диагност" if a["kind"] == "diag" else db.ROLES.get(a["role"], ""), share, st, earn, acts))
    asg_html = "".join(rows) or '<p class="muted">Мастера ещё не назначены.</p>'
    if boss and o["status"] != "closed":
        opts = "".join('<label class="pick"><input type="checkbox" name="u" value="{id}"> {name} <small class="muted">{role}</small>'
                       '<input type="text" name="share_{id}" value="1" inputmode="decimal" title="Доля" class="share"></label>'.format(
                           id=u["id"], name=h(u["name"]), role=db.ROLES.get(u["role"], "")) for u in staff(req))
        dopts = [("", "— выбрать —")] + [(u["id"], u["name"]) for u in staff(req)]
        asg_html += ('<details class="more"><summary>Назначить мастеров</summary>' + form(req, "/orders/{}/assign".format(oid),
                     '<p class="hint">Отметьте, кто работал. Процент с работ делится по долям (1 и 1 — поровну, 2 и 1 — две трети и треть). '
                     'Мастер получит уведомление и должен подтвердить участие.</p>' + opts
                     + '<input type="hidden" name="kind" value="work"><button class="btn small main">Назначить</button>')
                     + form(req, "/orders/{}/assign".format(oid), select("Диагност (отдельный процент)", "diag", dopts)
                            + '<input type="hidden" name="kind" value="diag"><button class="btn small">Назначить диагноста</button>')
                     + '</details>')
    if boss and (pay["pool"] or pay["diag_pool"]):
        asg_html += '<p class="hint">Фонд мастеров: {} · диагност: {}{}</p>'.format(
            money(pay["pool"]), money(pay["diag_pool"]),
            ' · <span class="red">не распределено {}</span>'.format(money(pay["unassigned"])) if pay["unassigned"] >= 1 else "")
    out.append('<div class="card"><h3>Мастера</h3>{}</div>'.format(asg_html))

    # --- работы и запчасти ---
    lines = req.con.execute("SELECT l.*, c.name AS cname FROM order_lines l LEFT JOIN work_categories c ON c.id = l.category_id "
                            "WHERE l.order_id = ? ORDER BY l.kind DESC, l.id", (oid,)).fetchall()
    def line_html(ln):
        price = ""
        if boss:
            price = '<div class="side"><b>{}</b><small class="muted">{} × {}</small></div>'.format(
                money(ln["qty"] * ln["price"]), qty_fmt(ln["qty"]), money(ln["price"]))
        else:
            price = '<div class="side"><small class="muted">{} шт</small></div>'.format(qty_fmt(ln["qty"])) if ln["kind"] == "part" else ""
        dele = ""
        if not closed and (boss or ln["created_by"] == me):
            dele = form(req, "/lines/{}/delete".format(ln["id"]), '<button class="mini danger" data-confirm="Удалить строку?">✕</button>')
        sub = h(ln["cname"] or "") if ln["kind"] == "work" else ("со склада" if ln["part_id"] else "не со склада")
        return '<div class="lrow"><div class="main">{}<small class="muted">{}</small></div>{}{}</div>'.format(
            h(ln["name"]), sub, price, dele)
    works = [ln for ln in lines if ln["kind"] == "work"]
    parts = [ln for ln in lines if ln["kind"] == "part"]
    cats = req.con.execute("SELECT id, name FROM work_categories WHERE active = 1 ORDER BY sort, id").fetchall()
    add_work = add_part = ""
    if not closed:
        price_f = field("Цена, сом", "price", "", attrs='inputmode="numeric"') if boss else ""
        add_work = '<details class="more"><summary>+ Добавить работу</summary>' + form(req, "/orders/{}/lines".format(oid),
            '<input type="hidden" name="kind" value="work">' + field("Работа", "name", "", attrs='required list="worklist" placeholder="Замена модуля ВВБ"')
            + '<div class="two">' + select("Вид работы", "category", [(c["id"], c["name"]) for c in cats]) + price_f + '</div>'
            + '<button class="btn small main">Добавить</button>') + '</details>'
        plist = req.con.execute("SELECT id, sku, name, qty, price, unit FROM parts WHERE active = 1 ORDER BY name").fetchall()
        popts = "".join('<option value="{}" data-price="{}">{}{} · ост. {} {}</option>'.format(
            p["id"], p["price"], h(p["sku"] + " — ") if p["sku"] else "", h(p["name"]), qty_fmt(p["qty"]), h(p["unit"])) for p in plist)
        add_part = '<details class="more"><summary>+ Добавить запчасть</summary>' + form(req, "/orders/{}/lines".format(oid),
            '<input type="hidden" name="kind" value="part">'
            '<label class="f"><span>Со склада</span><input type="search" class="pfilter" placeholder="Поиск по складу…">'
            '<select name="part_id" class="psel"><option value="">— не со склада —</option>{}</select></label>'.format(popts)
            + field("Или название (если не со склада)", "name", "")
            + '<div class="two">' + field("Количество", "qty", "1", attrs='inputmode="decimal"')
            + (field("Цена за шт, сом", "price", "", attrs='inputmode="numeric" placeholder="как на складе"') if boss else "")
            + '</div><button class="btn small main">Добавить</button>') + '</details>'
    wl = req.con.execute("SELECT DISTINCT name FROM order_lines WHERE kind = 'work' ORDER BY id DESC LIMIT 300").fetchall()
    datalist = '<datalist id="worklist">{}</datalist>'.format("".join('<option value="{}">'.format(h(r["name"])) for r in wl))
    tot = ""
    if boss:
        tot = ('<div class="totals"><div><span>Работы</span><b>{}</b></div><div><span>Запчасти</span><b>{}</b></div>'
               '<div class="grand"><span>Итого</span><b>{}</b></div></div>').format(money(t["works"]), money(t["parts"]), money(t["total"]))
    out.append('<div class="card"><h3>Работы</h3>{}{}<h3 class="mt">Запчасти</h3>{}{}{}{}</div>'.format(
        "".join(line_html(x) for x in works) or '<p class="muted">Работ пока нет.</p>', add_work,
        "".join(line_html(x) for x in parts) or '<p class="muted">Запчастей пока нет.</p>', add_part, tot, datalist))

    # --- оплата ---
    if boss:
        pays = req.con.execute("SELECT p.*, u.name FROM payments p LEFT JOIN users u ON u.id = p.user_id "
                               "WHERE p.order_id = ? ORDER BY p.id", (oid,)).fetchall()
        prow = "".join('<div class="lrow"><div class="main">{}<small class="muted">{} · {}</small></div><div class="side"><b>{}</b></div>{}</div>'.format(
            h(s["wallets"].get(p["wallet"], p["wallet"])), fdate(p["created"]), h(p["name"] or ""), money(p["amount"]),
            form(req, "/payments/{}/delete".format(p["id"]), '<button class="mini danger" data-confirm="Удалить оплату?">✕</button>')
            if o["status"] != "closed" else "") for p in pays)
        wbtns = "".join('<label class="wbtn"><input type="radio" name="wallet" value="{}"{} required><span>{}</span></label>'.format(
            k, " checked" if k == "cash" else "", h(s["wallets"][k])) for k in ("cash", "b1", "b2", "b3"))
        payform = ""
        if o["status"] != "cancelled" and (t["due"] > 0 or o["status"] != "closed"):
            payform = form(req, "/orders/{}/pay".format(oid),
                           field("Сумма, сом", "amount", t["due"] if t["due"] > 0 else "", attrs='inputmode="numeric" required')
                           + '<div class="wallet-pick">{}</div><button class="btn main">Принять оплату</button>'.format(wbtns))
            if t["due"] <= 0 and t["total"] > 0:
                payform = '<details class="more"><summary>+ Ещё оплата</summary>{}</details>'.format(payform)
        status_line = ('<div class="totals"><div><span>Оплачено</span><b>{}</b></div><div class="grand"><span>{}</span><b class="{}">{}</b></div></div>'
                       .format(money(t["paid"]), "Осталось" if t["due"] >= 0 else "Переплата",
                               "red" if t["due"] > 0 else "", money(abs(t["due"]))))
        out.append('<div class="card"><h3>Оплата</h3>{}{}{}</div>'.format(prow, status_line, payform))

    # --- гарантия ---
    if o["status"] in ("ready", "closed"):
        out.append('<a class="card link" href="/orders/{}/warranty">🧾 Гарантийный талон и отправка клиенту →</a>'.format(oid))

    # --- история ---
    ev = req.con.execute("SELECT e.*, u.name FROM events e LEFT JOIN users u ON u.id = e.user_id WHERE e.order_id = ? "
                         "ORDER BY e.id DESC LIMIT 100", (oid,)).fetchall()
    out.append('<details class="card more"><summary>История заказа</summary>{}</details>'.format(
        "".join('<div class="ev"><small class="muted">{} · {}</small> {}</div>'.format(fdate(e["created"]), h(e["name"] or ""), h(e["text"])) for e in ev)))
    return page(req, "Заказ " + order_no(oid), "".join(out), nav="orders", back="/orders")


def editable(req, o):
    if o["status"] in ("closed", "cancelled"):
        raise Redirect("/orders/{}".format(o["id"]), "!Заказ закрыт. Чтобы менять — откройте его заново.")


@route(r"/orders/(\d+)/info", ("POST",))
def order_info(req, oid):
    o = get_order(req, int(oid))
    with db.write_lock():
        req.con.execute("UPDATE orders SET complaint = ?, diagnosis = ?, mileage = ? WHERE id = ?",
                        (req.ftext("complaint", 2000), req.ftext("diagnosis", 5000), req.f("mileage", 20), o["id"]))
        logic.log(req.con, o["id"], req.user["id"], "Изменены жалоба/диагноз")
    raise Redirect("/orders/{}".format(o["id"]), "Сохранено")


@route(r"/orders/(\d+)/status", ("POST",))
def order_status(req, oid):
    o = get_order(req, int(oid))
    to = req.form.get("to")
    cur = o["status"]
    boss = req.is_boss()
    allowed = {("new", "work"), ("work", "ready"), ("work", "new"), ("ready", "work")}
    boss_only = {("ready", "closed"), ("new", "closed"), ("closed", "reopen"), ("new", "cancelled"),
                 ("work", "cancelled"), ("ready", "cancelled"), ("cancelled", "new")}
    if (cur, to) not in allowed and not ((cur, to) in boss_only and boss):
        raise Redirect("/orders/{}".format(o["id"]), "!Так изменить статус нельзя")
    t = logic.order_totals(req.con, o["id"])
    if to == "closed" and t["due"] > 0 and not req.form.get("debt"):
        raise Redirect("/orders/{}".format(o["id"]), "!Заказ не оплачен полностью")
    with db.write_lock():
        req.con.execute("BEGIN IMMEDIATE")
        try:
            if to == "closed":
                logic.close_order(req.con, o, req.user["id"])
                req.con.execute("UPDATE orders SET debt_ok = ? WHERE id = ?", (1 if t["due"] > 0 else 0, o["id"]))
                msg = "Машина выдана, заказ закрыт" + (" с долгом {}".format(money(t["due"])) if t["due"] > 0 else "")
            elif to == "reopen":
                logic.reopen_order(req.con, o, req.user["id"])
                msg = "Заказ открыт заново, запчасти возвращены на склад"
            else:
                req.con.execute("UPDATE orders SET status = ? WHERE id = ?", (to, o["id"]))
                msg = "Статус: " + db.STATUSES[to]
            logic.log(req.con, o["id"], req.user["id"], msg)
            req.con.execute("COMMIT")
        except Exception:
            req.con.execute("ROLLBACK")
            raise
    raise Redirect("/orders/{}".format(o["id"]), msg)


@route(r"/orders/(\d+)/lines", ("POST",))
def line_add(req, oid):
    o = get_order(req, int(oid))
    editable(req, o)
    kind = req.form.get("kind")
    qty = to_float(req.form.get("qty", 1), 1) or 1
    if qty <= 0 or qty > 10000:
        raise Redirect("/orders/{}".format(o["id"]), "!Неверное количество")
    price_raw = req.form.get("price", "")
    with db.write_lock():
        if kind == "work":
            name = req.f("name", 200)
            if not name:
                raise Redirect("/orders/{}".format(o["id"]), "!Укажите работу")
            cat = to_int(req.form.get("category"), 0) or None
            price = to_int(price_raw, 0) if req.is_boss() else 0
            req.con.execute("INSERT INTO order_lines(order_id, kind, name, qty, price, category_id, created, created_by) "
                            "VALUES (?, 'work', ?, ?, ?, ?, ?, ?)", (o["id"], name, qty, max(price, 0), cat, db.now(), req.user["id"]))
            logic.log(req.con, o["id"], req.user["id"], "Добавлена работа: {}".format(name))
        elif kind == "part":
            pid = to_int(req.form.get("part_id"), 0)
            p = req.con.execute("SELECT * FROM parts WHERE id = ?", (pid,)).fetchone() if pid else None
            name = p["name"] if p else req.f("name", 200)
            if not name:
                raise Redirect("/orders/{}".format(o["id"]), "!Выберите запчасть со склада или впишите название")
            price = to_int(price_raw, -1) if (req.is_boss() and str(price_raw).strip()) else -1
            if price < 0:
                price = p["price"] if p else 0
            req.con.execute("INSERT INTO order_lines(order_id, kind, name, qty, price, part_id, cost, created, created_by) "
                            "VALUES (?, 'part', ?, ?, ?, ?, ?, ?, ?)",
                            (o["id"], name, qty, price, p["id"] if p else None, p["cost"] if p else 0, db.now(), req.user["id"]))
            logic.log(req.con, o["id"], req.user["id"], "Добавлена запчасть: {} × {}".format(name, qty_fmt(qty)))
            if p and p["qty"] < qty:
                raise Redirect("/orders/{}".format(o["id"]), "!Добавлено, но на складе только {} {}".format(qty_fmt(p["qty"]), p["unit"]))
        else:
            raise HTTPError(400, "Неверный запрос")
    raise Redirect("/orders/{}".format(o["id"]), "Добавлено")


@route(r"/lines/(\d+)/delete", ("POST",))
def line_delete(req, lid):
    ln = req.con.execute("SELECT * FROM order_lines WHERE id = ?", (int(lid),)).fetchone()
    if not ln:
        raise HTTPError(404, "Не найдено")
    o = get_order(req, ln["order_id"])
    editable(req, o)
    if not (req.is_boss() or ln["created_by"] == req.user["id"]):
        raise HTTPError(403, "Удалить может главный или тот, кто добавил")
    with db.write_lock():
        req.con.execute("DELETE FROM order_lines WHERE id = ?", (ln["id"],))
        logic.log(req.con, o["id"], req.user["id"], "Удалено: {}".format(ln["name"]))
    raise Redirect("/orders/{}".format(o["id"]), "Удалено")


# ================================ МАСТЕРА ================================
@route(r"/orders/(\d+)/assign", ("POST",))
def assign(req, oid):
    req.need_boss()
    o = get_order(req, int(oid))
    if o["status"] == "closed":
        raise Redirect("/orders/{}".format(o["id"]), "!Заказ закрыт")
    kind = "diag" if req.form.get("kind") == "diag" else "work"
    ids = [to_int(x) for x in (req.flist("u") if kind == "work" else [req.form.get("diag", "")]) if to_int(x)]
    if not ids:
        raise Redirect("/orders/{}".format(o["id"]), "!Никто не выбран")
    names = []
    with db.write_lock():
        for uid in ids:
            u = req.con.execute("SELECT * FROM users WHERE id = ? AND active = 1", (uid,)).fetchone()
            if not u:
                continue
            share = to_float(req.form.get("share_{}".format(uid), 1), 1) if kind == "work" else 1
            share = min(max(share, 0.01), 100)
            ex = req.con.execute("SELECT * FROM assignments WHERE order_id = ? AND user_id = ? AND kind = ?",
                                 (o["id"], uid, kind)).fetchone()
            if ex:
                req.con.execute("UPDATE assignments SET share = ?, status = CASE WHEN status = 'declined' THEN 'pending' ELSE status END "
                                "WHERE id = ?", (share, ex["id"]))
            else:
                # кого назначил сам себя — подтверждать не нужно
                st = "confirmed" if uid == req.user["id"] else "pending"
                req.con.execute("INSERT INTO assignments(order_id, user_id, kind, share, status, created, responded) "
                                "VALUES (?, ?, ?, ?, ?, ?, ?)", (o["id"], uid, kind, share, st, db.now(),
                                                                 db.now() if st == "confirmed" else None))
            names.append(u["name"])
        logic.log(req.con, o["id"], req.user["id"], "Назначены{}: {}".format(" (диагност)" if kind == "diag" else "", ", ".join(names)))
    raise Redirect("/orders/{}".format(o["id"]), "Назначено. Мастера увидят уведомление.")


@route(r"/assign/(\d+)/(confirm|decline|delete)", ("POST",))
def assign_answer(req, aid, action):
    a = req.con.execute("SELECT * FROM assignments WHERE id = ?", (int(aid),)).fetchone()
    if not a:
        raise HTTPError(404, "Не найдено")
    o = get_order(req, a["order_id"])
    back = req.form.get("back") or "/orders/{}".format(o["id"])
    if not back.startswith("/"):
        back = "/"
    with db.write_lock():
        if action == "delete":
            req.need_boss()
            if o["status"] == "closed":
                raise Redirect(back, "!Заказ закрыт")
            req.con.execute("DELETE FROM assignments WHERE id = ?", (a["id"],))
            logic.log(req.con, o["id"], req.user["id"], "Мастер убран из заказа")
            raise Redirect(back, "Убрано")
        if a["user_id"] != req.user["id"]:
            raise HTTPError(403, "Это не ваше назначение")
        st = "confirmed" if action == "confirm" else "declined"
        req.con.execute("UPDATE assignments SET status = ?, responded = ? WHERE id = ?", (st, db.now(), a["id"]))
        logic.log(req.con, o["id"], req.user["id"], "Подтвердил участие" if st == "confirmed" else "Отказался от участия")
    raise Redirect(back, "Участие подтверждено" if action == "confirm" else "Отмечено: не участвую")


# ================================ ОПЛАТА ================================
@route(r"/orders/(\d+)/pay", ("POST",))
def pay(req, oid):
    req.need_boss()
    o = get_order(req, int(oid))
    amount = to_int(req.form.get("amount"), 0)
    wallet = req.form.get("wallet")
    if amount <= 0 or amount > 50_000_000:
        raise Redirect("/orders/{}".format(o["id"]), "!Неверная сумма")
    if wallet not in ("cash", "b1", "b2", "b3"):
        raise Redirect("/orders/{}".format(o["id"]), "!Выберите, куда пришли деньги")
    s = db.get_settings(req.con)
    with db.write_lock():
        req.con.execute("INSERT INTO payments(order_id, amount, wallet, created, user_id) VALUES (?, ?, ?, ?, ?)",
                        (o["id"], amount, wallet, db.now(), req.user["id"]))
        logic.log(req.con, o["id"], req.user["id"], "Оплата {} — {}".format(money(amount), s["wallets"][wallet]))
    raise Redirect("/orders/{}".format(o["id"]), "Оплата {} принята".format(money(amount)))


@route(r"/payments/(\d+)/delete", ("POST",))
def pay_delete(req, pid):
    req.need_boss()
    p = req.con.execute("SELECT * FROM payments WHERE id = ?", (int(pid),)).fetchone()
    if not p:
        raise HTTPError(404, "Не найдено")
    o = get_order(req, p["order_id"])
    if o["status"] == "closed":
        raise Redirect("/orders/{}".format(o["id"]), "!Заказ закрыт")
    with db.write_lock():
        req.con.execute("DELETE FROM payments WHERE id = ?", (p["id"],))
        logic.log(req.con, o["id"], req.user["id"], "Удалена оплата {}".format(money(p["amount"])))
    raise Redirect("/orders/{}".format(o["id"]), "Оплата удалена")


# ================================ ФОТО ================================
def image_ext(data):
    if data[:3] == b"\xff\xd8\xff":
        return "jpg"
    if data[:8] == b"\x89PNG\r\n\x1a\n":
        return "png"
    if data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        return "webp"
    return None


def save_image(data):
    ext = image_ext(data)
    if not ext:
        return None
    sub = datetime.now().strftime("%Y-%m")
    os.makedirs(os.path.join(db.PHOTO_DIR, sub), exist_ok=True)
    name = "{}/{}.{}".format(sub, secrets.token_hex(10), ext)
    with open(os.path.join(db.PHOTO_DIR, name), "wb") as f:
        f.write(data)
    return name


@route(r"/orders/(\d+)/photo", ("POST",))
def photo_upload(req, oid):
    o = get_order(req, int(oid))
    files = req.files.get("photo") or []
    thumbs = req.files.get("thumb") or []
    kind = req.form.get("kind") if req.form.get("kind") in ("work", "car", "client") else "work"
    caption = req.f("caption", 200)
    n = 0
    with db.write_lock():
        for i, f in enumerate(files):
            if not f["data"]:
                continue
            name = save_image(f["data"])
            if not name:
                continue
            tname = save_image(thumbs[i]["data"]) if i < len(thumbs) and thumbs[i]["data"] else None
            req.con.execute("INSERT INTO photos(order_id, client_id, kind, file, thumb, caption, created, user_id) "
                            "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                            (o["id"], o["client_id"], kind, name, tname or name, caption, db.now(), req.user["id"]))
            n += 1
        if n:
            logic.log(req.con, o["id"], req.user["id"], "Добавлено фото: {}{}".format(n, " — " + caption if caption else ""))
    if req.handler.headers.get("X-Requested-With") == "fetch":
        return json_response({"ok": bool(n), "count": n, "message": "Загружено фото: {}".format(n) if n else "Это не фото (нужен JPG/PNG)"})
    raise Redirect("/orders/{}".format(o["id"]), "Загружено фото: {}".format(n) if n else "!Это не фото (нужен JPG/PNG)")


def send_photo(req, p, thumb=False):
    name = p["thumb"] if thumb and p["thumb"] else p["file"]
    path = os.path.realpath(os.path.join(db.PHOTO_DIR, name))
    if not path.startswith(os.path.realpath(db.PHOTO_DIR) + os.sep) or not os.path.isfile(path):
        raise HTTPError(404, "Фото не найдено")
    with open(path, "rb") as f:
        data = f.read()
    ctype = {"jpg": "image/jpeg", "png": "image/png", "webp": "image/webp"}.get(name.rsplit(".", 1)[-1], "image/jpeg")
    return Response(data, 200, ctype, [("Cache-Control", "private, max-age=604800")])


@route(r"/photo/(\d+)(/t)?")
def photo_get(req, pid, t):
    p = req.con.execute("SELECT * FROM photos WHERE id = ?", (int(pid),)).fetchone()
    if not p:
        raise HTTPError(404, "Фото не найдено")
    return send_photo(req, p, bool(t))


@route(r"/photos/(\d+)/(share|delete)", ("POST",))
def photo_action(req, pid, action):
    p = req.con.execute("SELECT * FROM photos WHERE id = ?", (int(pid),)).fetchone()
    if not p:
        raise HTTPError(404, "Фото не найдено")
    back = "/orders/{}".format(p["order_id"]) if p["order_id"] else "/clients/{}".format(p["client_id"])
    with db.write_lock():
        if action == "share":
            req.need_boss()
            req.con.execute("UPDATE photos SET shared = 1 - shared WHERE id = ?", (p["id"],))
            raise Redirect(back, "Фото {} клиенту".format("скрыто от" if p["shared"] else "будет видно"))
        if not (req.is_boss() or p["user_id"] == req.user["id"]):
            raise HTTPError(403, "Удалить может главный или автор фото")
        req.con.execute("DELETE FROM photos WHERE id = ?", (p["id"],))
        for name in {p["file"], p["thumb"]}:
            try:
                if name:
                    os.unlink(os.path.join(db.PHOTO_DIR, name))
            except OSError:
                pass
        if p["order_id"]:
            logic.log(req.con, p["order_id"], req.user["id"], "Удалено фото")
    raise Redirect(back, "Фото удалено")
