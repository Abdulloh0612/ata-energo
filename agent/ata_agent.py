"""Локальный агент "Ата Энерго".

Устанавливается на компьютер / мини-ПК / Raspberry Pi В СЕТИ КЛИЕНТА (там, где инвертор).
Сервер может быть где угодно в интернете — агент сам ходит к нему по HTTPS.

Что делает:
  1) раз в 3 сек спрашивает у сервера задачи (поиск Wi-Fi/Bluetooth, проверка устройства);
  2) выполняет их по-настоящему (scan_wifi.py, scan_bluetooth.py) и отправляет результат;
  3) для подключённых устройств регулярно читает показания (protocols.py)
     и отправляет их на сервер -> они появляются в мониторинге.

Первый запуск (токен выдаётся командой make agent на сервере):
    python ata_agent.py --server https://monitoring.example.org --token ТОКЕН
Дальше достаточно:  python ata_agent.py   (настройки сохраняются в agent_config.json)
"""
import argparse
import json
import platform
import socket
import ssl
import sys
import threading
import time
import traceback
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from protocols import read_payload  # noqa: E402
from scan_wifi import local_ip, probe_device, scan_network, solarman_broadcast, network_for  # noqa: E402

VERSION = "1.0"
CONFIG_FILE = HERE / "agent_config.json"
POLL_SECONDS = 3


class Api:
    """Связь с сервером (JSON по HTTP/HTTPS, токен в заголовке X-Agent-Token)."""

    def __init__(self, server, token):
        """Запоминает адрес и токен. HTTPS-сертификат проверяется всегда."""
        self.server = server.rstrip("/")
        self.token = token
        self.ssl = ssl.create_default_context()

    def call(self, method, path, data=None):
        """Отправляет запрос и возвращает JSON-ответ."""
        body = json.dumps(data).encode() if data is not None else None
        req = urllib.request.Request(self.server + path, data=body, method=method, headers={
            "X-Agent-Token": self.token, "Content-Type": "application/json", "Accept": "application/json",
            "User-Agent": f"AtaEnergoAgent/{VERSION}",
        })
        with urllib.request.urlopen(req, timeout=20, context=self.ssl if self.server.startswith("https") else None) as resp:
            return json.loads(resp.read() or b"{}")


def load_config(args):
    """Берёт настройки из аргументов или из agent_config.json и сохраняет их."""
    cfg = {}
    if CONFIG_FILE.exists():
        cfg = json.loads(CONFIG_FILE.read_text(encoding="utf-8"))
    if args.server:
        cfg["server"] = args.server
    if args.token:
        cfg["token"] = args.token
    if not cfg.get("server") or not cfg.get("token"):
        sys.exit("Укажите --server и --token (токен выдаётся командой make agent на сервере).")
    CONFIG_FILE.write_text(json.dumps(cfg, ensure_ascii=False, indent=2), encoding="utf-8")
    host = cfg["server"].split("//")[-1].split("/")[0].split(":")[0]
    if cfg["server"].startswith("http://") and host not in ("127.0.0.1", "localhost") and not host.startswith("192.168."):
        print("ВНИМАНИЕ: сервер без HTTPS — данные и токен идут незашифрованными. Используйте https://")
    return cfg


def capabilities():
    """Что умеет этот агент (сообщается серверу)."""
    from scan_bluetooth import bluetooth_status

    caps = {"wifi": {"available": True}, "bluetooth": bluetooth_status()}
    try:
        import zeroconf  # noqa: F401
        caps["mdns"] = True
    except ImportError:
        caps["mdns"] = False
    try:
        import pysolarmanv5  # noqa: F401
        caps["solarman"] = True
    except ImportError:
        caps["solarman"] = False
    return caps


