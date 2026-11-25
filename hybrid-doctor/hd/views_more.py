# -*- coding: utf-8 -*-
"""Вход, клиенты, склад, отчёты, гарантийный талон, профиль, админка."""
import csv
import io
import os
import re
import tempfile
import time
import urllib.parse
from datetime import date, datetime, timedelta

from . import db, logic
from .web import (MOUNT, route, h, money, qty_fmt, to_int, to_float, clean, fdate, Redirect, HTTPError, Response,
                  start_session, end_session)
from .ui import page, form, field, select, status_chip, order_no, client_no, client_name, empty
from .views_orders import (ORDER_LIST_SQL, order_row, get_order, wa_link, send_photo, save_image, norm_plate)

# ================================ ВХОД ================================
@route("/login", ("GET", "POST"), public=True)
def login(req):
    nxt = req.query.get("next") or req.form.get("next") or "/"
    if not nxt.startswith("/") or nxt.startswith("//"):
        nxt = "/"
    err = ""
    if req.method == "POST":
        ip = req.ip
        now = time.time()
        with db.write_lock():
            req.con.execute("DELETE FROM login_attempts WHERE created < ?", (now - 900,))
            allowed = req.con.execute("SELECT COUNT(*) FROM login_attempts WHERE ip = ?", (ip,)).fetchone()[0] < 8
            if allowed:
                req.con.execute("INSERT INTO login_attempts(ip, created) VALUES (?, ?)", (ip, now))
        if not allowed:
            err = "Слишком много попыток. Подождите 15 минут."
        else:
            lg = req.f("login", 60)
            pw = req.form.get("password", "")
            digits = re.sub(r"\D", "", lg)
            u = req.con.execute("SELECT * FROM users WHERE login = ? AND active = 1", (lg,)).fetchone()
            if not u and len(digits) >= 9:
                for cand in req.con.execute("SELECT * FROM users WHERE active = 1 AND phone != ''"):
                    if re.sub(r"\D", "", cand["phone"])[-9:] == digits[-9:]:
                        u = cand
                        break
            if u and db.check_password(pw, u["pass_hash"]):
                req.con.execute("DELETE FROM login_attempts WHERE ip = ?", (ip,))
                start_session(req, u["id"])
                raise Redirect(nxt)
            time.sleep(0.8)
            err = "Неверный логин или пароль"
    body = """<div class="login">
<img src="/static/icons/icon-192.png" alt="" width="84" height="84">
<h2>Hybrid Doctor</h2><p class="muted">Вход для сотрудников</p>
<form method="post">{err}<input type="hidden" name="next" value="{nxt}">
<label class="f"><span>Логин или телефон</span><input name="login" autocomplete="username" autocapitalize="none" required></label>
<label class="f"><span>Пароль</span><input type="password" name="password" autocomplete="current-password" required></label>
<button class="btn big main">Войти</button></form>
<button class="btn ghost install-btn" hidden>📲 Установить приложение</button>
<p class="ios-hint muted" hidden>На iPhone: нажмите «Поделиться» ⬆ и «На экран Домой», чтобы появилась иконка.</p>
</div>""".format(err='<p class="flash bad">{}</p>'.format(h(err)) if err else "", nxt=h(nxt))
    return page(req, "Вход", body)


@route("/logout", ("POST",))
def logout(req):
    end_session(req)
    raise Redirect("/login")


# ================================ КЛИЕНТЫ ================================
@route("/clients")
def clients(req):
    q = clean(req.query.get("q", ""), 60)
    args = []
    sql = ("SELECT c.*, (SELECT COUNT(*) FROM orders o WHERE o.client_id = c.id) AS n, "
           "(SELECT GROUP_CONCAT(plate || ' ' || model, ', ') FROM cars WHERE client_id = c.id) AS cars "
           "FROM clients c ")
    if q:
        nq = norm_plate(q)
        sql += ("WHERE c.last_name LIKE ? OR c.first_name LIKE ? OR c.phone LIKE ? OR c.id = ? OR EXISTS "
                "(SELECT 1 FROM cars WHERE client_id = c.id AND (REPLACE(REPLACE(UPPER(plate),' ',''),'-','') LIKE ? OR model LIKE ? OR vin LIKE ?)) ")
        num = re.sub(r"\D", "", q)
        args = ["%" + q + "%"] * 3 + [int(num) if num else -1, "%" + (nq or q) + "%", "%" + q + "%", "%" + q + "%"]
    sql += "ORDER BY c.id DESC LIMIT 200"
    rows = req.con.execute(sql, args).fetchall()
    items = "".join('<a class="row" href="/clients/{id}"><div class="main"><b>{name}</b> <small class="muted">{no}</small>'
                    '<div class="sub">{cars}</div><div class="sub muted">{ph}</div></div><div class="side"><small>{n} зак.</small></div></a>'.format(
                        id=c["id"], name=h(client_name(c)), no=client_no(c["id"]), cars=h(c["cars"] or ""),
                        ph=h(c["phone"]), n=c["n"]) for c in rows)
    body = ('<form class="search" method="get"><input type="search" name="q" value="{}" placeholder="Имя, телефон, номер авто, K-0001"></form>'
            .format(h(q)) + ('<div class="list">{}</div>'.format(items) if items else empty("Клиентов не найдено.")))
    return page(req, "Клиенты", body, nav="clients")


