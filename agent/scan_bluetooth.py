"""Реальный поиск Bluetooth LE устройств (библиотека bleak). Django не нужен.

scan_ble()  — 8 секунд слушает эфир: имя, адрес (ID), сигнал RSSI, сервисы, данные производителя.
check_ble() — подключается к выбранному устройству и читает список GATT-сервисов
              и стандартный сервис "Device Information" (производитель, модель, SN).

Ограничения:
  * bleak работает только с Bluetooth LE. Классический Bluetooth (SPP) он не ищет.
  * Нужен Bluetooth-адаптер на компьютере агента и включённый Bluetooth.
  * Большинство BLE-модулей инверторов служат для НАСТРОЙКИ Wi-Fi логгера, а не для
    передачи показаний, и используют закрытые протоколы производителя.
"""
import asyncio
import sys

from scan_wifi import guess_vendor, INVERTER_BRANDS, LOGGER_WORDS

# Bluetooth SIG Company ID -> производитель чипа/устройства (из официального списка)
COMPANY_IDS = {0x004C: "Apple", 0x0006: "Microsoft", 0x0075: "Samsung", 0x00E0: "Google",
               0x02E5: "Espressif", 0x0059: "Nordic Semiconductor", 0x000D: "Texas Instruments",
               0x0157: "Huami", 0x038F: "Xiaomi", 0x0171: "Amazon", 0x02FF: "Silicon Labs",
               0x0131: "Cypress", 0x0002: "Intel", 0x000F: "Broadcom", 0x001D: "Qualcomm"}

# Стандартные сервисы GATT (короткие коды)
GATT_NAMES = {"1800": "Generic Access", "1801": "Generic Attribute", "180a": "Device Information",
              "180f": "Battery Service", "1805": "Current Time", "ffe0": "UART-подобный (HM-10 / CC254x)",
              "ffff": "Производителя", "fee7": "Tencent / WeChat", "6e400001": "Nordic UART"}


def bluetooth_status():
    """Есть ли у агента рабочий Bluetooth LE. Возвращает {"available": bool, "reason": str}."""
    try:
        import bleak  # noqa: F401
    except ImportError:
        return {"available": False, "reason": "не установлена библиотека bleak (pip install bleak)"}
    try:
        from bleak import BleakScanner

        async def probe():
            scanner = BleakScanner()
            await scanner.start()
            await scanner.stop()

        asyncio.run(asyncio.wait_for(probe(), 10))
        return {"available": True, "reason": ""}
    except Exception as exc:
        hint = " (Linux: нужен BlueZ и служба bluetooth)" if sys.platform.startswith("linux") else ""
        return {"available": False, "reason": f"Bluetooth-адаптер не найден или выключен{hint}: {exc}"}


def short_uuid(uuid):
    """0000180a-0000-1000-8000-00805f9b34fb -> 180a"""
    uuid = str(uuid).lower()
    if uuid.endswith("-0000-1000-8000-00805f9b34fb") and uuid.startswith("0000"):
        return uuid[4:8]
    return uuid


def describe_service(uuid):
    """Понятное название сервиса."""
    s = short_uuid(uuid)
    return GATT_NAMES.get(s) or GATT_NAMES.get(s[:8]) or ""


def scan_ble(log, timeout=8.0):
    """Ищет BLE-устройства. Возвращает список устройств для сервера."""
    from bleak import BleakScanner

    log(f"Поиск Bluetooth LE ({timeout:.0f} сек)…")
    found = asyncio.run(BleakScanner.discover(timeout=timeout, return_adv=True))
    devices = []
    for address, (dev, adv) in found.items():
        name = adv.local_name or dev.name or ""
        company = [COMPANY_IDS.get(cid, f"0x{cid:04X}") for cid in (adv.manufacturer_data or {})]
        services = [short_uuid(u) for u in (adv.service_uuids or [])]
        vendor = guess_vendor(name)
        lname = name.lower()
        kind = "logger" if any(w in lname for w in LOGGER_WORDS) else ("inverter" if vendor in INVERTER_BRANDS else "unknown")
        devices.append({
            "key": f"bt:{address}", "connection_type": "bluetooth", "name": name or "(без имени)",
            "manufacturer": vendor or (", ".join(company) + " (чип)" if company else ""),
            "bt_address": address, "rssi": adv.rssi, "device_kind": kind,
            "protocols": ["Bluetooth LE"],
            "details": {"services": [f"{s} {describe_service(s)}".strip() for s in services][:20],
                        "company_ids": company, "tx_power": adv.tx_power, "found_by": "агент (bleak)"},
        })
    devices.sort(key=lambda d: -(d["rssi"] or -200))
    log(f"  найдено Bluetooth-устройств: {len(devices)}", found=devices)
    return devices


def check_ble(params, log):
    """Пробует подключиться к BLE-устройству и прочитать его сервисы."""
    from bleak import BleakClient

    address = params.get("bt_address")
    attempts = []

    async def run():
        async with BleakClient(address, timeout=20) as client:
            services, info = [], {}
            for service in client.services:
                chars = []
                for ch in service.characteristics:
                    chars.append(f"{short_uuid(ch.uuid)} [{', '.join(ch.properties)}]")
                    if short_uuid(service.uuid) == "180a" and "read" in ch.properties:
                        try:
                            value = (await client.read_gatt_char(ch.uuid)).decode("utf-8", "ignore").strip("\x00 ")
                            info[{"2a29": "manufacturer", "2a24": "model", "2a25": "serial",
                                  "2a26": "firmware"}.get(short_uuid(ch.uuid), short_uuid(ch.uuid))] = value
                        except Exception:
                            pass
                services.append({"uuid": short_uuid(service.uuid), "name": describe_service(service.uuid),
                                 "characteristics": chars[:15]})
            return services, info

    log(f"Подключаюсь к {address}…")
    try:
        services, info = asyncio.run(run())
    except Exception as exc:
        attempts.append({"protocol": "Bluetooth LE (GATT)", "ok": False, "detail": f"подключение не удалось: {exc}"})
        return {"ok": False, "message": "Устройство обнаружено, требуется настройка протокола", "attempts": attempts}
    attempts.append({"protocol": "Bluetooth LE (GATT)", "ok": True, "detail": f"подключение есть, сервисов: {len(services)}"})
    attempts.append({"protocol": "Чтение показаний", "ok": False,
                     "detail": "протокол показаний по Bluetooth у этого устройства неизвестен "
                               "(обычно BLE служит для настройки Wi-Fi логгера)"})
    log("Подключение по Bluetooth есть, сервисы прочитаны")
    return {"ok": False, "message": "Устройство обнаружено, требуется настройка протокола",
            "manufacturer": info.get("manufacturer", ""), "model": info.get("model", ""),
            "serial": info.get("serial", ""), "ble_services": services, "device_info": info, "attempts": attempts}
