"""Протоколы чтения инверторов (используются агентом). Django не нужен.

ModbusTCP        — простой клиент Modbus TCP (порт 502), без внешних библиотек.
SolarmanV5       — чтение через Wi-Fi логгер Solarman (порт 8899), библиотека pysolarmanv5.
detect_sunspec() — поиск блоков SunSpec (SMA, Fronius, SolarEdge, Huawei и др.).
detect_deye()    — определение инвертора Deye / Sunsynk (и их OEM) по регистрам.
read_payload()   — прочитать показания и собрать сообщение в формате сервера.

ВАЖНО: карты регистров Deye/Sunsynk взяты из открытых описаний (проекты
ha-solarman, kellerza/sunsynk). Перед подключением сверьте значения
с экраном инвертора — для этого в проверке показываются сырые цифры.
"""
import socket
import struct


class ModbusError(Exception):
    """Ошибка Modbus (в т.ч. "исключение" от устройства)."""


# ---------------------------------------------------------------------------
# Modbus TCP
# ---------------------------------------------------------------------------
class ModbusTCP:
    """Минимальный клиент Modbus TCP: чтение регистров и идентификации устройства."""

    def __init__(self, host, port=502, unit_id=1, timeout=3.0):
        """Подключается к устройству."""
        self.unit_id = int(unit_id)
        self.sock = socket.create_connection((host, int(port)), timeout=timeout)
        self.sock.settimeout(timeout)
        self.tid = 0

    def close(self):
        """Закрывает соединение."""
        try:
            self.sock.close()
        except OSError:
            pass

    def _recv_exact(self, size):
        """Читает ровно size байт."""
        data = b""
        while len(data) < size:
            chunk = self.sock.recv(size - len(data))
            if not chunk:
                raise ModbusError("соединение закрыто устройством")
            data += chunk
        return data

    def request(self, pdu):
        """Отправляет PDU и возвращает PDU ответа (без заголовка MBAP)."""
        self.tid = (self.tid + 1) % 65536
        header = struct.pack(">HHHB", self.tid, 0, len(pdu) + 1, self.unit_id)
        self.sock.sendall(header + pdu)
        tid, proto, length, _unit = struct.unpack(">HHHB", self._recv_exact(7))
        if proto != 0 or length < 2 or length > 260:
            raise ModbusError("ответ не похож на Modbus TCP")
        body = self._recv_exact(length - 1)
        if body[0] & 0x80:
            raise ModbusError(f"устройство ответило исключением Modbus {body[1] if len(body) > 1 else '?'}")
        return body

    def _read(self, function, address, count):
        """Чтение регистров функцией 3 (holding) или 4 (input)."""
        body = self.request(struct.pack(">BHH", function, int(address), int(count)))
        if body[0] != function or len(body) < 2 + 2 * count:
            raise ModbusError("неожиданный ответ на чтение регистров")
        return list(struct.unpack(f">{count}H", body[2:2 + 2 * count]))

    def read_holding(self, address, count):
        """Читает holding-регистры (функция 3)."""
        return self._read(3, address, count)

    def read_input(self, address, count):
        """Читает input-регистры (функция 4)."""
        return self._read(4, address, count)

    def read_device_id(self):
        """Функция 43/14: производитель, код продукта, версия (если устройство поддерживает)."""
        body = self.request(bytes([0x2B, 0x0E, 0x01, 0x00]))
        if len(body) < 7 or body[0] != 0x2B:
            raise ModbusError("нет идентификации устройства")
        count, pos, out = body[6], 7, {}
        names = {0: "vendor", 1: "product", 2: "revision"}
        for _ in range(count):
            obj_id, size = body[pos], body[pos + 1]
            out[names.get(obj_id, str(obj_id))] = body[pos + 2:pos + 2 + size].decode("ascii", "ignore").strip()
            pos += 2 + size
        return out


class SolarmanV5:
    """Чтение регистров через Wi-Fi логгер Solarman (нужен серийный номер логгера)."""

    def __init__(self, host, logger_serial, port=8899, unit_id=1, timeout=5):
        """Подключается к логгеру."""
        from pysolarmanv5 import PySolarmanV5  # импорт здесь: библиотека нужна только для Solarman

        self.client = PySolarmanV5(host, int(logger_serial), port=int(port), mb_slave_id=int(unit_id),
                                   socket_timeout=timeout, verbose=False)

    def read_holding(self, address, count):
        """Читает holding-регистры."""
        return list(self.client.read_holding_registers(register_addr=int(address), quantity=int(count)))

    def read_input(self, address, count):
        """Читает input-регистры."""
        return list(self.client.read_input_registers(register_addr=int(address), quantity=int(count)))

    def close(self):
        """Закрывает соединение."""
        try:
            self.client.disconnect()
        except Exception:
            pass


