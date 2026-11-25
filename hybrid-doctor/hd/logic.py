# -*- coding: utf-8 -*-
"""Расчёты: суммы заказа, оплата, списание со склада, зарплата мастеров."""
from . import db


def log(con, order_id, user_id, text):
    con.execute("INSERT INTO events(order_id, user_id, text, created) VALUES (?, ?, ?, ?)",
                (order_id, user_id, text, db.now()))


def order_totals(con, oid):
    r = con.execute(
        "SELECT COALESCE(SUM(CASE WHEN kind='work' THEN qty*price END),0) AS works, "
        "COALESCE(SUM(CASE WHEN kind='part' THEN qty*price END),0) AS parts, "
        "COALESCE(SUM(CASE WHEN kind='part' THEN qty*cost END),0) AS parts_cost "
        "FROM order_lines WHERE order_id = ?", (oid,)).fetchone()
    paid = con.execute("SELECT COALESCE(SUM(amount),0) FROM payments WHERE order_id = ?", (oid,)).fetchone()[0]
    works, parts = int(round(r["works"])), int(round(r["parts"]))
    total = works + parts
    return {"works": works, "parts": parts, "parts_cost": int(round(r["parts_cost"])),
            "total": total, "paid": int(paid), "due": total - int(paid)}


def close_order(con, order, user_id):
    """Закрытие заказа: фиксируем ставки работ и списываем запчасти со склада."""
    oid = order["id"]
    for ln in con.execute("SELECT * FROM order_lines WHERE order_id = ?", (oid,)).fetchall():
        if ln["kind"] == "work":
            rate = None
            if ln["category_id"]:
                c = con.execute("SELECT rate FROM work_categories WHERE id = ?", (ln["category_id"],)).fetchone()
                rate = c["rate"] if c else None
            con.execute("UPDATE order_lines SET rate = ? WHERE id = ?", (rate if rate is not None else 0, ln["id"]))
        elif ln["kind"] == "part" and ln["part_id"]:
            con.execute("UPDATE parts SET qty = qty - ? WHERE id = ?", (ln["qty"], ln["part_id"]))
            con.execute("INSERT INTO stock_moves(part_id, delta, reason, order_id, cost, price, created, user_id) "
                        "VALUES (?, ?, 'order', ?, ?, ?, ?, ?)",
                        (ln["part_id"], -ln["qty"], oid, ln["cost"], ln["price"], db.now(), user_id))
    s = db.get_settings(con)
    con.execute("UPDATE orders SET status = 'closed', closed_at = ?, w_date = COALESCE(w_date, ?), diag_rate = ?, "
                "w_period = CASE WHEN w_period = '' THEN ? ELSE w_period END WHERE id = ?",
                (db.now(), db.now()[:10], float(s.get("diag_rate", 0) or 0), str(s.get("warranty_period", "")), oid))


def reopen_order(con, order, user_id):
    """Возврат закрытого заказа в работу: запчасти возвращаются на склад."""
    oid = order["id"]
    for mv in con.execute("SELECT part_id, SUM(delta) AS d FROM stock_moves WHERE order_id = ? "
                          "AND reason IN ('order', 'return') GROUP BY part_id", (oid,)).fetchall():
        if mv["d"]:
            con.execute("UPDATE parts SET qty = qty - ? WHERE id = ?", (mv["d"], mv["part_id"]))
            con.execute("INSERT INTO stock_moves(part_id, delta, reason, order_id, created, user_id, note) "
                        "VALUES (?, ?, 'return', ?, ?, ?, 'заказ открыт заново')",
                        (mv["part_id"], -mv["d"], oid, db.now(), user_id))
    con.execute("UPDATE orders SET status = 'ready', closed_at = NULL, diag_rate = NULL WHERE id = ?", (oid,))


def order_diag_rate(con, oid, settings=None):
    r = con.execute("SELECT diag_rate FROM orders WHERE id = ?", (oid,)).fetchone()
    if r and r["diag_rate"] is not None:
        return float(r["diag_rate"])
    s = settings or db.get_settings(con)
    return float(s.get("diag_rate", 0) or 0)


def order_pay(con, oid, settings=None, closed=None):
    """
    Начисления мастерам по заказу.
    Фонд мастеров = сумма по работам × ставка категории работы. Делят все подтвердившие
    участники (kind='work') пропорционально долям.
    Диагност (kind='diag') получает отдельный % от суммы всех работ заказа.
    Возвращает {user_id: {"work": сумма, "diag": сумма}}, нераспределённый фонд, фонд, и кто ещё не подтвердил.
    """
    o = con.execute("SELECT status FROM orders WHERE id = ?", (oid,)).fetchone()
    closed = (o and o["status"] == "closed") if closed is None else closed
    pool = 0.0
    works_total = 0.0
    for ln in con.execute("SELECT l.qty, l.price, l.rate, c.rate AS crate FROM order_lines l "
                          "LEFT JOIN work_categories c ON c.id = l.category_id "
                          "WHERE l.order_id = ? AND l.kind = 'work'", (oid,)):
        amount = ln["qty"] * ln["price"]
        rate = ln["rate"] if (closed and ln["rate"] is not None) else (ln["crate"] or 0)
        pool += amount * rate / 100.0
        works_total += amount
    diag_pool = works_total * order_diag_rate(con, oid, settings) / 100.0
    res = {}
    asg = con.execute("SELECT * FROM assignments WHERE order_id = ?", (oid,)).fetchall()
    workers = [a for a in asg if a["kind"] == "work" and a["status"] == "confirmed"]
    total_share = sum(max(a["share"], 0) for a in workers)
    unassigned = 0.0
    if workers and total_share > 0:
        for a in workers:
            res.setdefault(a["user_id"], {"work": 0.0, "diag": 0.0})["work"] += pool * max(a["share"], 0) / total_share
    else:
        unassigned += pool
    diags = [a for a in asg if a["kind"] == "diag" and a["status"] == "confirmed"]
    if diags:
        for a in diags:
            res.setdefault(a["user_id"], {"work": 0.0, "diag": 0.0})["diag"] += diag_pool / len(diags)
    elif any(a["kind"] == "diag" for a in asg):
        unassigned += diag_pool
    pending = [a for a in asg if a["status"] == "pending"]
    return {"by_user": res, "unassigned": unassigned, "pool": pool, "diag_pool": diag_pool, "pending": pending}


def payroll(con, date_from, date_to):
    """Начисления по закрытым в периоде заказам (даты включительно, 'YYYY-MM-DD')."""
    s = db.get_settings(con)
    users = {}
    unassigned = 0.0
    pending_orders = set()
    for o in con.execute("SELECT id FROM orders WHERE status = 'closed' AND substr(closed_at, 1, 10) BETWEEN ? AND ?",
                         (date_from, date_to)).fetchall():
        p = order_pay(con, o["id"], s, closed=True)
        unassigned += p["unassigned"]
        if p["pending"]:
            pending_orders.add(o["id"])
        for uid, v in p["by_user"].items():
            u = users.setdefault(uid, {"work": 0.0, "diag": 0.0, "orders": set()})
            u["work"] += v["work"]
            u["diag"] += v["diag"]
            u["orders"].add(o["id"])
    return users, unassigned, pending_orders