@route(r"/clients/(\d+)", ("GET", "POST"))
def client_view(req, cid):
    c = req.con.execute("SELECT * FROM clients WHERE id = ?", (int(cid),)).fetchone()
    if not c:
        raise HTTPError(404, "Клиент не найден")
    if req.method == "POST":
        with db.write_lock():
            act = req.form.get("act")
            if act == "client":
                req.con.execute("UPDATE clients SET last_name = ?, first_name = ?, phone = ?, note = ? WHERE id = ?",
                                (req.f("last_name", 60), req.f("first_name", 60), req.f("phone", 30), req.ftext("note", 2000), c["id"]))
            elif act == "car":
                car_id = to_int(req.form.get("car_id"), 0)
                vals = (req.f("plate", 20).upper(), req.f("model", 80), req.f("vin", 30).upper(), req.f("year", 4))
                if car_id:
                    req.con.execute("UPDATE cars SET plate = ?, model = ?, vin = ?, year = ? WHERE id = ? AND client_id = ?",
                                    vals + (car_id, c["id"]))
                elif vals[0] or vals[1]:
                    req.con.execute("INSERT INTO cars(client_id, plate, model, vin, year, created) VALUES (?, ?, ?, ?, ?, ?)",
                                    (c["id"],) + vals + (db.now(),))
        raise Redirect("/clients/{}".format(c["id"]), "Сохранено")
    cars = req.con.execute("SELECT * FROM cars WHERE client_id = ? ORDER BY id", (c["id"],)).fetchall()
    ords = req.con.execute(ORDER_LIST_SQL + "WHERE o.client_id = ? ORDER BY o.id DESC", (c["id"],)).fetchall()
    ph = req.con.execute("SELECT * FROM photos WHERE client_id = ? AND kind = 'client' ORDER BY id DESC LIMIT 1", (c["id"],)).fetchone()
    wa = wa_link(c["phone"])
    out = ['<div class="card head">{img}<div class="cardhead"><b class="big">{name}</b><span class="chip">{no}</span></div>'
           '<div class="sub">{ph}</div>{btn}</div>'.format(
               img='<img class="avatar" src="/photo/{}/t" alt="">'.format(ph["id"]) if ph else "",
               name=h(client_name(c)), no=client_no(c["id"]), ph=h(c["phone"]),
               btn='<div class="btnrow"><a class="btn small" href="tel:{}">Позвонить</a><a class="btn small wa" target="_blank" href="{}">WhatsApp</a></div>'.format(
                   h(re.sub(r"[^\d+]", "", c["phone"])), h(wa)) if wa else "")]
    total = 0
    if req.is_boss():
        for o in ords:
            if o["status"] == "closed":
                total += logic.order_totals(req.con, o["id"])["total"]
        out.append('<div class="stats"><div><b>{}</b><span>визитов</span></div><div><b>{}</b><span>оплачено всего</span></div></div>'.format(len(ords), money(total)))
    out.append('<h2>Автомобили</h2>')
    for car in cars:
        out.append('<details class="card more"><summary><b>{}</b> {} {}</summary>{}</details>'.format(
            h(car["plate"] or "без номера"), h(car["model"]), h(car["vin"]),
            form(req, "/clients/{}".format(c["id"]), '<input type="hidden" name="act" value="car"><input type="hidden" name="car_id" value="{}">'.format(car["id"])
                 + '<div class="two">' + field("Госномер", "plate", car["plate"]) + field("Модель", "model", car["model"]) + '</div>'
                 + '<div class="two">' + field("VIN", "vin", car["vin"]) + field("Год", "year", car["year"]) + '</div>'
                 + '<button class="btn small">Сохранить</button>')))
    out.append('<details class="card more"><summary>+ Добавить автомобиль</summary>{}</details>'.format(
        form(req, "/clients/{}".format(c["id"]), '<input type="hidden" name="act" value="car">'
             + '<div class="two">' + field("Госномер", "plate") + field("Модель", "model") + '</div>'
             + '<div class="two">' + field("VIN", "vin") + field("Год", "year") + '</div><button class="btn small">Добавить</button>')))
    out.append('<h2>Заказы</h2>' + ('<div class="list">{}</div>'.format("".join(
        order_row(req, o, logic.order_totals(req.con, o["id"])) for o in ords)) if ords else empty("Заказов нет.")))
    out.append('<details class="card more"><summary>Изменить данные клиента</summary>{}</details>'.format(
        form(req, "/clients/{}".format(c["id"]), '<input type="hidden" name="act" value="client"><div class="two">'
             + field("Фамилия", "last_name", c["last_name"]) + field("Имя", "first_name", c["first_name"]) + '</div>'
             + field("Телефон", "phone", c["phone"], "tel") + field("Заметка", "note", c["note"], "textarea")
             + '<button class="btn small">Сохранить</button>')))
    return page(req, "Клиент", "".join(out), nav="clients", back="/clients")


# ================================ СКЛАД ================================
@route("/stock")
def stock(req):
    q = clean(req.query.get("q", ""), 60)
    low = req.query.get("low") == "1"
    where, args = ["active = 1"], []
    if q:
        where.append("(name LIKE ? OR sku LIKE ?)")
        args += ["%" + q + "%"] * 2
    if low:
        where.append("qty <= min_qty AND min_qty > 0")
    rows = req.con.execute("SELECT * FROM parts WHERE " + " AND ".join(where) + " ORDER BY name LIMIT 500", args).fetchall()
    boss = req.is_boss()
    items = []
    for p in rows:
        warn = p["min_qty"] > 0 and p["qty"] <= p["min_qty"]
        price = '<small class="muted">{} / закуп {}</small>'.format(money(p["price"]), money(p["cost"])) if boss else ""
        items.append('<a class="row" href="/stock/{id}"><div class="main"><b>{name}</b><div class="sub muted">{sku}</div></div>'
                     '<div class="side"><b class="{cls}">{q} {u}</b>{price}</div></a>'.format(
                         id=p["id"], name=h(p["name"]), sku=h(p["sku"]), cls="red" if warn else "",
                         q=qty_fmt(p["qty"]), u=h(p["unit"]), price=price))
    value = ""
    if boss:
        v = req.con.execute("SELECT COALESCE(SUM(qty*cost),0) FROM parts WHERE active = 1 AND qty > 0").fetchone()[0]
        value = '<p class="muted">Склад по закупке: <b>{}</b></p>'.format(money(v))
    chips = '<div class="chips"><a class="{}" href="/stock">Все</a><a class="{}" href="/stock?low=1">Заканчиваются</a></div>'.format(
        "" if low else "on", "on" if low else "")
    body = ('<form class="search" method="get"><input type="search" name="q" value="{}" placeholder="Название или артикул"></form>'.format(h(q))
            + chips + value + ('<div class="list">{}</div>'.format("".join(items)) if items else empty("Ничего не найдено.")))
    acts = '<a class="hbtn" href="/stock/new">+ Позиция</a>' if boss else ""
    return page(req, "Склад", body, nav="stock", actions=acts)


def part_form(req, p=None):
    p = p or {"sku": "", "name": "", "unit": "шт", "cost": "", "price": "", "min_qty": ""}
    return ('<div class="two">' + field("Название", "name", p["name"], attrs="required") + field("Артикул", "sku", p["sku"]) + '</div>'
            + '<div class="two">' + field("Закупочная цена", "cost", p["cost"], attrs='inputmode="numeric"')
            + field("Цена для клиента", "price", p["price"], attrs='inputmode="numeric"') + '</div>'
            + '<div class="two">' + field("Ед. изм.", "unit", p["unit"]) + field("Мин. остаток (предупредить)", "min_qty", qty_fmt(p["min_qty"]) if p["min_qty"] != "" else "", attrs='inputmode="decimal"') + '</div>')