class Agent:
    """Главный цикл агента."""

    def __init__(self, api):
        """Создаёт агента."""
        self.api = api
        self.pool = ThreadPoolExecutor(max_workers=4)
        self.running_tasks = set()
        self.next_poll = {}       # serial -> когда опрашивать
        self.busy = set()         # serial, которые сейчас читаются
        self.ip_override = {}     # serial -> новый IP логгера (если DHCP его поменял)
        self.lock = threading.Lock()

    # ---------- задачи ----------
    def run_task(self, task):
        """Выполняет задачу сервера и отправляет результат."""
        tid = task["id"]

        def log(text, found=None):
            print(f"[задача {tid}] {text}")
            try:
                self.api.call("POST", f"/api/agent/tasks/{tid}/progress/", {"log": text, "found": found or []})
            except Exception as exc:
                print("  не удалось отправить прогресс:", exc)

        try:
            if task["kind"] == "scan":
                found, errors = [], []
                if task["method"] in ("wifi", "all"):
                    try:
                        found += scan_network(task.get("params") or {}, log)
                    except Exception as exc:
                        errors.append(f"Wi-Fi/LAN: {exc}")
                        log(f"Ошибка поиска Wi-Fi/LAN: {exc}")
                if task["method"] in ("bluetooth", "all"):
                    from scan_bluetooth import bluetooth_status, scan_ble
                    status = bluetooth_status()
                    if status["available"]:
                        try:
                            found += scan_ble(log)
                        except Exception as exc:
                            errors.append(f"Bluetooth: {exc}")
                            log(f"Ошибка поиска Bluetooth: {exc}")
                    else:
                        errors.append(f"Bluetooth: {status['reason']}")
                        log(f"Bluetooth пропущен: {status['reason']}")
                ok = bool(found) or not errors
                self.api.call("POST", f"/api/agent/tasks/{tid}/finish/", {
                    "status": "done" if ok else "error", "error": "; ".join(errors),
                    "result": {"count": len(found), "warnings": errors}, "found": found})
            elif task["kind"] == "probe":
                result = probe_device(task.get("params") or {}, log)
                self.api.call("POST", f"/api/agent/tasks/{tid}/finish/", {"status": "done", "result": result})
            elif task["kind"] == "ble_check":
                from scan_bluetooth import check_ble
                result = check_ble(task.get("params") or {}, log)
                self.api.call("POST", f"/api/agent/tasks/{tid}/finish/", {"status": "done", "result": result})
            else:
                raise ValueError(f"неизвестная задача {task['kind']}")
        except Exception as exc:
            traceback.print_exc()
            try:
                self.api.call("POST", f"/api/agent/tasks/{tid}/finish/", {"status": "error", "error": str(exc)})
            except Exception:
                pass
        finally:
            with self.lock:
                self.running_tasks.discard(tid)

    # ---------- опрос подключённых инверторов ----------
    def poll_device(self, device):
        """Читает показания одного устройства и отправляет на сервер."""
        serial = device["serial"]
        conn = dict(device["connection"])
        if serial in self.ip_override:
            conn["host"] = self.ip_override[serial]
        try:
            payload = read_payload(conn)
            if not device.get("has_generator"):
                payload.pop("generator", None)
            payload["modem"] = serial
            self.api.call("POST", "/api/agent/telemetry/", {"serial": serial, "payload": payload})
        except Exception as exc:
            print(f"[{serial}] нет данных от {conn.get('host')}: {exc}")
            # Логгер мог получить новый IP от роутера — ищем его по серийному номеру
            if conn.get("protocol") == "solarman_v5" and conn.get("logger_serial"):
                try:
                    for ip, info in solarman_broadcast(network_for()).items():
                        if info["serial"] == str(conn["logger_serial"]) and ip != conn.get("host"):
                            print(f"[{serial}] логгер найден по новому адресу {ip}")
                            self.ip_override[serial] = ip
                except Exception:
                    pass
        finally:
            with self.lock:
                self.busy.discard(serial)

    def loop(self):
        """Бесконечный цикл: забрать задачи, опросить устройства, подождать."""
        while True:
            try:
                data = self.api.call("GET", "/api/agent/poll/")
            except urllib.error.HTTPError as exc:
                if exc.code == 401:
                    sys.exit("Сервер не принял токен агента. Выдайте новый токен через POST /api/agents/{id}/new_token/.")
                print("Сервер ответил ошибкой:", exc)
                time.sleep(10)
                continue
            except Exception as exc:
                print("Нет связи с сервером:", exc)
                time.sleep(10)
                continue

            for task in data.get("tasks", []):
                with self.lock:
                    if task["id"] in self.running_tasks:
                        continue
                    self.running_tasks.add(task["id"])
                print(f"Новая задача {task['id']}: {task['kind']} {task.get('method') or ''}")
                threading.Thread(target=self.run_task, args=(task,), daemon=True).start()

            now = time.time()
            for device in data.get("devices", []):
                serial = device["serial"]
                interval = int(device["connection"].get("poll_interval", 10))
                with self.lock:
                    if serial in self.busy or now < self.next_poll.get(serial, 0):
                        continue
                    self.busy.add(serial)
                    self.next_poll[serial] = now + interval
                self.pool.submit(self.poll_device, device)
            time.sleep(POLL_SECONDS)


def main():
    """Запуск агента."""
    parser = argparse.ArgumentParser(description="Локальный агент Ата Энерго")
    parser.add_argument("--server", help="адрес сервера, например https://monitoring.ata-energo.kg")
    parser.add_argument("--token", help="токен агента (выдаётся на сервере)")
    args = parser.parse_args()
    cfg = load_config(args)
    api = Api(cfg["server"], cfg["token"])

    print(f"Агент Ата Энерго {VERSION}. Сервер: {cfg['server']}")
    caps = capabilities()
    print(f"IP в локальной сети: {local_ip()}. Bluetooth: "
          f"{'есть' if caps['bluetooth']['available'] else 'нет — ' + caps['bluetooth']['reason']}")
    while True:
        try:
            info = api.call("POST", "/api/agent/hello/", {
                "hostname": socket.gethostname(), "platform": platform.platform(), "version": VERSION,
                "local_ip": local_ip(), "capabilities": caps})
            print(f"Подключено к серверу как «{info['name']}». Жду задачи… (остановить: Ctrl+C)")
            break
        except urllib.error.HTTPError as exc:
            if exc.code == 401:
                sys.exit("Сервер не принял токен агента. Проверьте --token.")
            print("Сервер ответил ошибкой:", exc, "— повтор через 10 сек")
        except Exception as exc:
            print("Нет связи с сервером:", exc, "— повтор через 10 сек")
        time.sleep(10)
    try:
        Agent(api).loop()
    except KeyboardInterrupt:
        print("Агент остановлен.")


if __name__ == "__main__":
    main()
