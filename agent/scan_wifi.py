"""Реальный поиск устройств в локальной сети (Wi-Fi / LAN). Django не нужен.

Что делает scan_network():
  1) определяет локальную сеть компьютера (например 192.168.1.0/24);
  2) широковещательный запрос Solarman/IGEN (UDP 48899) — Wi-Fi логгеры отвечают "IP,MAC,SN";
  3) mDNS (Bonjour) — устройства, которые объявляют свои сервисы;
  4) SSDP (UPnP) — устройства, отвечающие на M-SEARCH, + их описание (производитель, модель);
  5) проверка портов 502 (Modbus TCP), 8899 (Solarman V5), 1883 (MQTT), 80 (веб) у всех адресов сети;
  6) для открытого 502 — реальный Modbus-запрос (идентификация, SunSpec);
     для логгера Solarman — реальное чтение регистров инвертора;
  7) MAC-адреса из ARP-таблицы и производитель по первым байтам MAC.
Ничего не выдумывается: в список попадает только то, что ответило.
"""
import ipaddress
import re
import socket
import subprocess
import sys
import urllib.request
import xml.etree.ElementTree as ET
from concurrent.futures import ThreadPoolExecutor

from protocols import ModbusError, ModbusTCP, detect_deye, detect_sunspec, SolarmanV5

PORTS = {502: "Modbus TCP (502)", 8899: "Solarman V5 (8899)", 1883: "MQTT (1883)", 80: "HTTP (80)"}

# Ключевые слова -> производитель (ищем в названиях mDNS/SSDP/BLE)
VENDORS = {
    "deye": "Deye", "sunsynk": "Sunsynk", "solarman": "Solarman (IGEN)", "igen": "Solarman (IGEN)",
    "growatt": "Growatt", "shine": "Growatt", "goodwe": "GoodWe", "huawei": "Huawei", "sun2000": "Huawei",
    "fusionsolar": "Huawei", "sma": "SMA", "sunny": "SMA", "fronius": "Fronius", "solaredge": "SolarEdge",
    "victron": "Victron Energy", "venus": "Victron Energy", "enphase": "Enphase", "envoy": "Enphase",
    "sofar": "Sofar Solar", "solis": "Solis (Ginlong)", "ginlong": "Solis (Ginlong)", "kstar": "Kstar",
    "voltronic": "Voltronic", "axpert": "Voltronic", "luxpower": "LuxPower", "foxess": "FoxESS",
    "afore": "Afore", "must": "Must", "srne": "SRNE", "anenji": "Anenji", "easun": "EASUN",
}
INVERTER_BRANDS = set(VENDORS.values()) - {"Solarman (IGEN)"}
LOGGER_WORDS = ("logger", "solarman", "igen", "shine", "dongle", "wifi stick", "wifi-stick", "datalogger", "envoy")

# Первые 3 байта MAC -> производитель модуля связи (только проверенные)
OUI = {
    "98D863": "Shanghai High-Flying (Wi-Fi модуль логгера)",
    "ACCF23": "Hi-Flying (Wi-Fi модуль логгера)",
    "240AC4": "Espressif (ESP32)", "30AEA4": "Espressif (ESP32)", "A4CF12": "Espressif (ESP)",
    "84F3EB": "Espressif (ESP8266)", "BCDDC2": "Espressif (ESP)", "3C71BF": "Espressif (ESP32)",
    "B827EB": "Raspberry Pi", "DCA632": "Raspberry Pi", "E45F01": "Raspberry Pi",
}


def guess_vendor(*texts):
    """Ищет название производителя в текстах."""
    blob = " ".join(t for t in texts if t).lower()
    for word, vendor in VENDORS.items():
        if re.search(rf"(^|[^a-z]){re.escape(word)}", blob):
            return vendor
    return ""