@route("/stock/new", ("GET", "POST"))
def stock_new(req):
    req.need_boss()
    if req.method == "POST":
        name = req.f("name", 200)
        if not name:
            raise Redirect("/stock/new", "!Укажите название")
        qty = to_float(req.form.get("qty"), 0)
        cost = to_int(req.form.get("cost"), 0)
        with db.write_lock():
            pid = req.con.execute("INSERT INTO parts(sku, name, unit, qty, cost, price, min_qty, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                                  (req.f("sku", 60), name, req.f("unit", 10) or "шт", qty, cost, to_int(req.form.get("price"), 0),
                                   to_float(req.form.get("min_qty"), 0), db.now())).lastrowid
            if qty:
                req.con.execute("INSERT INTO stock_moves(part_id, delta, reason, cost, note, created, user_id) VALUES (?, ?, 'in', ?, 'начальный остаток', ?, ?)",
                                (pid, qty, cost, db.now(), req.user["id"]))
        raise Redirect("/stock/{}".format(pid), "Позиция добавлена")
    body = '<div class="card">' + form(req, "/stock/new", part_form(req) + field("Сейчас на складе", "qty", "0", attrs='inputmode="decimal"')
                                       + '<button class="btn main">Добавить</button>') + '</div>'
    return page(req, "Новая позиция", body, nav="stock", back="/stock")


@route(r"/stock/(\d+)", ("GET", "POST"))
def stock_item(req, pid):
    p = req.con.execute("SELECT * FROM parts WHERE id = ?", (int(pid),)).fetchone()
    if not p:
        raise HTTPError(404, "Позиция не найдена")
    boss = req.is_boss()
    if req.method == "POST":
        req.need_boss()
        act = req.form.get("act")
        with db.write_lock():
            if act == "edit":
                name = req.f("name", 200)
                if not name:
                    raise Redirect("/stock/{}".format(p["id"]), "!Укажите название")
                req.con.execute("UPDATE parts SET name = ?, sku = ?, unit = ?, cost = ?, price = ?, min_qty = ? WHERE id = ?",
                                (name, req.f("sku", 60), req.f("unit", 10) or "шт", to_int(req.form.get("cost"), 0),
                                 to_int(req.form.get("price"), 0), to_float(req.form.get("min_qty"), 0), p["id"]))
                msg = "Сохранено"
            elif act == "in":
                qty = to_float(req.form.get("qty"), 0)
                if qty <= 0:
                    raise Redirect("/stock/{}".format(p["id"]), "!Укажите количество")
                cost = to_int(req.form.get("cost"), p["cost"]) if str(req.form.get("cost", "")).strip() else p["cost"]
                req.con.execute("UPDATE parts SET qty = qty + ?, cost = ? WHERE id = ?", (qty, cost, p["id"]))
                req.con.execute("INSERT INTO stock_moves(part_id, delta, reason, cost, note, created, user_id) VALUES (?, ?, 'in', ?, ?, ?, ?)",
                                (p["id"], qty, cost, req.f("note", 200), db.now(), req.user["id"]))
                msg = "Приход +{} {}".format(qty_fmt(qty), p["unit"])
            elif act == "adjust":
                real = to_float(req.form.get("real"), -1)
                if real < 0:
                    raise Redirect("/stock/{}".format(p["id"]), "!Укажите фактический остаток")
                delta = real - p["qty"]
                req.con.execute("UPDATE parts SET qty = ? WHERE id = ?", (real, p["id"]))
                req.con.execute("INSERT INTO stock_moves(part_id, delta, reason, cost, note, created, user_id) VALUES (?, ?, 'adjust', ?, ?, ?, ?)",
                                (p["id"], delta, p["cost"], req.f("note", 200) or "пересчёт", db.now(), req.user["id"]))
                msg = "Остаток исправлен ({:+g})".format(delta)
            elif act == "archive":
                req.con.execute("UPDATE parts SET active = 0 WHERE id = ?", (p["id"],))
                raise Redirect("/stock", "Позиция убрана в архив")
            else:
                raise HTTPError(400, "Неверный запрос")
        raise Redirect("/stock/{}".format(p["id"]), msg)
    moves = req.con.execute("SELECT m.*, u.name FROM stock_moves m LEFT JOIN users u ON u.id = m.user_id WHERE m.part_id = ? "
                            "ORDER BY m.id DESC LIMIT 100", (p["id"],)).fetchall()
    rs = {"in": "Приход", "order": "В заказ", "return": "Возврат", "adjust": "Пересчёт"}
    mv = "".join('<div class="lrow"><div class="main">{r}{o}<small class="muted">{d} · {n}{note}</small></div><div class="side"><b class="{c}">{q:+g}</b></div></div>'.format(
        r=rs.get(m["reason"], m["reason"]), o=' <a href="/orders/{}">{}</a>'.format(m["order_id"], order_no(m["order_id"])) if m["order_id"] else "",
        d=fdate(m["created"]), n=h(m["name"] or ""), note=" · " + h(m["note"]) if m["note"] else "",
        c="red" if m["delta"] < 0 else "green", q=m["delta"]) for m in moves)
    out = ['<div class="card head"><div class="cardhead"><b class="big">{}</b></div><div class="sub muted">{}</div>'
           '<div class="stats"><div><b>{} {}</b><span>на складе</span></div>{}</div></div>'.format(
               h(p["name"]), h(p["sku"]), qty_fmt(p["qty"]), h(p["unit"]),
               '<div><b>{}</b><span>цена</span></div><div><b>{}</b><span>закуп</span></div>'.format(money(p["price"]), money(p["cost"])) if boss else "")]
    if boss:
        out.append('<div class="card"><h3>Приход</h3>' + form(req, "/stock/{}".format(p["id"]), '<input type="hidden" name="act" value="in"><div class="two">'
                   + field("Количество", "qty", "", attrs='inputmode="decimal" required') + field("Закупочная цена", "cost", p["cost"], attrs='inputmode="numeric"')
                   + '</div>' + field("Откуда / заметка", "note", "") + '<button class="btn main small">Оприходовать</button>') + '</div>')
        out.append('<details class="card more"><summary>Пересчёт (фактический остаток)</summary>' + form(req, "/stock/{}".format(p["id"]),
                   '<input type="hidden" name="act" value="adjust">' + field("Сколько реально на полке", "real", qty_fmt(p["qty"]), attrs='inputmode="decimal"')
                   + field("Причина", "note", "") + '<button class="btn small">Исправить остаток</button>') + '</details>')
        out.append('<details class="card more"><summary>Изменить позицию</summary>' + form(req, "/stock/{}".format(p["id"]),
                   '<input type="hidden" name="act" value="edit">' + part_form(req, p) + '<button class="btn small">Сохранить</button>')
                   + form(req, "/stock/{}".format(p["id"]), '<input type="hidden" name="act" value="archive"><button class="btn small ghost danger" data-confirm="Убрать позицию в архив?">Убрать в архив</button>')
                   + '</details>')
    out.append('<div class="card"><h3>Движение</h3>{}</div>'.format(mv or '<p class="muted">Движений нет.</p>'))
    return page(req, "Склад", "".join(out), nav="stock", back="/stock")


