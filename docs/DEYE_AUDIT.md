# Проверка интеграции с Deye Cloud · 22 ноября 2026

В присланном Swagger EU перечислены 40 операций. Для всех 40 Go-клиент отправил ожидаемый HTTP-метод и путь локальному тестовому серверу и прочитал синтетический ответ об успехе. Из CRM доступны 39 операций, включая автоматическое получение токена. Повторный прогон с сохранённым логином подтвердил успешные авторизованные ответы реального облака, результаты приведены ниже.

## Повторная проверка после сохранения логина и пароля

Логин и SHA-256 пароля сохранены в зашифрованной записи аккаунта № 3 и проверены повторным чтением. Реальное облако успешно выдало токен и сведения об организации, список содержит 4 станции.

Для первых трёх станций успешно выполнены detail, latest, history, history/power, alertList и station/device. Для их инверторов успешно прочитаны latest, measurePoints, history, historyRaw и alertList. Настройки battery/system/tou успешно прочитаны у инверторов второй и третьей станции. У первого инвертора все три запроса настроек вернули код Deye `2104006`. Запрос measurePoints для логгеров типа COLLECTOR вернул `2104005`, для инверторов он успешен.

В конце последовательного прогона запросы четвёртой станции, listWithDevice и device/list вернули `1001001`. После паузы повторный listWithDevice прошёл успешно. Повторный device/list с личным токеном вернул `2101019`, `auth invalid token`. По Swagger этот метод предназначен для бизнес-участника и требует соответствующего контекста организации. Повторная проверка четвёртой станции остаётся открытой. Аккаунт сохранён с прежним значением `is_active=false`. Команды записи и создание объектов проверены только локально.

## Первый прогон до сохранения логина и пароля

- **Учётные данные.** Прочитали аккаунт № 3 через штатное зашифрованное хранилище, выводя только признаки заполненности. App ID и App Secret присутствуют. При повторной проверке после сообщения о сохранении логин и хеш пароля оставались пустыми. Вызов `AccountInfo` через этот аккаунт остановился при получении токена до сетевого запроса. В истории аккаунта есть только событие создания.
- **Связь с облаком.** Контрольный запрос без токена на `POST https://eu1-developer.deyecloud.com/v1.0/account/info` получил HTTP 200, `success=false`, код Deye `2101017` и сообщение `auth token not found`. Это подтверждает доступность EU API и показывает, почему одного HTTP-статуса недостаточно.
- **Фоновый сбор.** На момент проверки провайдер включён, аккаунт выключен. В локальной базе 0 станций каталога Deye и 0 импортированных модемов Deye. Проверка входа доступна и у выключенного аккаунта. Для фонового сбора нужен активный аккаунт, а для показаний — импортированный инвертор.
- **Изменения оборудования.** Регистрация и удаление логгеров, создание станций, команды записи и запросы, создающие задание на инверторе, выполнялись только на тестовом сервере.

## Расхождения между Deye и CRM

| Операция Deye | Что это означает для пользователя CRM |
|---|---|
| `POST /v1.0/order/customControl` | Метод есть в Go-клиенте. В таблице разрешённых команд CRM его нет. Попытка обратиться к нему через универсальный маршрут управления получает локальный 404 «Неизвестная команда». |
| `POST /v1.0/device/alertList`, `POST /v1.0/device/register`, `GET /v1.0/order/{orderId}` | Deye описывает необязательный параметр запроса `language`. Клиент CRM его не передаёт. Запросы используют язык, выбранный облаком по умолчанию. |
| `POST /v1.0/device/latest`, `POST /v1.0/device/alertList` | Обработчики CRM передают серийный номер модема, но не позволяют выбрать необязательный `deviceType` для уточнения типа устройства. Для `latest` облачный клиент также поддерживает пакет до 10 серийных номеров, а маршрут CRM обслуживает один модем. |

## Результаты по операциям

В колонке CRM `A` означает `/api/deye/accounts/{id}`, где нужен ID аккаунта. `M` означает `/api/modems/{id}/deye`, где нужен ID модема. **ОК** означает, что конкретный метод Go-клиента прошёл локальную проверку транспорта и разбора ответа. Доступность операции для конкретной организации, модели устройства и набора разрешений Deye требует проверки с действующим токеном.

