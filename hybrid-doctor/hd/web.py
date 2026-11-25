# -*- coding: utf-8 -*-
"""
Мини-веб-фреймворк Hybrid Doctor на стандартной библиотеке Python:
запросы, маршруты, вход сотрудников, защита форм (CSRF), разбор фото из форм.
"""
import html
import json
import re
import secrets
import time
import urllib.parse
from email.parser import BytesParser
from email.policy import HTTP

from . import db

SESSION_DAYS = 30
COOKIE = "hd_s"
MOUNT = "/hybrid-doctor"
SECURE_COOKIE = False      # включается из app.py (HD_SECURE_COOKIE=1)
MAX_FORM = 2_000_000
MAX_UPLOAD = 25_000_000


class HTTPError(Exception):
    def __init__(self, code, message=""):
        super().__init__(message)
        self.code = code
        self.message = message


class Redirect(Exception):
    def __init__(self, url, flash=None):
        super().__init__(url)
        self.url = url
        self.flash = flash


class Response:
    def __init__(self, body=b"", status=200, ctype="text/html; charset=utf-8", headers=None):
        if isinstance(body, str):
            body = body.encode("utf-8")
        self.body = body
        self.status = status
        self.headers = [("Content-Type", ctype)] + list(headers or [])


def h(v):
    """Экранирование текста для HTML."""
    return html.escape("" if v is None else str(v), quote=True)


def money(v):
    try:
        v = int(round(float(v or 0)))
    except (TypeError, ValueError):
        v = 0
    s = "{:,}".format(abs(v)).replace(",", " ")
    return ("−" if v < 0 else "") + s + " с"


def qty_fmt(v):
    v = float(v or 0)
    return str(int(v)) if v == int(v) else ("%.2f" % v).rstrip("0").rstrip(".")


def to_int(v, default=0):
    try:
        return int(round(float(str(v).replace(",", ".").replace(" ", "").replace(" ", ""))))
    except (TypeError, ValueError):
        return default


def to_float(v, default=0.0):
    try:
        return float(str(v).replace(",", ".").replace(" ", ""))
    except (TypeError, ValueError):
        return default


def clean(v, limit=500):
    return re.sub(r"[ \t]+", " ", str(v or "").replace("\r", "")).strip()[:limit]


def fdate(s, with_time=True):
    """2026-11-18 10:22:00 -> 18.11.2026 10:22"""
    if not s:
        return ""
    d = s[8:10] + "." + s[5:7] + "." + s[0:4]
    return d + (" " + s[11:16] if with_time and len(s) > 11 else "")


class Request:
    def __init__(self, handler, method):
        self.handler = handler
        self.method = method
        u = urllib.parse.urlsplit(handler.path)
        self.path = urllib.parse.unquote(u.path[len(MOUNT):])
        self.query = {k: v[-1] for k, v in urllib.parse.parse_qs(u.query, keep_blank_values=True).items()}
        self.form = {}
        self.form_lists = {}
        self.files = {}
        self.cookies = {}
        for part in (handler.headers.get("Cookie") or "").split(";"):
            k, _, v = part.strip().partition("=")
            if k:
                self.cookies[k] = urllib.parse.unquote(v)
        self.user = None
        self.session = None
        self.con = None
        self.set_cookies = []
        self.ip = handler.headers.get("X-Real-IP") or handler.client_address[0]

    # ------------- тело запроса -------------
    def read_body(self):
        if self.method != "POST":
            return
        size = int(self.handler.headers.get("Content-Length") or 0)
        ctype = self.handler.headers.get("Content-Type") or ""
        limit = MAX_UPLOAD if ctype.startswith("multipart/") else MAX_FORM
        if size > limit:
            raise HTTPError(413, "Слишком большой запрос")
        raw = self.handler.rfile.read(size) if size > 0 else b""
        if ctype.startswith("application/x-www-form-urlencoded"):
            pairs = urllib.parse.parse_qsl(raw.decode("utf-8", "replace"), keep_blank_values=True)
            for k, v in pairs:
                self.form[k] = v
                self.form_lists.setdefault(k, []).append(v)
        elif ctype.startswith("multipart/form-data"):
            msg = BytesParser(policy=HTTP).parsebytes(
                b"Content-Type: " + ctype.encode("latin-1") + b"\r\nMIME-Version: 1.0\r\n\r\n" + raw)
            if msg.is_multipart():
                for part in msg.iter_parts():
                    name = part.get_param("name", header="content-disposition")
                    if not name:
                        continue
                    filename = part.get_filename()
                    data = part.get_payload(decode=True) or b""
                    if filename is not None:
                        self.files.setdefault(name, []).append({"filename": filename, "data": data,
                                                                 "type": part.get_content_type()})
                    else:
                        v = data.decode(part.get_content_charset() or "utf-8", "replace")
                        self.form[name] = v
                        self.form_lists.setdefault(name, []).append(v)

    def f(self, name, limit=500):
        return clean(self.form.get(name, ""), limit)

    def ftext(self, name, limit=5000):
        return str(self.form.get(name, "")).replace("\r", "").strip()[:limit]

    def flist(self, name):
        return self.form_lists.get(name, [])

    # ------------- сообщения после действия -------------
    def flash(self):
        v = self.cookies.get("hd_flash")
        if v:
            self.set_cookies.append("hd_flash=; Path={}/; Max-Age=0".format(MOUNT))
        return v

    # ------------- права -------------
    def role(self):
        return self.user["role"] if self.user else None

    def is_boss(self):
        """Владелец или главный: деньги, цены, касса, склад."""
        return self.role() in ("owner", "chief")

    def is_owner(self):
        return self.role() == "owner"

    def need_boss(self):
        if not self.is_boss():
            raise HTTPError(403, "Недостаточно прав")

    def need_owner(self):
        if not self.is_owner():
            raise HTTPError(403, "Только для владельца")