# ================================ ОТЧЁТЫ ================================
def period(req):
    today = date.today()
    try:
        d1 = datetime.strptime(req.query.get("from", ""), "%Y-%m-%d").date()
    except ValueError:
        d1 = today - timedelta(days=14)
    try:
        d2 = datetime.strptime(req.query.get("to", ""), "%Y-%m-%d").date()
    except ValueError:
        d2 = today
    if d1 > d2:
        d1, d2 = d2, d1
    return d1.isoformat(), d2.isoformat()


def report_data(con, d1, d2):
    s = db.get_settings(con)
    closed = con.execute("SELECT id FROM orders WHERE status = 'closed' AND substr(closed_at,1,10) BETWEEN ? AND ?", (d1, d2)).fetchall()
    works = parts = parts_cost = debt = 0
    diag_only = 0
    for o in closed:
        t = logic.order_totals(con, o["id"])
        works += t["works"]
        parts += t["parts"]
        parts_cost += t["parts_cost"]
        debt += max(t["due"], 0)
        if t["total"] == 0:
            diag_only += 1
    cash = {r["wallet"]: r["s"] for r in con.execute(
        "SELECT wallet, SUM(amount) AS s FROM payments WHERE substr(created,1,10) BETWEEN ? AND ? GROUP BY wallet", (d1, d2))}
    used = con.execute(
        "SELECT p.name, p.sku, p.unit, -SUM(m.delta) AS q, SUM(-m.delta*m.cost) AS cost, SUM(-m.delta*m.price) AS sale, p.qty AS left "
        "FROM stock_moves m JOIN parts p ON p.id = m.part_id WHERE m.reason IN ('order','return') "
        "AND substr(m.created,1,10) BETWEEN ? AND ? GROUP BY p.id HAVING q != 0 ORDER BY p.name", (d1, d2)).fetchall()
    users, unassigned, pending_orders = logic.payroll(con, d1, d2)
    names = {u["id"]: (u["name"], u["role"]) for u in con.execute("SELECT id, name, role FROM users")}
    staff = sorted(([names.get(uid, ("?", ""))[0], db.ROLES.get(names.get(uid, ("", ""))[1], ""), v["work"], v["diag"], len(v["orders"])]
                    for uid, v in users.items()), key=lambda r: -(r[2] + r[3]))
    debts = []
    for o in con.execute(ORDER_LIST_SQL + "WHERE o.status = 'closed' ORDER BY o.id DESC").fetchall():
        t = logic.order_totals(con, o["id"])
        if t["due"] > 0:
            debts.append((o, t["due"]))
    return {"s": s, "count": len(closed), "works": works, "parts": parts, "parts_cost": parts_cost, "debt": debt,
            "diag_only": diag_only, "cash": cash, "used": used, "staff": staff, "unassigned": unassigned,
            "pending_orders": pending_orders, "debts": debts}


@route("/reports")
def reports(req):
    req.need_boss()
    d1, d2 = period(req)
    r = report_data(req.con, d1, d2)
    s = r["s"]
    total = r["works"] + r["parts"]
    payroll_total = sum(x[2] + x[3] for x in r["staff"])
    q = "from={}&to={}".format(d1, d2)
    today = date.today()
    quick = [("15 дней", today - timedelta(days=14), today), ("Этот месяц", today.replace(day=1), today),
             ("Сегодня", today, today)]
    out = ['<form class="card period" method="get"><div class="two">{}{}</div><button class="btn small main">Показать</button>'
           '<div class="chips">{}</div></form>'.format(
               field("С", "from", d1, "date"), field("По", "to", d2, "date"),
               "".join('<a href="/reports?from={}&to={}">{}</a>'.format(a.isoformat(), b.isoformat(), n) for n, a, b in quick))]
    out.append('<div class="stats"><div><b>{}</b><span>выдано машин</span></div><div><b>{}</b><span>выручка</span></div>'
               '<div><b>{}</b><span>средний чек</span></div></div>'.format(
                   r["count"], money(total), money(total / max(r["count"] - r["diag_only"], 1) if r["count"] else 0)))
    out.append('<div class="card"><h3>Выручка по выданным машинам</h3><div class="totals">'
               '<div><span>Работы</span><b>{}</b></div><div><span>Запчасти (продажа)</span><b>{}</b></div>'
               '<div><span>Запчасти (закуп)</span><b>−{}</b></div><div><span>Зарплата мастеров</span><b>−{}</b></div>'
               '<div class="grand"><span>Остаётся сервису</span><b>{}</b></div>'
               '<div><span>Только диагностика (бесплатно)</span><b>{}</b></div>{}</div></div>'.format(
                   money(r["works"]), money(r["parts"]), money(r["parts_cost"]), money(payroll_total),
                   money(total - r["parts_cost"] - payroll_total), r["diag_only"],
                   '<div><span>Не оплачено клиентами</span><b class="red">{}</b></div>'.format(money(r["debt"])) if r["debt"] else ""))
    out.append('<div class="card"><h3>Касса: деньги, принятые за период</h3><div class="totals">{}<div class="grand"><span>Всего</span><b>{}</b></div></div></div>'.format(
        "".join('<div><span>{}</span><b>{}</b></div>'.format(h(s["wallets"][k]), money(r["cash"].get(k, 0))) for k in ("cash", "b1", "b2", "b3")),
        money(sum(r["cash"].values()))))
    st = "".join('<div class="lrow"><div class="main"><b>{}</b><small class="muted">{} · заказов {}</small></div><div class="side"><b>{}</b>{}</div></div>'.format(
        h(n), h(role), cnt, money(w + d), '<small class="muted">в т.ч. диагност {}</small>'.format(money(d)) if d >= 1 else "")
        for n, role, w, d, cnt in r["staff"])
    warn = ""
    if r["pending_orders"]:
        warn += '<p class="hint red">Не все мастера подтвердили участие в {} заказ(ах) — их доля пока не начислена.</p>'.format(len(r["pending_orders"]))
    if r["unassigned"] >= 1:
        warn += '<p class="hint">Не распределено (в заказах без мастеров): {}</p>'.format(money(r["unassigned"]))
    out.append('<div class="card"><h3>Зарплата мастеров</h3>{}{}<a class="btn small" href="/reports.csv?type=staff&{}">Скачать для Excel</a></div>'.format(
        st or '<p class="muted">Начислений нет.</p>', warn, q))
    used = "".join('<div class="lrow"><div class="main">{}<small class="muted">{} · осталось {}</small></div><div class="side"><b>{} {}</b><small class="muted">{}</small></div></div>'.format(
        h(u["name"]), h(u["sku"]), qty_fmt(u["left"]), qty_fmt(u["q"]), h(u["unit"]), money(u["sale"])) for u in r["used"])
    out.append('<div class="card"><h3>Израсходовано запчастей</h3>{}<a class="btn small" href="/reports.csv?type=parts&{}">Скачать для Excel</a></div>'.format(
        used or '<p class="muted">Ничего не списано.</p>', q))
    if r["debts"]:
        out.append('<div class="card"><h3>Должники (все периоды)</h3><div class="list">{}</div></div>'.format(
            "".join(order_row(req, o, logic.order_totals(req.con, o["id"])) for o, _ in r["debts"])))
    out.append('<a class="btn" href="/reports.csv?type=orders&{}">Скачать все заказы периода для Excel</a>'.format(q))
    return page(req, "Отчёт", "".join(out), nav="more", back="/more")