| Операция Deye | Метод Go | Проверка на стенде | Вызов из CRM |
|---|---|---|---|
| `POST /v1.0/account/info` | `AccountInfo` | ОК | `POST A/test/` |
| `POST /v1.0/account/token` | `ObtainToken` | ОК | Автоматически при обращении к облаку |
| `POST /v1.0/config/battery` | `ConfigBattery` | ОК | `GET M/config/?part=battery` |
| `POST /v1.0/config/system` | `ConfigSystem` | ОК | `GET M/config/?part=system` |
| `POST /v1.0/config/tou` | `ConfigTOU` | ОК | `GET M/config/?part=tou` |
| `POST /v1.0/device/addLogger` | `AddLoggers` | ОК | `POST A/loggers/add/` |
| `POST /v1.0/device/alertList` | `DeviceAlertList` | ОК | `GET M/alerts/` |
| `POST /v1.0/device/deleteLogger` | `DeleteLoggers` | ОК | `POST A/loggers/delete/` |
| `POST /v1.0/device/history` | `DeviceHistory` | ОК | `POST M/history/` |
| `POST /v1.0/device/historyRaw` | `DeviceHistoryRaw` | ОК | `POST M/history-raw/` |
| `POST /v1.0/device/latest` | `DeviceLatest` | ОК | `GET M/latest/` |
| `POST /v1.0/device/list` | `DeviceList` | ОК | `GET A/devices/` |
| `POST /v1.0/device/measurePoints` | `DeviceMeasurePoints` | ОК | `GET M/measure-points/` |
| `POST /v1.0/device/register` | `DeviceRegister` | ОК | `POST A/devices/register/` |
| `POST /v1.0/order/battery/modeControl` | `BatteryModeControl` | ОК | `POST M/control/battery-mode/` |
| `POST /v1.0/order/battery/parameter/update` | `BatteryParameterUpdate` | ОК | `POST M/control/battery-parameter/` |
| `POST /v1.0/order/battery/type/update` | `BatteryTypeUpdate` | ОК | `POST M/control/battery-type/` |
| `POST /v1.0/order/customControl` | `CustomControl` | ОК | Нет команды в CRM |
| `POST /v1.0/order/gridPeakShaving/control` | `GridPeakShavingControl` | ОК | `POST M/control/peak-shaving/` |
| `POST /v1.0/order/smartload/update` | `SmartLoadUpdate` | ОК | `POST M/control/smartload/` |
| `POST /v1.0/order/sys/energyPattern/update` | `EnergyPatternUpdate` | ОК | `POST M/control/energy-pattern/` |
| `POST /v1.0/order/sys/limitControl` | `LimitControl` | ОК | `POST M/control/limit-control/` |
| `POST /v1.0/order/sys/power/update` | `PowerUpdate` | ОК | `POST M/control/power/` |
| `POST /v1.0/order/sys/solarSell/control` | `SolarSellControl` | ОК | `POST M/control/solar-sell/` |
| `POST /v1.0/order/sys/tou/switch` | `TOUSwitch` | ОК | `POST M/control/tou-switch/` |
| `POST /v1.0/order/sys/tou/update` | `TOUUpdate` | ОК | `POST M/control/tou/` |
| `POST /v1.0/order/sys/workMode/update` | `WorkModeUpdate` | ОК | `POST M/control/work-mode/` |
| `GET /v1.0/order/{orderId}` | `GetOrder` | ОК | `GET M/orders/{orderId}/` |
| `POST /v1.0/station/alertList` | `StationAlertList` | ОК | `GET M/station-alerts/` |
| `POST /v1.0/station/create` | `StationCreate` | ОК | `POST A/stations/create/` |
| `POST /v1.0/station/detail` | `StationDetail` | ОК | `GET A/stations/{sid}/` |
| `POST /v1.0/station/device` | `StationDevices` | ОК | `GET A/stations/{sid}/devices/` |
| `POST /v1.0/station/history` | `StationHistory` | ОК | `POST M/station-history/` |
| `POST /v1.0/station/history/power` | `StationHistoryPower` | ОК | `POST M/station-history-power/` |
| `POST /v1.0/station/latest` | `StationLatest` | ОК | `GET M/station/` |
| `POST /v1.0/station/list` | `StationList` | ОК | `GET A/stations/` |
| `POST /v1.0/station/listWithDevice` | `StationListWithDevice` | ОК | `GET A/stations/?with_devices=1` |
| `POST /v1.0/strategy/dynamicControl` | `DynamicControl` | ОК | `POST M/control/dynamic/` |
| `POST /v1.0/strategy/dynamicControl/read` | `DynamicControlRead` | ОК | `POST M/dynamic/read/` |
| `POST /v1.0/strategy/dynamicControl/readResult` | `DynamicControlReadResult` | ОК | `GET M/dynamic/result/{orderId}/` |

## Как проводилась сверка

Локальный HTTP-сервер принял вызов каждого публичного метода из таблицы. Проверялись HTTP-метод, путь, заголовок Bearer, параметр `appId` при выдаче токена, JSON переданных полей и разбор успешного ответа с вложенными данными. Примеры синтетические и предназначены для проверки структуры. Тип `expiresIn` клиент приводит к числу секунд. Во временной копии присланного Swagger исправлены кавычки вокруг одного строкового значения в примере `latestDataRequest.deviceList` для чтения JSON. Дополнительно прошли штатные тесты `go test -count=1 ./pkg/deye ./services ./server ./docs`.

Для продолжения проверки чтения достаточно заполнить в аккаунте № 3 логин и пароль Deye и получить успешный ответ на `POST /api/deye/accounts/3/test/`. Для проверки истории и конфигурации нужны станция и серийный номер, доступные этому аккаунту. Для проверки результата ранее отправленной команды нужен её `orderId`.

[Клиент Deye](../pkg/deye) · [Обработчики CRM](../handlers/api/deye.go) · [Руководство по интеграциям](INTEGRATIONS.md) · [Портал разработчика Deye](https://developer.deyecloud.com/start)
