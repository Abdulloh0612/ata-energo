"""Обработчики модуля автосервиса и команды управления его базой."""
import os
import sys
import traceback
import urllib.parse
from http import HTTPStatus

BASE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, BASE)

from hd import db, web, ui            # noqa: E402
from hd import views_orders, views_more  # noqa: E402,F401  (регистрируют страницы)

web.SECURE_COOKIE = os.environ.get("HD_SECURE_COOKIE") == "1"
STATIC = os.path.join(BASE, "static")
TYPES = {".css": "text/css; charset=utf-8", ".js": "application/javascript; charset=utf-8", ".png": "image/png",
         ".svg": "image/svg+xml", ".ico": "image/x-icon", ".webmanifest": "application/manifest+json", ".json": "application/json", ".html": "text/html; charset=utf-8"}


def error_page(req, code, message):
    title = {404: "Не найдено", 403: "Нет доступа", 401: "Нужно войти"}.get(code, "Ошибка")
    body = '<div class="card"><p>{}</p><a class="btn" href="/">На главную</a></div>'.format(web.h(message or title))
    try:
        r = ui.page(req, title, body)
    except Exception:
        r = web.Response("<h1>{}</h1>".format(web.h(title)))
    r.status = code
    return r


class Handler:
    def __init__(self):
        self.path = os.environ["REQUEST_URI"]
        self.client_address = (os.environ.get("REMOTE_ADDR", ""),)
        self.headers = {
            "Cookie": os.environ.get("HTTP_COOKIE"),
            "Content-Type": os.environ.get("CONTENT_TYPE"),
            "Content-Length": os.environ.get("CONTENT_LENGTH"),
            "Host": os.environ.get("HTTP_HOST"),
            "X-Forwarded-Proto": os.environ.get("HTTP_X_FORWARDED_PROTO"),
            "X-Real-IP": os.environ.get("HTTP_X_REAL_IP"),
            "X-CSRF": os.environ.get("HTTP_X_CSRF"),
            "X-Requested-With": os.environ.get("HTTP_X_REQUESTED_WITH"),
        }
        self.rfile = sys.stdin.buffer

    def static(self, path):
        if path in ("/manifest.webmanifest", "/sw.js"):
            path = "/static" + path
        rel = path[len("/static/"):]
        full = os.path.realpath(os.path.join(STATIC, rel))
        if not full.startswith(os.path.realpath(STATIC) + os.sep) or not os.path.isfile(full):
            return web.Response("Not found", 404, "text/plain; charset=utf-8")
        with open(full, "rb") as f:
            data = f.read()
        ext = os.path.splitext(full)[1]
        cache = "no-cache" if rel in ("sw.js", "manifest.webmanifest") else "public, max-age=86400"
        hdrs = [("Cache-Control", cache)]
        if rel == "sw.js":
            hdrs.append(("Service-Worker-Allowed", web.MOUNT + "/"))
        return web.Response(data, 200, TYPES.get(ext, "application/octet-stream"), hdrs)

    def handle_any(self, method, head=False):
        path = urllib.parse.urlsplit(self.path).path
        if path == web.MOUNT:
            return self.send(web.Response(b"", 308, headers=[("Location", web.MOUNT + "/")]), head)
        if not path.startswith(web.MOUNT + "/"):
            return self.send(web.Response("Not found", 404), head)
        path = path[len(web.MOUNT):]
        if path.startswith("/static/") or path in ("/manifest.webmanifest", "/sw.js"):
            return self.send(self.static(path), head)
        req = web.Request(self, method)
        con = None
        try:
            con = db.connect()
            req.con = con
            web.load_session(req)
            req.read_body()
            resp = web.dispatch(req)
        except web.Redirect as r:
            resp = web.Response(b"", 303, headers=[("Location", web.MOUNT + r.url)])
            if r.flash:
                req.set_cookies.append("hd_flash={}; Path={}/; Max-Age=60; HttpOnly; SameSite=Lax{}".format(urllib.parse.quote(r.flash), web.MOUNT, "; Secure" if web.SECURE_COOKIE else ""))
        except web.HTTPError as e:
            resp = error_page(req, e.code, e.message)
        except Exception:
            traceback.print_exc()
            resp = error_page(req, 500, "Что-то пошло не так. Попробуйте ещё раз; если повторится — сообщите владельцу.")
        finally:
            if con is not None:
                try:
                    if con.in_transaction:
                        con.execute("ROLLBACK")
                except Exception:
                    pass
        try:
            self.send(resp, head, req.set_cookies)
        finally:
            if con is not None:
                con.close()

    def send(self, resp, head=False, cookies=()):
        headers = [("Status", f"{resp.status} {HTTPStatus(resp.status).phrase}")]
        headers.extend(resp.headers)
        headers.extend(("Set-Cookie", cookie) for cookie in cookies)
        headers.extend([
            ("X-Content-Type-Options", "nosniff"),
            ("X-Frame-Options", "DENY"),
            ("Referrer-Policy", "same-origin"),
            ("Content-Length", str(len(resp.body))),
        ])
        output = sys.stdout.buffer
        output.write(("".join(f"{k}: {v}\r\n" for k, v in headers) + "\r\n").encode("utf-8"))
        if not head:
            output.write(resp.body)


def cli(args):
    if args[0] == "backup-daily":
        db.backup_daily()
        return 0
    db.init()
    if args[0] == "init":
        return 0
    con = db.connect()
    if args[0] == "create-owner":
        if len(args) < 3:
            print('Использование: python3 app.py create-owner "Имя" логин')
            return 1
        name, login = args[1], args[2].lower()
        if con.execute("SELECT 1 FROM users WHERE login = ?", (login,)).fetchone():
            print("Логин {} уже есть. Новый пароль: python3 app.py reset-password {}".format(login, login))
            return 1
        pw = db.new_password()
        con.execute("INSERT INTO users(name, login, role, pass_hash, created) VALUES (?, ?, 'owner', ?, ?)",
                    (name, login, db.hash_password(pw), db.now()))
        print("Владелец создан.  Логин: {}   Пароль: {}".format(login, pw))
        return 0
    if args[0] == "reset-password":
        if len(args) < 2:
            print("Использование: python3 app.py reset-password логин")
            return 1
        pw = db.new_password()
        cur = con.execute("UPDATE users SET pass_hash = ?, active = 1 WHERE login = ?", (db.hash_password(pw), args[1].lower()))
        if not cur.rowcount:
            print("Нет такого логина. Сотрудники:", ", ".join(r[0] for r in con.execute("SELECT login FROM users")))
            return 1
        con.execute("DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE login = ?)", (args[1].lower(),))
        print("Новый пароль для {}: {}".format(args[1], pw))
        return 0
    if args[0] == "has-users":
        return 0 if con.execute("SELECT 1 FROM users LIMIT 1").fetchone() else 1
    if args[0] == "backup":
        print("Копия:", db.backup())
        return 0
    print(__doc__)
    return 1


if __name__ == "__main__":
    if len(sys.argv) > 1:
        sys.exit(cli(sys.argv[1:]))
    method = os.environ.get("REQUEST_METHOD")
    handler = Handler()
    if method == "HEAD":
        handler.handle_any("GET", head=True)
    elif method in ("GET", "POST"):
        handler.handle_any(method)
    else:
        handler.send(web.Response("Method not allowed", 405, headers=[("Allow", "GET, HEAD, POST")]))
