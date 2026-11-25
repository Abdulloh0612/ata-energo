# -*- coding: utf-8 -*-
"""Оформление страниц: общий каркас, меню, мелкие элементы форм."""
import re

from .web import MOUNT, h, csrf_field, Response
from . import db

VERSION = "1.0"

ICONS = {
    "home": '<path d="M3 11l9-7 9 7v9a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z"/>',
    "orders": '<rect x="5" y="3" width="14" height="18" rx="2"/><path d="M9 8h6M9 12h6M9 16h4"/>',
    "clients": '<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20c.8-3.6 3.3-5.5 6.5-5.5s5.7 1.9 6.5 5.5"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7M18 14.8c2 .7 3.2 2.5 3.6 5.2"/>',
    "stock": '<path d="M3 8l9-5 9 5v8l-9 5-9-5z"/><path d="M3 8l9 5 9-5M12 13v8"/>',
    "more": '<circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/>',
}


def icon(name):
    return '<svg viewBox="0 0 24 24" aria-hidden="true">{}</svg>'.format(ICONS.get(name, ""))


def pending_count(req):
    if not req.user:
        return 0
    return req.con.execute("SELECT COUNT(*) FROM assignments a JOIN orders o ON o.id = a.order_id "
                           "WHERE a.user_id = ? AND a.status = 'pending' AND o.status != 'cancelled'",
                           (req.user["id"],)).fetchone()[0]


def page(req, title, body, nav="", back=None, actions=""):
    flash = req.flash() if req.user is not None or True else None
    fl = ""
    if flash:
        bad = flash.startswith("!")
        fl = '<div class="flash{}">{}</div>'.format(" bad" if bad else "", h(flash.lstrip("!")))
    pend = pending_count(req)
    tabs = [("home", "/", "Главная"), ("orders", "/orders", "Заказы"), ("clients", "/clients", "Клиенты"),
            ("stock", "/stock", "Склад"), ("more", "/more", "Ещё")]
    navhtml = ""
    if req.user:
        items = []
        for key, url, label in tabs:
            badge = '<i class="badge">{}</i>'.format(pend) if key == "home" and pend else ""
            items.append('<a href="{}" class="{}">{}{}<span>{}</span></a>'.format(
                url, "on" if key == nav else "", icon(key), badge, label))
        navhtml = '<nav class="tabbar">{}</nav>'.format("".join(items))
    backhtml = '<a class="back" href="{}" aria-label="Назад">‹</a>'.format(h(back)) if back else ""
    csrf_meta = '<meta name="csrf" content="{}">'.format(h(req.session["csrf"])) if req.session else ""
    doc = """<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#0B3B36">
<meta name="robots" content="noindex, nofollow">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-title" content="Hybrid Doctor">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
{csrf}
<title>{title} — Hybrid Doctor</title>
<link rel="manifest" href="/manifest.webmanifest">
<link rel="icon" href="/static/icons/icon-192.png">
<link rel="apple-touch-icon" href="/static/icons/apple-touch-icon.png">
<link rel="stylesheet" href="/static/style.css?v={v}">
</head>
<body>
<header class="top">{back}<h1>{title}</h1><div class="acts">{actions}</div></header>
<main>{flash}{body}</main>
{nav}
<script src="/static/app.js?v={v}" defer></script>
</body>
</html>""".format(csrf=csrf_meta, title=h(title), v=VERSION, back=backhtml, actions=actions,
                  flash=fl, body=body, nav=navhtml)
    doc = re.sub(r'( (?:href|src|action)="/)(?!/)', lambda m: m[0][:-1] + MOUNT + "/", doc)
    if req.user:
        doc = doc.replace('<div class="acts">', '<div class="acts"><a class="hbtn" href="/">Ата Энерго</a>')
    return Response(doc, headers=[("Cache-Control", "no-store")])


def form(req, action, inner, cls="", attrs=""):
    return '<form method="post" action="{}" class="{}" {}>{}{}</form>'.format(
        h(action), cls, attrs, csrf_field(req), inner)


def field(label, name, value="", typ="text", attrs="", hint=""):
    hint_html = '<small>{}</small>'.format(h(hint)) if hint else ""
    if typ == "textarea":
        inp = '<textarea name="{}" rows="3" {}>{}</textarea>'.format(name, attrs, h(value))
    else:
        inp = '<input type="{}" name="{}" value="{}" {}>'.format(typ, name, h(value), attrs)
    return '<label class="f"><span>{}</span>{}{}</label>'.format(h(label), inp, hint_html)


def select(label, name, options, value="", attrs=""):
    opts = "".join('<option value="{}"{}>{}</option>'.format(
        h(k), " selected" if str(k) == str(value) else "", h(v)) for k, v in options)
    return '<label class="f"><span>{}</span><select name="{}" {}>{}</select></label>'.format(h(label), name, attrs, opts)


def status_chip(st):
    return '<span class="chip st-{}">{}</span>'.format(h(st), h(db.STATUSES.get(st, st)))


def order_no(oid):
    return "№ {}".format(1000 + int(oid))


def client_no(cid):
    return "K-{:04d}".format(int(cid))


def client_name(r):
    return (" ".join(x for x in (r["last_name"], r["first_name"]) if x)).strip() or "Без имени"


def empty(text):
    return '<p class="empty">{}</p>'.format(h(text))
