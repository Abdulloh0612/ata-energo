# -*- coding: utf-8 -*-
"""
База данных Hybrid Doctor (SQLite, один файл data/hybrid.db).
Здесь: структура таблиц, начальные настройки, пароли, копии базы.
"""
import fcntl
import hashlib
import hmac
import json
import os
import secrets
import sqlite3
from contextlib import contextmanager
from datetime import datetime

DATA_DIR = os.environ.get("HD_DATA", os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data"))
DB_PATH = os.path.join(DATA_DIR, "hybrid.db")
PHOTO_DIR = os.path.join(DATA_DIR, "photos")
BACKUP_DIR = os.path.join(DATA_DIR, "backups")

ROLES = {
    "owner": "Владелец",
    "chief": "Главный",
    "diag": "Диагност",
    "mech": "Механик",
    "hvb": "ВВБ-профи",
}
MASTER_ROLES = ("diag", "mech", "hvb", "chief")

STATUSES = {
    "new": "Принят",
    "work": "В ремонте",
    "ready": "Готов",
    "closed": "Выдан",
    "cancelled": "Отменён",
}

SCHEMA = """
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    login TEXT NOT NULL UNIQUE COLLATE NOCASE,
    phone TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL,
    pass_hash TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    created TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf TEXT NOT NULL,
    expires REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS login_attempts (
    ip TEXT NOT NULL,
    created REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS work_categories (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    rate REAL NOT NULL DEFAULT 0,          -- % от стоимости работы, который делят мастера заказа
    active INTEGER NOT NULL DEFAULT 1,
    sort INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS clients (
    id INTEGER PRIMARY KEY,
    last_name TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    created TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cars (
    id INTEGER PRIMARY KEY,
    client_id INTEGER NOT NULL REFERENCES clients(id),
    plate TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    vin TEXT NOT NULL DEFAULT '',
    year TEXT NOT NULL DEFAULT '',
    created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS cars_plate ON cars(plate);

CREATE TABLE IF NOT EXISTS orders (
    id INTEGER PRIMARY KEY,
    client_id INTEGER NOT NULL REFERENCES clients(id),
    car_id INTEGER REFERENCES cars(id),
    status TEXT NOT NULL DEFAULT 'new',
    mileage TEXT NOT NULL DEFAULT '',
    complaint TEXT NOT NULL DEFAULT '',
    diagnosis TEXT NOT NULL DEFAULT '',
    diag_user_id INTEGER REFERENCES users(id),
    created TEXT NOT NULL,
    created_by INTEGER REFERENCES users(id),
    closed_at TEXT,
    debt_ok INTEGER NOT NULL DEFAULT 0,
    diag_rate REAL,                        -- % диагноста, фиксируется при закрытии
    -- гарантийный талон
    w_text TEXT,
    w_period TEXT NOT NULL DEFAULT '',
    w_serials TEXT NOT NULL DEFAULT '',
    w_date TEXT,
    share_token TEXT UNIQUE
);
CREATE INDEX IF NOT EXISTS orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS orders_closed ON orders(closed_at);

CREATE TABLE IF NOT EXISTS parts (
    id INTEGER PRIMARY KEY,
    sku TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    unit TEXT NOT NULL DEFAULT 'шт',
    qty REAL NOT NULL DEFAULT 0,
    cost INTEGER NOT NULL DEFAULT 0,       -- закупочная цена
    price INTEGER NOT NULL DEFAULT 0,      -- цена для клиента
    min_qty REAL NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 1,
    created TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS order_lines (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,                    -- work | part
    name TEXT NOT NULL,
    qty REAL NOT NULL DEFAULT 1,
    price INTEGER NOT NULL DEFAULT 0,
    category_id INTEGER REFERENCES work_categories(id),
    rate REAL,                             -- ставка категории на момент закрытия
    part_id INTEGER REFERENCES parts(id),
    cost INTEGER NOT NULL DEFAULT 0,       -- закупочная цена запчасти на момент добавления
    created TEXT NOT NULL,
    created_by INTEGER REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS assignments (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL DEFAULT 'work',     -- work: делит процент с работ; diag: процент диагноста
    share REAL NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending',-- pending | confirmed | declined
    created TEXT NOT NULL,
    responded TEXT,
    UNIQUE(order_id, user_id, kind)
);

CREATE TABLE IF NOT EXISTS payments (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    amount INTEGER NOT NULL,
    wallet TEXT NOT NULL,                  -- cash | b1 | b2 | b3
    created TEXT NOT NULL,
    user_id INTEGER REFERENCES users(id),
    note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS payments_created ON payments(created);

CREATE TABLE IF NOT EXISTS photos (
    id INTEGER PRIMARY KEY,
    order_id INTEGER REFERENCES orders(id) ON DELETE CASCADE,
    client_id INTEGER REFERENCES clients(id),
    kind TEXT NOT NULL DEFAULT 'work',     -- client | car | work
    file TEXT NOT NULL,
    thumb TEXT NOT NULL DEFAULT '',
    caption TEXT NOT NULL DEFAULT '',
    shared INTEGER NOT NULL DEFAULT 0,     -- показывать клиенту по ссылке гарантии
    created TEXT NOT NULL,
    user_id INTEGER REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS stock_moves (
    id INTEGER PRIMARY KEY,
    part_id INTEGER NOT NULL REFERENCES parts(id),
    delta REAL NOT NULL,
    reason TEXT NOT NULL,                  -- in | order | return | adjust
    order_id INTEGER REFERENCES orders(id),
    cost INTEGER NOT NULL DEFAULT 0,
    price INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created TEXT NOT NULL,
    user_id INTEGER REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS stock_moves_created ON stock_moves(created);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY,
    order_id INTEGER REFERENCES orders(id) ON DELETE CASCADE,
    user_id INTEGER REFERENCES users(id),
    text TEXT NOT NULL,
    created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_order ON events(order_id);
"""

DEFAULT_SETTINGS = {
    "service_name": "Hybrid Doctor",
    "service_phone": "",
    "service_address": "г. Ош",
    "wallets": {"cash": "Наличные", "b1": "Безнал 1", "b2": "Безнал 2", "b3": "Безнал 3"},
    "diag_rate": 5,
    "warranty_period": "3 месяца",
    "warranty_template": (
        "Сервис {service} предоставляет гарантию на выполненные работы и установленные запчасти "
        "по заказу № {order} от {date}.\n\n"
        "Автомобиль: {car}, гос. номер {plate}, VIN {vin}, пробег {mileage}.\n"
        "Клиент: {client}.\n\n"
        "Выполненные работы:\n{works}\n\n"
        "Установленные запчасти:\n{parts}\n\n"
        "Срок гарантии: {period} с даты выдачи автомобиля.\n\n"
        "Гарантия не распространяется на повреждения после ДТП, вмешательства третьих лиц, "
        "нарушения правил эксплуатации и естественный износ. При обращении по гарантии "
        "сохраните этот талон."
    ),
}

DEFAULT_CATEGORIES = [
    ("Диагностика", 30), ("Механика", 30), ("ВВБ (высоковольтная батарея)", 30),
    ("Электрика", 30), ("Прочее", 30),
]

@contextmanager
def write_lock():
    with open(os.path.join(DATA_DIR, ".write.lock"), "ab") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def now():
    return datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def connect():
    con = sqlite3.connect(DB_PATH, timeout=15, isolation_level=None)
    con.row_factory = sqlite3.Row
    con.execute("PRAGMA foreign_keys = ON")
    con.execute("PRAGMA busy_timeout = 15000")
    return con


def init():
    os.makedirs(DATA_DIR, exist_ok=True)
    os.makedirs(PHOTO_DIR, exist_ok=True)
    os.makedirs(BACKUP_DIR, exist_ok=True)
    con = connect()
    con.execute("PRAGMA journal_mode = WAL")
    con.executescript(SCHEMA)
    for k, v in DEFAULT_SETTINGS.items():
        con.execute("INSERT OR IGNORE INTO settings(key, value) VALUES (?, ?)", (k, json.dumps(v, ensure_ascii=False)))
    if not con.execute("SELECT 1 FROM work_categories LIMIT 1").fetchone():
        for i, (name, rate) in enumerate(DEFAULT_CATEGORIES):
            con.execute("INSERT INTO work_categories(name, rate, sort) VALUES (?, ?, ?)", (name, rate, i))
    con.close()


def get_settings(con):
    out = dict(DEFAULT_SETTINGS)
    for r in con.execute("SELECT key, value FROM settings"):
        try:
            out[r["key"]] = json.loads(r["value"])
        except ValueError:
            pass
    w = dict(DEFAULT_SETTINGS["wallets"])
    w.update(out.get("wallets") or {})
    out["wallets"] = w
    return out


def set_setting(con, key, value):
    con.execute("INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
                (key, json.dumps(value, ensure_ascii=False)))


# ------------------------------- пароли -------------------------------
def hash_password(pw):
    salt = secrets.token_hex(16)
    dk = hashlib.pbkdf2_hmac("sha256", pw.encode("utf-8"), salt.encode(), 240_000)
    return "pbkdf2${}${}".format(salt, dk.hex())


def check_password(pw, stored):
    try:
        _, salt, h = stored.split("$")
    except ValueError:
        return False
    dk = hashlib.pbkdf2_hmac("sha256", pw.encode("utf-8"), salt.encode(), 240_000)
    return hmac.compare_digest(dk.hex(), h)


def new_password():
    alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
    return "".join(secrets.choice(alphabet) for _ in range(10))


# ------------------------------- копии -------------------------------
def backup(dest=None):
    """Копия базы средствами SQLite (безопасно во время работы)."""
    os.makedirs(BACKUP_DIR, exist_ok=True)
    if dest is None:
        dest = os.path.join(BACKUP_DIR, "hybrid-{}.db".format(datetime.now().strftime("%Y%m%d-%H%M")))
    src = connect()
    dst = sqlite3.connect(dest)
    with dst:
        src.backup(dst)
    dst.close()
    src.close()
    return dest


def rotate_backups(keep=30):
    files = sorted(f for f in os.listdir(BACKUP_DIR) if f.startswith("hybrid-") and f.endswith(".db"))
    for f in files[:-keep]:
        try:
            os.unlink(os.path.join(BACKUP_DIR, f))
        except OSError:
            pass


def backup_daily():
    """Создаёт суточную копию базы и сохраняет последние 30 копий."""
    today = datetime.now().strftime("%Y%m%d")
    if not any(f.startswith("hybrid-" + today) for f in os.listdir(BACKUP_DIR)):
        backup()
        rotate_backups()