def open_reader(conn):
    """Открывает подключение по описанию conn = {"protocol", "host", "port", "unit_id", "logger_serial"}."""
    protocol = conn.get("protocol")
    if protocol == "solarman_v5":
        return SolarmanV5(conn["host"], conn["logger_serial"], conn.get("port", 8899), conn.get("unit_id", 1))
    if protocol in ("modbus_tcp", "sunspec"):
        return ModbusTCP(conn["host"], conn.get("port", 502), conn.get("unit_id", 1))
    raise ModbusError(f"протокол {protocol} не поддерживается агентом")


# ---------------------------------------------------------------------------
# Вспомогательное
# ---------------------------------------------------------------------------
def s16(value):
    """Беззнаковое 16-бит -> со знаком."""
    return value - 65536 if value >= 32768 else value


def regs_to_text(regs):
    """Регистры -> строка ASCII (2 символа на регистр)."""
    raw = b"".join(struct.pack(">H", r) for r in regs)
    return raw.replace(b"\x00", b"").decode("ascii", "ignore").strip()


def temp_deye(value):
    """Температура Deye: (x - 1000) / 10."""
    return round((value - 1000) / 10, 1)


# ---------------------------------------------------------------------------
# SunSpec (Modbus TCP)
# ---------------------------------------------------------------------------
SUNSPEC_BASES = (40000, 0, 50000)


def detect_sunspec(mb):
    """Ищет SunSpec. Возвращает {"base", "manufacturer", "model", "serial", "inverter_addr", "inverter_model"} или None."""
    for base in SUNSPEC_BASES:
        try:
            if mb.read_holding(base, 2) != [0x5375, 0x6E53]:  # "SunS"
                continue
        except (ModbusError, OSError):
            continue
        info = {"base": base}
        addr = base + 2
        for _ in range(40):
            model_id, length = mb.read_holding(addr, 2)
            if model_id == 0xFFFF or length == 0:
                break
            if model_id == 1:
                block = mb.read_holding(addr + 2, 66)
                info["manufacturer"] = regs_to_text(block[0:16])
                info["model"] = regs_to_text(block[16:32])
                info["serial"] = regs_to_text(block[48:64])
            if model_id in (101, 102, 103) and "inverter_addr" not in info:
                info["inverter_addr"] = addr + 2
                info["inverter_model"] = model_id
            addr += 2 + length
        return info
    return None


def _sf(value, scale, signed=True):
    """Значение SunSpec с масштабом 10^scale. Нереализованное значение -> None."""
    if value in (0x8000, 0xFFFF) or scale == 0x8000:
        return None
    v = s16(value) if signed else value
    return round(v * 10 ** s16(scale), 2)


def read_sunspec(mb, info):
    """Читает блок инвертора SunSpec (101/102/103). Возвращает сообщение сервера."""
    b = mb.read_holding(info["inverter_addr"], 50)
    ac_w = _sf(b[12], b[13])
    payload = {
        "inverter": {"power": ac_w, "voltage": _sf(b[8], b[11], signed=False), "current": _sf(b[0], b[4], signed=False),
                     "temperature": _sf(b[31], b[35]), "error": None},
        "pv": [{"power": _sf(b[29], b[30], signed=False), "voltage": _sf(b[27], b[28], signed=False),
                "current": _sf(b[25], b[26], signed=False)}],
    }
    return payload, {"Частота, Гц": _sf(b[14], b[15], signed=False), "Статус (St)": b[36]}


# ---------------------------------------------------------------------------
# Deye / Sunsynk (и OEM): через Solarman V5 или Modbus TCP-шлюз RS485
# ---------------------------------------------------------------------------
DEYE_TYPES = {2: "сетевой (string)", 3: "гибридный однофазный", 4: "микроинвертор",
              5: "гибридный трёхфазный LV", 6: "гибридный трёхфазный HV"}


def detect_deye(reader):
    """Определяет Deye/Sunsynk по регистру 0 (тип) и 3..7 (серийный номер)."""
    device_type = reader.read_holding(0, 1)[0]
    serial = regs_to_text(reader.read_holding(3, 5))
    profile = {3: "deye_1ph", 5: "deye_3ph", 6: "deye_3ph"}.get(device_type)
    return {"device_type": device_type, "type_name": DEYE_TYPES.get(device_type, f"неизвестный тип {device_type}"),
            "serial": serial, "profile": profile}