def local_ip():
    """IP этого компьютера в локальной сети (пакет никуда не отправляется)."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("10.255.255.255", 1))
        return s.getsockname()[0]
    except OSError:
        return "127.0.0.1"
    finally:
        s.close()


def network_for(subnet=None):
    """Сеть для поиска: из параметра или /24 вокруг IP компьютера. Максимум 1024 адреса."""
    if subnet:
        net = ipaddress.ip_network(subnet, strict=False)
    else:
        net = ipaddress.ip_network(f"{local_ip()}/24", strict=False)
    if net.num_addresses > 1024:
        raise ValueError(f"Сеть {net} слишком большая (больше 1024 адресов). Укажите, например, /24.")
    return net


def tcp_open(ip, port, timeout=0.6):
    """Открыт ли TCP-порт."""
    try:
        with socket.create_connection((str(ip), port), timeout=timeout):
            return True
    except OSError:
        return False


def solarman_broadcast(net, timeout=3.0):
    """Широковещательный запрос Wi-Fi логгерам Solarman/IGEN (UDP 48899). Ответ: "IP,MAC,SN"."""
    found = {}
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
    sock.settimeout(0.5)
    try:
        for target in {str(net.broadcast_address), "255.255.255.255"}:
            for msg in (b"WIFIKIT-214028-READ", b"HF-A11ASSISTHREAD"):
                try:
                    sock.sendto(msg, (target, 48899))
                except OSError:
                    pass
        import time
        end = time.time() + timeout
        while time.time() < end:
            try:
                data, addr = sock.recvfrom(1024)
            except socket.timeout:
                continue
            except OSError:
                break
            parts = data.decode("ascii", "ignore").strip().split(",")
            if len(parts) >= 3 and parts[2].strip().isdigit():
                # логгер сам пишет свой IP в ответе (надёжнее, чем адрес отправителя)
                try:
                    ip = str(ipaddress.ip_address(parts[0].strip()))
                except ValueError:
                    ip = addr[0]
                found[ip] = {"mac": format_mac(parts[1]), "serial": parts[2].strip()}
    finally:
        sock.close()
    return found


def ssdp_search(timeout=3.0):
    """SSDP M-SEARCH: устройства UPnP отвечают со ссылкой на своё описание."""
    msg = ("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\n"
           "MX: 2\r\nST: ssdp:all\r\n\r\n").encode()
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM, socket.IPPROTO_UDP)
    sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_TTL, 2)
    sock.settimeout(0.5)
    results = {}
    try:
        sock.sendto(msg, ("239.255.255.250", 1900))
        import time
        end = time.time() + timeout
        while time.time() < end:
            try:
                data, addr = sock.recvfrom(4096)
            except socket.timeout:
                continue
            headers = {}
            for line in data.decode("utf-8", "ignore").split("\r\n")[1:]:
                if ":" in line:
                    k, v = line.split(":", 1)
                    headers[k.strip().lower()] = v.strip()
            entry = results.setdefault(addr[0], {"server": headers.get("server", ""), "st": set(), "location": ""})
            entry["st"].add(headers.get("st", ""))
            entry["location"] = entry["location"] or headers.get("location", "")
    except OSError:
        pass
    finally:
        sock.close()
    for ip, entry in results.items():
        entry["st"] = sorted(s for s in entry["st"] if s)[:10]
        if entry["location"]:
            entry.update(ssdp_description(entry["location"]))
    return results


def ssdp_description(url):
    """Читает XML-описание UPnP устройства (friendlyName, manufacturer, modelName...)."""
    try:
        with urllib.request.urlopen(url, timeout=2) as resp:
            root = ET.fromstring(resp.read(200000))
    except Exception:
        return {}
    out = {}
    for el in root.iter():
        tag = el.tag.split("}")[-1]
        if tag in ("friendlyName", "manufacturer", "modelName", "modelNumber", "serialNumber") and el.text and tag not in out:
            out[tag] = el.text.strip()
    return out


def mdns_browse(timeout=4.0):
    """mDNS (Bonjour): какие сервисы объявляют устройства в сети. Нужна библиотека zeroconf."""
    try:
        from zeroconf import ServiceBrowser, Zeroconf, ZeroconfServiceTypes
    except ImportError:
        return None  # библиотека не установлена
    zc = Zeroconf()
    results = {}
    try:
        types = ZeroconfServiceTypes.find(zc=zc, timeout=timeout / 2)
        names = []

        class Listener:
            """Собирает имена найденных сервисов."""

            def add_service(self, zc_, type_, name):
                names.append((type_, name))

            def update_service(self, zc_, type_, name):
                pass

            def remove_service(self, zc_, type_, name):
                pass

        browsers = [ServiceBrowser(zc, t, Listener()) for t in types]
        import time
        time.sleep(timeout / 2)
        for b in browsers:
            b.cancel()
        for type_, name in names:
            info = zc.get_service_info(type_, name, timeout=1000)
            if not info:
                continue
            for ip in info.parsed_addresses():
                if ":" in ip:
                    continue  # IPv6 пропускаем
                entry = results.setdefault(ip, {"services": [], "names": [], "server": info.server or "", "txt": {}})
                entry["services"].append(type_.replace(".local.", ""))
                entry["names"].append(name.replace("." + type_, ""))
                for k, v in (info.properties or {}).items():
                    try:
                        entry["txt"][k.decode()] = (v or b"").decode("utf-8", "ignore")[:100]
                    except Exception:
                        pass
    finally:
        zc.close()
    return results


def format_mac(mac):
    """Приводит MAC к виду AA:BB:CC:DD:EE:FF."""
    hexes = re.sub(r"[^0-9A-Fa-f]", "", mac or "")
    return ":".join(hexes[i:i + 2] for i in range(0, 12, 2)).upper() if len(hexes) == 12 else ""


def arp_table():
    """IP -> MAC из ARP-таблицы компьютера (Windows, Linux, macOS)."""
    table = {}
    try:
        if sys.platform.startswith("linux"):
            with open("/proc/net/arp") as f:
                for line in f.readlines()[1:]:
                    parts = line.split()
                    if len(parts) >= 4 and parts[3] != "00:00:00:00:00:00":
                        table[parts[0]] = format_mac(parts[3])
        else:
            out = subprocess.run(["arp", "-a"], capture_output=True, text=True, timeout=5).stdout
            for m in re.finditer(r"(\d+\.\d+\.\d+\.\d+)\D+?(([0-9a-fA-F]{1,2}[:-]){5}[0-9a-fA-F]{1,2})", out):
                mac = format_mac("".join(x.zfill(2) for x in re.split("[:-]", m.group(2))))
                if mac and mac != "FF:FF:FF:FF:FF:FF":
                    table[m.group(1)] = mac
    except Exception:
        pass
    return table


def quick_modbus(ip):
    """Короткая реальная проверка Modbus TCP: отвечает ли устройство, SunSpec, идентификация."""
    info = {"modbus": False}
    try:
        mb = ModbusTCP(ip, 502, unit_id=1, timeout=2)
    except OSError as exc:
        info["modbus_error"] = str(exc)
        return info
    try:
        try:
            info["device_id"] = mb.read_device_id()
            info["modbus"] = True
        except (ModbusError, OSError):
            pass
        try:
            sunspec = detect_sunspec(mb)
            if sunspec:
                info["sunspec"] = sunspec
                info["modbus"] = True
        except (ModbusError, OSError):
            pass
        if not info.get("sunspec"):
            try:
                deye = detect_deye(mb)
                info["modbus"] = True
                # похоже на Deye/Sunsynk, только если тип известен и SN — нормальный текст
                if deye["profile"] and len(deye["serial"]) >= 6 and deye["serial"].isalnum():
                    info["deye"] = deye
            except ModbusError:
                info["modbus"] = True  # ответ-исключение — это тоже Modbus
            except OSError:
                pass
    finally:
        mb.close()
    return info


def quick_solarman(ip, serial):
    """Короткая реальная проверка логгера Solarman: читаем тип и SN инвертора (Deye-совместимые)."""
    try:
        r = SolarmanV5(ip, serial, timeout=4)
    except ImportError:
        return {"solarman_error": "не установлена библиотека pysolarmanv5"}
    except Exception as exc:
        return {"solarman_error": str(exc)}
    try:
        return {"deye": detect_deye(r)}
    except Exception as exc:
        return {"solarman_error": f"логгер ответил, но регистры не прочитаны: {exc}"}
    finally:
        r.close()


def scan_network(params, log):
    """Полный поиск в локальной сети. log(text, found=None) — отправка прогресса на сервер."""
    net = network_for(params.get("subnet"))
    hosts = [str(h) for h in net.hosts()]
    me = local_ip()
    log(f"Локальная сеть: {net} (IP агента {me}), адресов: {len(hosts)}")

    log("Запрос Wi-Fi логгерам Solarman/IGEN (UDP 48899)…")
    solarman = solarman_broadcast(net)
    log(f"  ответили логгеры: {len(solarman)}")

    log("mDNS (Bonjour)…")
    mdns = mdns_browse()
    if mdns is None:
        log("  пропущено: не установлена библиотека zeroconf (pip install zeroconf)")
        mdns = {}
    else:
        log(f"  устройств с сервисами mDNS: {len(mdns)}")

    log("SSDP (UPnP)…")
    ssdp = ssdp_search()
    log(f"  ответили по SSDP: {len(ssdp)}")

    log(f"Проверка портов {', '.join(map(str, PORTS))} на {len(hosts)} адресах…")
    open_ports = {}

    def check(pair):
        ip, port = pair
        return ip, port, tcp_open(ip, port, timeout=0.5)

    with ThreadPoolExecutor(max_workers=128) as pool:
        for ip, port, ok in pool.map(check, [(h, p) for h in hosts for p in PORTS]):
            if ok:
                open_ports.setdefault(ip, []).append(port)
    log(f"  адресов с открытыми портами: {len(open_ports)}")

    arp = arp_table()
    candidates = set(solarman) | set(mdns) | set(ssdp) | {ip for ip, ports in open_ports.items()
                                                           if 502 in ports or 8899 in ports}
    candidates.discard(me)
    devices = []
    for ip in sorted(candidates, key=lambda x: ipaddress.ip_address(x)):
        ports = sorted(open_ports.get(ip, []))
        m, s, sl = mdns.get(ip, {}), ssdp.get(ip, {}), solarman.get(ip)
        mac = (sl or {}).get("mac") or arp.get(ip, "")
        protocols = [PORTS[p] for p in ports]
        details = {"open_ports": ports}
        if m:
            protocols.append("mDNS")
            details["mdns"] = {"services": m["services"][:10], "names": m["names"][:5], "server": m["server"]}
        if s:
            protocols.append("SSDP / UPnP")
            details["ssdp"] = {k: v for k, v in s.items() if k != "location"}
        name = (s.get("friendlyName") or (m.get("names") or [""])[0] or m.get("server", "").rstrip(".")
                or (f"Logger {sl['serial']}" if sl else ""))
        manufacturer = s.get("manufacturer") or guess_vendor(name, m.get("server"), s.get("server"),
                                                             " ".join(m.get("services", [])))
        model = s.get("modelName") or ""
        serial = (sl or {}).get("serial", "")
        kind = "unknown"
        if mac and mac.replace(":", "")[:6] in OUI:
            details["mac_vendor"] = OUI[mac.replace(":", "")[:6]]

        if sl:
            kind = "logger"
            manufacturer = manufacturer or "Solarman (IGEN)"
            protocols.append("Solarman broadcast (48899)")
            details["logger_serial"] = serial
            if 8899 in ports:
                log(f"  {ip}: логгер {serial} — читаю регистры инвертора через Solarman V5…")
                q = quick_solarman(ip, serial)
                details.update(q)
                if q.get("deye"):
                    d = q["deye"]
                    details["inverter"] = f"Deye-совместимый, {d['type_name']}, SN {d['serial']}"
                    model = model or f"Logger → инвертор Deye ({d['type_name']})"
        if 502 in ports:
            log(f"  {ip}: проверяю Modbus TCP…")
            q = quick_modbus(ip)
            details["modbus_check"] = {k: v for k, v in q.items() if k != "sunspec"}
            if q.get("sunspec"):
                sp = q["sunspec"]
                details["sunspec"] = sp
                protocols.append("SunSpec")
                manufacturer = manufacturer or sp.get("manufacturer", "")
                model = model or sp.get("model", "")
                serial = serial or sp.get("serial", "")
                if sp.get("inverter_addr"):
                    kind = "inverter"
            if q.get("deye"):
                d = q["deye"]
                kind = "inverter"
                manufacturer = manufacturer or "Deye / Sunsynk (вероятно)"
                model = model or d["type_name"]
                serial = serial or d["serial"]
                details["inverter"] = f"Deye-совместимый, {d['type_name']}, SN {d['serial']}"
            if q.get("device_id"):
                manufacturer = manufacturer or q["device_id"].get("vendor", "")
                model = model or q["device_id"].get("product", "")
        text = f"{name} {model} {' '.join(m.get('services', []))}".lower()
        if kind == "unknown" and any(w in text for w in LOGGER_WORDS):
            kind = "logger"
        if kind == "unknown" and manufacturer in INVERTER_BRANDS:
            kind = "inverter"

        devices.append({
            "key": f"ip:{ip}", "connection_type": "lan", "name": name or ip, "manufacturer": manufacturer,
            "model": model, "serial": serial, "ip": ip, "mac": mac, "device_kind": kind,
            "protocols": sorted(set(protocols)), "details": details,
        })
    log(f"Готово: найдено устройств {len(devices)}", found=devices)
    return devices


def probe_device(params, log):
    """Проверка совместимости: пытаемся РЕАЛЬНО прочитать показания инвертора.

    params: ip, ports, serial, + (необязательно) protocol, port, unit_id, logger_serial, profile.
    Возвращает result для сервера (ok=True только если показания прочитаны и выглядят правдоподобно).
    """
    from protocols import PROFILES, ModbusTCP as MB, open_reader, plausible, read_sunspec

    ip = params.get("ip")
    ports = set(params.get("ports") or [])
    protocol = params.get("protocol") or "auto"
    unit_id = int(params.get("unit_id") or 1)
    forced_profile = params.get("profile") or "auto"
    logger_serial = str(params.get("logger_serial") or params.get("serial") or "").strip()
    options = {"invert_battery": params.get("invert_battery", True), "invert_grid": params.get("invert_grid", False)}
    for key in options:
        if isinstance(options[key], str):
            options[key] = options[key].lower() in ("1", "true", "yes", "on")
    attempts = []

    def step(label, ok, detail):
        attempts.append({"protocol": label, "ok": ok, "detail": detail})
        log(f"{'✓' if ok else '✗'} {label}: {detail}")

    def try_deye(reader, conn, label):
        """Определяем профиль Deye и читаем показания."""
        profile = forced_profile if forced_profile in ("deye_1ph", "deye_3ph") else None
        info = {}
        if profile is None:
            info = detect_deye(reader)
            step(label, True, f"регистр типа = {info['device_type']} ({info['type_name']}), SN «{info['serial']}»")
            profile = info["profile"]
            if not profile:
                step(label, False, "этот тип инвертора пока не поддерживается профилями агента")
                return None
        conn = {**conn, "profile": profile, **options}
        payload, raw = PROFILES[profile][1](reader, conn)
        ok, why = plausible(payload)
        if not ok:
            step(label, False, f"значения не похожи на правду ({why}) — нужен другой профиль регистров")
            return None
        step(label, True, f"показания прочитаны по профилю «{PROFILES[profile][0]}»")
        return {
            "ok": True, "message": "Показания получены. Сверьте их с экраном инвертора.",
            "protocol": conn["protocol"],
            "protocol_label": "Solarman V5 (8899)" if conn["protocol"] == "solarman_v5" else "Modbus TCP (502)",
            "connection": conn, "profile": profile, "profile_name": PROFILES[profile][0],
            "manufacturer": "Deye / Sunsynk (совместимый)", "model": info.get("type_name", ""),
            "serial": info.get("serial") or logger_serial, "sections": PROFILES[profile][2],
            "readings": payload, "raw": raw, "attempts": attempts,
        }

    # 1) Solarman V5 (Wi-Fi логгер)
    if protocol in ("auto", "solarman_v5"):
        port = int(params.get("port") or 8899) if protocol == "solarman_v5" else 8899
        if protocol == "solarman_v5" or 8899 in ports or tcp_open(ip, 8899):
            if not logger_serial.isdigit():
                step("Solarman V5", False, "нужен серийный номер логгера (цифры на наклейке под QR-кодом)")
            else:
                conn = {"protocol": "solarman_v5", "host": ip, "port": port, "logger_serial": int(logger_serial), "unit_id": unit_id}
                try:
                    reader = open_reader(conn)
                    try:
                        result = try_deye(reader, conn, "Solarman V5")
                    finally:
                        reader.close()
                    if result:
                        result["serial"] = logger_serial  # модем = логгер
                        return result
                except ImportError:
                    step("Solarman V5", False, "не установлена библиотека pysolarmanv5 (pip install pysolarmanv5)")
                except Exception as exc:
                    step("Solarman V5", False, f"не удалось прочитать: {exc}")

    # 2) Modbus TCP (встроенный в инвертор или шлюз RS485 -> TCP)
    if protocol in ("auto", "modbus_tcp", "sunspec"):
        port = int(params.get("port") or 502) if protocol != "auto" else 502
        if protocol != "auto" or 502 in ports or tcp_open(ip, 502):
            try:
                mb = MB(ip, port, unit_id=unit_id, timeout=3)
            except OSError as exc:
                step("Modbus TCP", False, f"нет подключения к {ip}:{port} ({exc})")
                mb = None
            if mb:
                conn = {"protocol": "modbus_tcp", "host": ip, "port": port, "unit_id": unit_id}
                try:
                    if forced_profile in ("auto", "sunspec"):
                        sp = detect_sunspec(mb)
                        if sp and sp.get("inverter_addr"):
                            payload, raw = read_sunspec(mb, sp)
                            ok, why = plausible(payload)
                            step("SunSpec", ok, "показания блока инвертора прочитаны" if ok else why)
                            if ok:
                                return {
                                    "ok": True, "message": "Показания получены. Сверьте их с экраном инвертора.",
                                    "protocol": "modbus_tcp", "protocol_label": "SunSpec",
                                    "connection": {**conn, "profile": "sunspec", "sunspec": sp},
                                    "profile": "sunspec", "profile_name": PROFILES["sunspec"][0],
                                    "manufacturer": sp.get("manufacturer", ""), "model": sp.get("model", ""),
                                    "serial": sp.get("serial", ""), "sections": PROFILES["sunspec"][2],
                                    "readings": payload, "raw": raw, "attempts": attempts,
                                }
                        else:
                            step("SunSpec", False, "блоки SunSpec не найдены")
                    if forced_profile in ("auto", "deye_1ph", "deye_3ph"):
                        try:
                            result = try_deye(mb, conn, "Modbus TCP (Deye-регистры)")
                            if result:
                                return result
                        except ModbusError as exc:
                            step("Modbus TCP (Deye-регистры)", False, str(exc))
                except (ModbusError, OSError) as exc:
                    step("Modbus TCP", False, str(exc))
                finally:
                    mb.close()
    if not attempts:
        step("Поиск протокола", False, "у устройства нет открытых портов 502/8899 — данные читать не через что")
    return {"ok": False, "message": "Устройство обнаружено, требуется настройка протокола", "attempts": attempts}