@route("/reports.csv")
def reports_csv(req):
    req.need_boss()
    d1, d2 = period(req)
    typ = req.query.get("type")
    buf = io.StringIO()
    w = csv.writer(buf, delimiter=";")
    s = db.get_settings(req.con)
    def safe(v):
        v = "" if v is None else v
        return "'" + v if isinstance(v, str) and v[:1] in ("=", "+", "-", "@") else v
    if typ == "staff":
        r = report_data(req.con, d1, d2)
        w.writerow(["Сотрудник", "Роль", "Заказов", "Процент с работ", "Процент диагноста", "Итого"])
        for n, role, wk, dg, cnt in r["staff"]:
            w.writerow([safe(n), role, cnt, round(wk), round(dg), round(wk + dg)])
    elif typ == "parts":
        r = report_data(req.con, d1, d2)
        w.writerow(["Запчасть", "Артикул", "Израсходовано", "Ед.", "Закуп, сом", "Продажа, сом", "Остаток"])
        for u in r["used"]:
            w.writerow([safe(u["name"]), safe(u["sku"]), qty_fmt(u["q"]), u["unit"], round(u["cost"] or 0), round(u["sale"] or 0), qty_fmt(u["left"])])
    else:
        typ = "orders"
        w.writerow(["Заказ", "Выдан", "Клиент", "Телефон", "Авто", "Госномер", "Работы", "Запчасти", "Итого", "Оплачено",
                    "Долг"] + [s["wallets"][k] for k in ("cash", "b1", "b2", "b3")] + ["Мастера"])
        for o in req.con.execute(ORDER_LIST_SQL + "WHERE o.status = 'closed' AND substr(o.closed_at,1,10) BETWEEN ? AND ? ORDER BY o.closed_at", (d1, d2)).fetchall():
            t = logic.order_totals(req.con, o["id"])
            pw = {r["wallet"]: r["s"] for r in req.con.execute("SELECT wallet, SUM(amount) AS s FROM payments WHERE order_id = ? GROUP BY wallet", (o["id"],))}
            ms = ", ".join(r["name"] for r in req.con.execute("SELECT u.name FROM assignments a JOIN users u ON u.id = a.user_id "
                                                              "WHERE a.order_id = ? AND a.status = 'confirmed'", (o["id"],)))
            ph = req.con.execute("SELECT phone FROM clients WHERE id = ?", (o["client_id"],)).fetchone()["phone"]
            w.writerow([order_no(o["id"]), fdate(o["closed_at"]), safe(client_name(o)), safe(ph), safe(o["model"]), safe(o["plate"]),
                        t["works"], t["parts"], t["total"], t["paid"], max(t["due"], 0)] + [pw.get(k, 0) for k in ("cash", "b1", "b2", "b3")] + [safe(ms)])
    data = ("﻿" + buf.getvalue()).encode("utf-8")
    name = "hybrid-{}-{}_{}.csv".format(typ, d1, d2)
    return Response(data, 200, "text/csv; charset=utf-8", [("Content-Disposition", 'attachment; filename="{}"'.format(name))])


# ================================ ГАРАНТИЯ ================================
def warranty_text(con, o, s):
    lines = con.execute("SELECT * FROM order_lines WHERE order_id = ? ORDER BY id", (o["id"],)).fetchall()
    works = "\n".join("• " + ln["name"] for ln in lines if ln["kind"] == "work") or "—"
    parts = "\n".join("• {} — {} {}".format(ln["name"], qty_fmt(ln["qty"]), "шт") for ln in lines if ln["kind"] == "part") or "—"
    vals = {"service": s.get("service_name", ""), "order": order_no(o["id"]).replace("№ ", ""),
            "date": fdate(o["closed_at"] or db.now(), False), "car": o["model"] or "", "plate": o["plate"] or "",
            "vin": o["vin"] or "—", "mileage": (o["mileage"] + " км") if o["mileage"] else "—", "client": client_name(o),
            "works": works, "parts": parts, "period": o["w_period"] or s.get("warranty_period", "")}
    text = s.get("warranty_template", "")
    for k, v in vals.items():
        text = text.replace("{" + k + "}", str(v))
    return text


def warranty_doc(req, o, s, text, photos, public=False):
    serials = ""
    if o["w_serials"].strip():
        serials = '<h3>Серийные номера установленных модулей / аккумуляторов</h3><pre>{}</pre>'.format(h(o["w_serials"]))
    gal = ""
    if photos:
        base = "/w/{}/p/".format(o["share_token"]) if public else "/photo/"
        gal = '<h3>Фото ремонта</h3><div class="wgal">{}</div>'.format("".join(
            '<figure><a href="{b}{id}" target="_blank"><img src="{b}{id}" loading="lazy" alt=""></a><figcaption>{c}</figcaption></figure>'.format(
                b=base, id=p["id"], c=h(p["caption"])) for p in photos))
    return ('<article class="warranty"><header><img src="/static/icons/icon-192.png" alt="" width="56" height="56"><div>'
            '<b>{name}</b><small>{addr}{ph}</small></div></header>'
            '<h2>Гарантийный талон {no}</h2><div class="wtext">{text}</div>{serials}'
            '<div class="sign"><div>Мастер: ____________________</div><div>Клиент: ____________________</div><div>М.П.</div></div>'
            '{gal}</article>').format(
        name=h(s.get("service_name", "")), addr=h(s.get("service_address", "")),
        ph=" · " + h(s.get("service_phone", "")) if s.get("service_phone") else "", no=h(order_no(o["id"])),
        text=h(text).replace("\n", "<br>"), serials=serials, gal=gal)