def read_deye_1ph(r, conn):
    """Однофазный гибрид Deye/Sunsynk (SUN-xK-SG0xLP1 и аналоги)."""
    t = r.read_holding(90, 2)
    pv = r.read_holding(109, 4)
    grid_v = r.read_holding(150, 1)[0]
    m = r.read_holding(166, 26)          # регистры 166..191
    g = lambda reg: m[reg - 166]         # noqa: E731
    bat_sign = -1 if conn.get("invert_battery", True) else 1   # у Deye "+" = разряд, у нас "+" = заряд
    grid_sign = -1 if conn.get("invert_grid", False) else 1
    payload = {
        "inverter": {"power": s16(g(175)), "voltage": None, "current": None, "temperature": temp_deye(t[1]), "error": None},
        "pv": [{"power": g(186), "voltage": pv[0] / 10, "current": pv[1] / 10},
               {"power": g(187), "voltage": pv[2] / 10, "current": pv[3] / 10}],
        "battery": {"power": bat_sign * s16(g(190)), "voltage": g(183) / 100, "current": bat_sign * s16(g(191)) / 100,
                    "soc": g(184), "temperature": temp_deye(g(182))},
        "grid": {"power": grid_sign * s16(g(169)), "voltage": grid_v / 10, "current": None},
        "load": {"power": s16(g(178)), "voltage": None, "current": None},
        "generator": {"power": s16(g(166)), "voltage": None, "current": None},
    }
    raw = {"reg 184 SOC": g(184), "reg 190 батарея (сырое)": s16(g(190)), "reg 169 сеть (сырое)": s16(g(169)),
           "reg 178 нагрузка": s16(g(178)), "reg 186/187 PV": f"{g(186)} / {g(187)}"}
    return payload, raw


def read_deye_3ph(r, conn):
    """Трёхфазный гибрид Deye (SUN-xK-SG04LP3, SG01HP3 и аналоги)."""
    bat = r.read_holding(586, 6)         # 586..591
    grid_v = r.read_holding(598, 1)[0]
    grid_w = r.read_holding(625, 1)[0]
    inv_w = r.read_holding(636, 1)[0]
    load_w = r.read_holding(653, 1)[0]
    gen_w = r.read_holding(667, 1)[0]
    pv = r.read_holding(672, 8)          # 672..679
    ac_t = r.read_holding(541, 1)[0]
    bat_sign = -1 if conn.get("invert_battery", True) else 1
    grid_sign = -1 if conn.get("invert_grid", False) else 1
    payload = {
        "inverter": {"power": s16(inv_w), "voltage": None, "current": None, "temperature": temp_deye(ac_t), "error": None},
        "pv": [{"power": pv[0], "voltage": pv[4] / 10, "current": pv[5] / 10},
               {"power": pv[1], "voltage": pv[6] / 10, "current": pv[7] / 10}],
        "battery": {"power": bat_sign * s16(bat[4]), "voltage": bat[1] / 100, "current": bat_sign * s16(bat[5]) / 100,
                    "soc": bat[2], "temperature": temp_deye(bat[0])},
        "grid": {"power": grid_sign * s16(grid_w), "voltage": grid_v / 10, "current": None},
        "load": {"power": s16(load_w), "voltage": None, "current": None},
        "generator": {"power": s16(gen_w), "voltage": None, "current": None},
    }
    raw = {"reg 588 SOC": bat[2], "reg 590 батарея (сырое)": s16(bat[4]), "reg 625 сеть (сырое)": s16(grid_w),
           "reg 653 нагрузка": s16(load_w), "reg 672/673 PV": f"{pv[0]} / {pv[1]}"}
    return payload, raw


PROFILES = {
    "deye_1ph": ("Deye / Sunsynk гибрид однофазный", read_deye_1ph,
                 ["inverter", "pv", "battery", "grid", "load"]),
    "deye_3ph": ("Deye гибрид трёхфазный", read_deye_3ph, ["inverter", "pv", "battery", "grid", "load"]),
    "sunspec": ("SunSpec (сетевой инвертор)", None, ["inverter", "pv"]),
}


def plausible(payload):
    """Грубая проверка, что числа похожи на правду (а не мусор из чужих регистров)."""
    bat = payload.get("battery")
    if bat:
        if bat.get("soc") is not None and not 0 <= bat["soc"] <= 100:
            return False, f"заряд батареи {bat['soc']}% вне 0..100"
        if bat.get("voltage") and not 5 <= bat["voltage"] <= 1000:
            return False, f"напряжение батареи {bat['voltage']} В не похоже на правду"
    for section in ("inverter", "grid", "load"):
        p = (payload.get(section) or {}).get("power")
        if p is not None and abs(p) > 200000:
            return False, f"мощность {section} = {p} Вт не похожа на правду"
    for part in payload.get("pv") or []:
        if part.get("voltage") and part["voltage"] > 1500:
            return False, f"напряжение панелей {part['voltage']} В не похоже на правду"
    return True, ""


def read_payload(conn):
    """Читает показания по сохранённым параметрам подключения (для регулярного опроса)."""
    reader = open_reader(conn)
    try:
        profile = conn.get("profile")
        if profile == "sunspec":
            info = conn.get("sunspec") or detect_sunspec(reader)
            if not info or "inverter_addr" not in info:
                raise ModbusError("блок инвертора SunSpec не найден")
            payload, _ = read_sunspec(reader, info)
        elif profile in PROFILES and PROFILES[profile][1]:
            payload, _ = PROFILES[profile][1](reader, conn)
        else:
            raise ModbusError(f"нет профиля регистров «{profile}»")
        return payload
    finally:
        reader.close()