# ------------------------------- вход -------------------------------
def load_session(req):
    tok = req.cookies.get(COOKIE)
    if not tok:
        return
    r = req.con.execute("SELECT s.*, u.id AS uid, u.name, u.login, u.role, u.phone, u.active "
                        "FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token = ?", (tok,)).fetchone()
    if not r or r["expires"] < time.time() or not r["active"]:
        return
    req.session = r
    req.user = {"id": r["uid"], "name": r["name"], "login": r["login"], "role": r["role"], "phone": r["phone"]}


def start_session(req, user_id):
    tok = secrets.token_urlsafe(32)
    csrf = secrets.token_urlsafe(24)
    with db.write_lock():
        req.con.execute("DELETE FROM sessions WHERE expires < ?", (time.time(),))
        req.con.execute("INSERT INTO sessions(token, user_id, csrf, expires) VALUES (?, ?, ?, ?)",
                        (tok, user_id, csrf, time.time() + SESSION_DAYS * 86400))
    req.set_cookies.append(cookie(COOKIE, tok, SESSION_DAYS * 86400))


def end_session(req):
    if req.session:
        req.con.execute("DELETE FROM sessions WHERE token = ?", (req.session["token"],))
    req.set_cookies.append(cookie(COOKIE, "", 0))


def cookie(name, value, max_age):
    c = "{}={}; Path={}/; HttpOnly; SameSite=Lax; Max-Age={}".format(name, urllib.parse.quote(value), MOUNT, max_age)
    return c + ("; Secure" if SECURE_COOKIE else "")


def csrf_field(req):
    return '<input type="hidden" name="_csrf" value="{}">'.format(h(req.session["csrf"])) if req.session else ""


def check_csrf(req):
    sent = req.form.get("_csrf") or req.handler.headers.get("X-CSRF") or ""
    if not req.session or not secrets.compare_digest(sent, req.session["csrf"]):
        raise HTTPError(403, "Страница устарела — обновите её и повторите.")


# ------------------------------- маршруты -------------------------------
ROUTES = []


def route(pattern, methods=("GET",), public=False):
    rx = re.compile("^" + pattern + "$")

    def deco(fn):
        ROUTES.append((rx, tuple(methods), public, fn))
        return fn
    return deco


def dispatch(req):
    for rx, methods, public, fn in ROUTES:
        m = rx.match(req.path)
        if not m:
            continue
        if req.method not in methods:
            raise HTTPError(405, "Метод не поддерживается")
        if not public:
            if not req.user:
                if req.method == "GET":
                    raise Redirect("/login?next=" + urllib.parse.quote(req.path))
                raise HTTPError(401, "Нужно войти")
            if req.method == "POST":
                check_csrf(req)
        return fn(req, *m.groups())
    raise HTTPError(404, "Страница не найдена")


def json_response(obj, status=200):
    return Response(json.dumps(obj, ensure_ascii=False), status, "application/json; charset=utf-8")