@route(r"/orders/(\d+)/warranty", ("GET", "POST"))
def warranty(req, oid):
    o = get_order(req, int(oid))
    s = db.get_settings(req.con)
    if req.method == "POST":
        req.need_boss()
        act = req.form.get("act")
        with db.write_lock():
            if act == "reset":
                req.con.execute("UPDATE orders SET w_text = NULL WHERE id = ?", (o["id"],))
            else:
                req.con.execute("UPDATE orders SET w_text = ?, w_period = ?, w_serials = ? WHERE id = ?",
                                (req.ftext("w_text", 10000), req.f("w_period", 60), req.ftext("w_serials", 3000), o["id"]))
            logic.log(req.con, o["id"], req.user["id"], "Изменён гарантийный талон")
        raise Redirect("/orders/{}/warranty".format(o["id"]), "Талон сохранён")
    text = o["w_text"] if o["w_text"] else warranty_text(req.con, o, s)
    photos = req.con.execute("SELECT * FROM photos WHERE order_id = ? AND shared = 1 ORDER BY id", (o["id"],)).fetchall()
    base = req.handler.headers.get("Host") or ""
    scheme = req.handler.headers.get("X-Forwarded-Proto")
    if scheme not in ("http", "https"):
        scheme = "https" if base and not base.startswith(("localhost", "127.")) else "http"
    link = "{}://{}{}/w/{}".format(scheme, base, MOUNT, o["share_token"])
    msg = "Здравствуйте, {}! Ваш автомобиль {} обслужен в {}. Гарантийный талон и фото ремонта: {}".format(
        o["first_name"] or client_name(o), o["model"] or o["plate"] or "", s.get("service_name", ""), link)
    wa = wa_link(o["cphone"], msg)
    out = []
    out.append('<div class="btnrow wrap noprint"><button class="btn main" onclick="window.print()">🖨 Печать</button>{}'
               '<button class="btn" data-copy="{}">Скопировать ссылку</button><a class="btn ghost" href="/w/{}" target="_blank">Как видит клиент</a></div>'.format(
                   '<a class="btn wa" target="_blank" href="{}">Отправить в WhatsApp</a>'.format(h(wa)) if wa else
                   '<span class="muted">Нет телефона клиента — WhatsApp недоступен</span>', h(link), h(o["share_token"])))
    out.append('<p class="hint noprint">Клиенту уйдёт ссылка на талон и на фото, отмеченные «👁 клиенту» в заказе (сейчас: {}).</p>'.format(len(photos)))
    out.append(warranty_doc(req, o, s, text, photos))
    if req.is_boss():
        out.append('<details class="card more noprint"><summary>✏️ Изменить талон</summary>' + form(req, "/orders/{}/warranty".format(o["id"]),
                   field("Срок гарантии", "w_period", o["w_period"] or s.get("warranty_period", ""))
                   + field("Серийные номера модулей / АКБ", "w_serials", o["w_serials"], "textarea", 'rows="4" placeholder="Модуль 1: SN…"')
                   + field("Текст талона", "w_text", text, "textarea", 'rows="16"')
                   + '<button class="btn main small">Сохранить</button>')
                   + form(req, "/orders/{}/warranty".format(o["id"]), '<input type="hidden" name="act" value="reset">'
                          '<button class="btn ghost small" data-confirm="Собрать текст заново по шаблону?">Собрать текст заново по шаблону</button>')
                   + '</details>')
    return page(req, "Гарантия " + order_no(o["id"]), "".join(out), nav="orders", back="/orders/{}".format(o["id"]))


@route(r"/w/([A-Za-z0-9_-]{8,40})", public=True)
def warranty_public(req, token):
    o = req.con.execute(
        "SELECT o.*, c.last_name, c.first_name, c.phone AS cphone, car.plate, car.model, car.vin, car.year "
        "FROM orders o JOIN clients c ON c.id = o.client_id LEFT JOIN cars car ON car.id = o.car_id "
        "WHERE o.share_token = ? AND o.status IN ('ready','closed')", (token,)).fetchone()
    if not o:
        raise HTTPError(404, "Талон не найден")
    s = db.get_settings(req.con)
    text = o["w_text"] if o["w_text"] else warranty_text(req.con, o, s)
    photos = req.con.execute("SELECT * FROM photos WHERE order_id = ? AND shared = 1 ORDER BY id", (o["id"],)).fetchall()
    body = ('<div class="btnrow noprint"><button class="btn" onclick="window.print()">🖨 Сохранить / распечатать</button></div>'
            + warranty_doc(req, o, s, text, photos, public=True))
    resp = page(req, "Гарантийный талон", body)
    return resp


@route(r"/w/([A-Za-z0-9_-]{8,40})/p/(\d+)", public=True)
def warranty_photo(req, token, pid):
    p = req.con.execute("SELECT p.* FROM photos p JOIN orders o ON o.id = p.order_id WHERE p.id = ? AND p.shared = 1 "
                        "AND o.share_token = ? AND o.status IN ('ready','closed')", (int(pid), token)).fetchone()
    if not p:
        raise HTTPError(404, "Фото не найдено")
    return send_photo(req, p)


# ================================ ЕЩЁ / ПРОФИЛЬ ================================
@route("/more")
def more(req):
    u = req.user
    items = [("/me", "💰 Мои начисления и пароль")]
    if req.is_boss():
        items += [("/reports", "📊 Отчёт за 15 дней: касса, склад, зарплата")]
    if req.is_owner():
        items += [("/admin/users", "👥 Сотрудники и роли"), ("/admin/rates", "％ Проценты мастеров"),
                  ("/admin/settings", "⚙️ Кошельки, гарантия, данные сервиса"), ("/admin/backup", "💾 Резервная копия базы")]
    body = '<div class="card"><b>{}</b><div class="muted">{} · вход: {}</div></div>'.format(h(u["name"]), db.ROLES.get(u["role"], ""), h(u["login"]))
    body += '<div class="list">{}</div>'.format("".join('<a class="row" href="{}"><div class="main">{}</div><div class="side">›</div></a>'.format(a, t) for a, t in items))
    body += ('<button class="btn install-btn" hidden>📲 Установить приложение на телефон</button>'
             '<p class="ios-hint muted" hidden>На iPhone: «Поделиться» ⬆ → «На экран Домой».</p>')
    body += form(req, "/logout", '<button class="btn ghost danger full">Выйти</button>')
    return page(req, "Ещё", body, nav="more")


@route("/me", ("GET", "POST"))
def me(req):
    u = req.user
    if req.method == "POST":
        cur, new, new2 = req.form.get("cur", ""), req.form.get("new", ""), req.form.get("new2", "")
        row = req.con.execute("SELECT pass_hash FROM users WHERE id = ?", (u["id"],)).fetchone()
        if not db.check_password(cur, row["pass_hash"]):
            raise Redirect("/me", "!Текущий пароль неверный")
        if len(new) < 6 or new != new2:
            raise Redirect("/me", "!Новый пароль: не короче 6 символов и одинаковый в обоих полях")
        with db.write_lock():
            req.con.execute("UPDATE users SET pass_hash = ? WHERE id = ?", (db.hash_password(new), u["id"]))
            req.con.execute("DELETE FROM sessions WHERE user_id = ? AND token != ?", (u["id"], req.session["token"]))
        raise Redirect("/me", "Пароль изменён")
    d1, d2 = period(req)
    users, _, _ = logic.payroll(req.con, d1, d2)
    mine = users.get(u["id"], {"work": 0, "diag": 0, "orders": set()})
    s = db.get_settings(req.con)
    # ожидается по незакрытым заказам
    expect = 0.0
    for o in req.con.execute("SELECT DISTINCT order_id FROM assignments WHERE user_id = ? AND status = 'confirmed' "
                             "AND order_id IN (SELECT id FROM orders WHERE status IN ('new','work','ready'))", (u["id"],)):
        v = logic.order_pay(req.con, o["order_id"], s)["by_user"].get(u["id"], {})
        expect += v.get("work", 0) + v.get("diag", 0)
    out = ['<form class="card period" method="get"><div class="two">{}{}</div><button class="btn small main">Показать</button></form>'.format(
        field("С", "from", d1, "date"), field("По", "to", d2, "date"))]
    out.append('<div class="stats"><div><b>{}</b><span>начислено за период</span></div><div><b>{}</b><span>заказов</span></div>'
               '<div><b>{}</b><span>ожидается (машины в работе)</span></div></div>'.format(
                   money(mine["work"] + mine["diag"]), len(mine["orders"]), money(expect)))
    if mine["orders"]:
        rows = req.con.execute(ORDER_LIST_SQL + "WHERE o.id IN ({}) ORDER BY o.closed_at DESC".format(
            ",".join(str(int(x)) for x in mine["orders"]))).fetchall()
        lst = []
        for o in rows:
            v = logic.order_pay(req.con, o["id"], s, closed=True)["by_user"].get(u["id"], {})
            lst.append(order_row(req, o).replace('<div class="side"></div>', '<div class="side"><b>{}</b></div>'.format(money(v.get("work", 0) + v.get("diag", 0)))))
        out.append('<h2>За что начислено</h2><div class="list">{}</div>'.format("".join(lst)))
    out.append('<details class="card more"><summary>Сменить пароль</summary>' + form(req, "/me",
               field("Текущий пароль", "cur", "", "password", 'autocomplete="current-password"')
               + field("Новый пароль", "new", "", "password", 'autocomplete="new-password"')
               + field("Ещё раз", "new2", "", "password", 'autocomplete="new-password"') + '<button class="btn small">Сменить</button>') + '</details>')
    return page(req, "Мои начисления", "".join(out), nav="more", back="/more")


# ================================ АДМИНКА (владелец) ================================
@route("/admin/users", ("GET", "POST"))
def admin_users(req):
    req.need_owner()
    if req.method == "POST":
        act = req.form.get("act")
        uid = to_int(req.form.get("id"), 0)
        name, login_, phone, role = req.f("name", 80), req.f("login", 40).lower(), req.f("phone", 30), req.form.get("role")
        if role not in db.ROLES:
            raise Redirect("/admin/users", "!Выберите роль")
        if not name or not re.match(r"^[a-z0-9._-]{2,40}$", login_):
            raise Redirect("/admin/users", "!Имя обязательно, логин — латиница/цифры, от 2 символов")
        with db.write_lock():
            dup = req.con.execute("SELECT id FROM users WHERE login = ? AND id != ?", (login_, uid)).fetchone()
            if dup:
                raise Redirect("/admin/users", "!Такой логин уже есть")
            if act == "new":
                pw = db.new_password()
                req.con.execute("INSERT INTO users(name, login, phone, role, pass_hash, created) VALUES (?, ?, ?, ?, ?, ?)",
                                (name, login_, phone, role, db.hash_password(pw), db.now()))
                raise Redirect("/admin/users", "Сотрудник добавлен. Логин: {}  Пароль: {}  — передайте ему".format(login_, pw))
            u = req.con.execute("SELECT * FROM users WHERE id = ?", (uid,)).fetchone()
            if not u:
                raise HTTPError(404, "Не найдено")
            active = 1 if req.form.get("active") else 0
            if u["id"] == req.user["id"] and (role != "owner" or not active):
                raise Redirect("/admin/users", "!Себя нельзя отключить или лишить роли владельца")
            req.con.execute("UPDATE users SET name = ?, login = ?, phone = ?, role = ?, active = ? WHERE id = ?",
                            (name, login_, phone, role, active, uid))
            if not active:
                req.con.execute("DELETE FROM sessions WHERE user_id = ?", (uid,))
            if req.form.get("reset"):
                pw = db.new_password()
                req.con.execute("UPDATE users SET pass_hash = ? WHERE id = ?", (db.hash_password(pw), uid))
                req.con.execute("DELETE FROM sessions WHERE user_id = ?", (uid,))
                raise Redirect("/admin/users", "Новый пароль для {}: {}".format(login_, pw))
        raise Redirect("/admin/users", "Сохранено")
    roles = list(db.ROLES.items())
    rows = req.con.execute("SELECT * FROM users ORDER BY active DESC, CASE role WHEN 'owner' THEN 0 WHEN 'chief' THEN 1 ELSE 2 END, name").fetchall()
    out = ['<div class="card"><h3>Новый сотрудник</h3>' + form(req, "/admin/users", '<input type="hidden" name="act" value="new">'
           + '<div class="two">' + field("Имя", "name", "", attrs="required") + field("Логин (латиницей)", "login", "", attrs='required autocapitalize="none" placeholder="azamat"') + '</div>'
           + '<div class="two">' + field("Телефон", "phone", "", "tel", hint="Можно входить и по телефону") + select("Роль", "role", roles, "mech") + '</div>'
           + '<button class="btn main small">Добавить — пароль появится сверху</button>') + '</div>']
    out.append('<p class="hint">Роли: <b>Главный</b> — заказы, касса, склад, назначение мастеров, отчёт. '
               '<b>Диагност, Механик, ВВБ-профи</b> — видят машины, добавляют фото, работы и запчасти, подтверждают участие; цены и кассу не видят. '
               '<b>Владелец</b> — всё, плюс эта админка.</p>')
    for u in rows:
        out.append('<details class="card more"><summary><b>{}</b> <small class="muted">{} · {}</small>{}</summary>{}</details>'.format(
            h(u["name"]), db.ROLES.get(u["role"], ""), h(u["login"]), "" if u["active"] else ' <span class="chip st-cancelled">отключён</span>',
            form(req, "/admin/users", '<input type="hidden" name="act" value="edit"><input type="hidden" name="id" value="{}">'.format(u["id"])
                 + '<div class="two">' + field("Имя", "name", u["name"]) + field("Логин", "login", u["login"]) + '</div>'
                 + '<div class="two">' + field("Телефон", "phone", u["phone"], "tel") + select("Роль", "role", roles, u["role"]) + '</div>'
                 + '<label class="chk"><input type="checkbox" name="active" value="1"{}> Работает (может входить)</label>'.format(" checked" if u["active"] else "")
                 + '<label class="chk"><input type="checkbox" name="reset" value="1"> Выдать новый пароль</label>'
                 + '<button class="btn small">Сохранить</button>')))
    return page(req, "Сотрудники", "".join(out), nav="more", back="/more")


@route("/admin/rates", ("GET", "POST"))
def admin_rates(req):
    req.need_owner()
    if req.method == "POST":
        with db.write_lock():
            for c in req.con.execute("SELECT id FROM work_categories").fetchall():
                cid = c["id"]
                if "name_{}".format(cid) in req.form:
                    name = req.f("name_{}".format(cid), 80)
                    rate = min(max(to_float(req.form.get("rate_{}".format(cid)), 0), 0), 100)
                    active = 1 if req.form.get("active_{}".format(cid)) else 0
                    if name:
                        req.con.execute("UPDATE work_categories SET name = ?, rate = ?, active = ? WHERE id = ?", (name, rate, active, cid))
            nn = req.f("new_name", 80)
            if nn:
                req.con.execute("INSERT INTO work_categories(name, rate, sort) VALUES (?, ?, 99)",
                                (nn, min(max(to_float(req.form.get("new_rate"), 0), 0), 100)))
            db.set_setting(req.con, "diag_rate", min(max(to_float(req.form.get("diag_rate"), 0), 0), 100))
        raise Redirect("/admin/rates", "Проценты сохранены. Закрытые заказы не пересчитываются.")
    s = db.get_settings(req.con)
    cats = req.con.execute("SELECT * FROM work_categories ORDER BY sort, id").fetchall()
    rows = "".join('<div class="rate-row"><input name="name_{id}" value="{n}"><label class="pct"><input name="rate_{id}" value="{r}" inputmode="decimal">%</label>'
                   '<label class="chk"><input type="checkbox" name="active_{id}" value="1"{a}> вкл</label></div>'.format(
                       id=c["id"], n=h(c["name"]), r=qty_fmt(c["rate"]), a=" checked" if c["active"] else "") for c in cats)
    body = form(req, "/admin/rates", (
        '<div class="card"><h3>Процент мастеров с работ</h3>'
        '<p class="hint">У каждой работы в заказе есть вид. Мастера заказа делят между собой этот процент от стоимости работы '
        '(по долям, которые указал главный). Пример: работа 10 000 с, вид «ВВБ» 30% → 3 000 с делят мастера.</p>'
        + rows + '<div class="rate-row"><input name="new_name" placeholder="+ новый вид работы"><label class="pct"><input name="new_rate" inputmode="decimal" placeholder="30">%</label></div></div>'
        '<div class="card"><h3>Процент диагноста</h3><p class="hint">Диагност, назначенный на заказ, получает этот процент от суммы всех работ заказа — отдельно от мастеров.</p>'
        '<label class="pct big"><input name="diag_rate" value="{}" inputmode="decimal">%</label></div>'
        '<button class="btn main big">Сохранить</button>').format(qty_fmt(s.get("diag_rate", 0))))
    return page(req, "Проценты", body, nav="more", back="/more")


@route("/admin/settings", ("GET", "POST"))
def admin_settings(req):
    req.need_owner()
    s = db.get_settings(req.con)
    if req.method == "POST":
        with db.write_lock():
            for k, lim in (("service_name", 80), ("service_phone", 40), ("service_address", 160), ("warranty_period", 60)):
                db.set_setting(req.con, k, req.f(k, lim))
            db.set_setting(req.con, "wallets", {k: (req.f("w_" + k, 40) or s["wallets"][k]) for k in ("cash", "b1", "b2", "b3")})
            tpl = req.ftext("warranty_template", 10000)
            if tpl:
                db.set_setting(req.con, "warranty_template", tpl)
        raise Redirect("/admin/settings", "Сохранено")
    body = form(req, "/admin/settings", (
        '<div class="card"><h3>Кошельки (куда приходят деньги)</h3><div class="two">'
        + "".join(field("Кошелёк {}".format(i + 1) if k != "cash" else "Наличные", "w_" + k, s["wallets"][k],
                        hint="Например: «Мбанк Азамат»" if k == "b1" else "") for i, k in enumerate(("cash", "b1", "b2", "b3")))
        + '</div></div><div class="card"><h3>Сервис (для талона)</h3>'
        + field("Название", "service_name", s.get("service_name", "")) + field("Телефон", "service_phone", s.get("service_phone", ""))
        + field("Адрес", "service_address", s.get("service_address", "")) + '</div>'
        '<div class="card"><h3>Гарантийный талон</h3>' + field("Срок гарантии по умолчанию", "warranty_period", s.get("warranty_period", ""))
        + field("Шаблон текста", "warranty_template", s.get("warranty_template", ""), "textarea", 'rows="14"',
                hint="Подставляется сам: {service} {order} {date} {car} {plate} {vin} {mileage} {client} {works} {parts} {period}")
        + '</div><button class="btn main big">Сохранить</button>'))
    return page(req, "Настройки", body, nav="more", back="/more")


@route("/admin/backup", ("GET", "POST"))
def admin_backup(req):
    req.need_owner()
    if req.method == "POST":
        fd, tmp = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        try:
            db.backup(tmp)
            with open(tmp, "rb") as f:
                data = f.read()
        finally:
            os.unlink(tmp)
        name = "hybrid-doctor-{}.db".format(datetime.now().strftime("%Y%m%d-%H%M"))
        return Response(data, 200, "application/octet-stream", [("Content-Disposition", 'attachment; filename="{}"'.format(name))])
    files = sorted((f for f in os.listdir(db.BACKUP_DIR) if f.endswith(".db")), reverse=True)
    size = os.path.getsize(db.DB_PATH) if os.path.exists(db.DB_PATH) else 0
    body = ('<div class="card"><h3>Скачать базу себе</h3><p class="hint">Один файл со всеми клиентами, заказами, складом и кассой '
            '(без фото). Размер сейчас: {:.1f} МБ. Храните копию на компьютере.</p>{}</div>'
            '<div class="card"><h3>Автоматические копии на сервере</h3><p class="hint">Делаются раз в сутки, хранятся последние 30, '
            'в каталоге hybrid-doctor/data/backups.</p>{}</div>').format(
        size / 1e6, form(req, "/admin/backup", '<button class="btn main">Скачать копию базы</button>'),
        "".join('<div class="lrow"><div class="main">{}</div></div>'.format(h(f)) for f in files[:30]) or '<p class="muted">Пока нет.</p>')
    return page(req, "Резервная копия", body, nav="more", back="/more")
