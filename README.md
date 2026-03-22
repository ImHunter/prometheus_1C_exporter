# Prometheus 1C Exporter

Многофункциональный экспортер метрик 1С для Prometheus с расширенными возможностями управления сбором данных.

## 🔍 Возможности

- Сбор ключевых метрик 1С через утилиту `rac`:
  - Клиентские лицензии
  - Производительность серверов приложений
  - Активные соединения и сеансы
  - Ресурсы процессов (память, CPU)
  - Состояние дисковых операций (IOPS, latency)
  - Статус регламентных заданий
  - И другие [показатели производительности](#-метрики)

- Гибкое управление сбором метрик:
  - Выборочная приостановка сбора
  - Автоматическое возобновление
  - Раздельные эндпоинты для разных типов метрик

- Готовые примеры визуализации для Grafana
- Поддержка работы в качестве службы (Windows/Linux)

![Пример дашборда](doc/img/browser_d8CBonI15Y.png "Обзор метрик")
![Производительность серверов](doc/img/browser_FCaSoFVBDe.png "Доступная производительность")
![browser_V4ryXuTJoQ.png](doc/img/browser_V4ryXuTJoQ.png)
![browser_Vw8kZr5zb8.png](doc/img/browser_Vw8kZr5zb8.png)

## 📦 Установка

### Предварительные требования
- Go 1.19+ (для сборки из исходников)
- Доступ к утилите `rac`
- Prometheus 2.0+

### Способы установки:
1. **Готовые бинарники**:  
   [Скачать последний релиз](https://github.com/LazarenkoA/prometheus_1C_exporter/releases)

2. **Сборка из исходников**:
   ```bash
   git clone https://github.com/LazarenkoA/prometheus_1C_exporter
   cd prometheus_1C_exporter
   go build -o "1C_exporter"
   ```

## 🚀 Запуск

**Linux:**
```bash
./1C_exporter -port=9095 --settings=/path/to/settings.yaml
```

**Windows:**
```bash
./1C_exporter.exe -port=9095 --settings=/path/to/settings.yaml
```

Пример настроек [examples_settings.yaml](examples_settings.yaml)

## ⚙️ Конфигурация Prometheus

Добавьте в `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: '1c_metrics'
    scrape_interval: 30s
    metrics_path: '/metrics'
    static_configs:
      - targets: ['1c-server1:9091', '1c-server2:9091']
```

Опционально: раздельные задания для разных типов метрик

```yaml
scrape_configs:
  - job_name: '1c_os_metrics'
    scrape_interval: 10s
    metrics_path: '/metrics_os'
    static_configs:
      - targets: ['1c-server1:9091']

  - job_name: '1c_rac_metrics'
    scrape_interval: 30s
    metrics_path: '/metrics_rac'
    static_configs:
      - targets: ['1c-server1:9091']
```

## 🛠 Методы HTTP-сервиса

| Метод | URL-формат | Параметры | Описание |
|-------|------------|-----------|----------|
| GET | `/` | – | Информационная страница со списком доступных эндпоинтов |
| GET | `/metrics` | – | Основные метрики Prometheus (композиционные) |
| GET | `/metrics_os` | – | Метрики операционной системы (CPU, память, диски) |
| GET | `/metrics_rac` | – | Метрики RAC (лицензии, соединения, сеансы) |
| GET | `/Pause` | `metricNames`<br>`offsetMin` (опционально) | Приостанавливает сбор указанных метрик на заданное время (в минутах) |
| GET | `/Continue` | `metricNames` | Возобновляет сбор указанных метрик |
| GET | `/log` | `mode`, `n`, `from` | Читает содержимое лога: <br>`mode=first` – первые `n` строк,<br>`mode=last` – последние `n` строк,<br>`mode=range` – строки с `from` по `from+n-1` |
| GET | `/public-key` | – | Возвращает публичный ключ RSA в формате PEM для шифрования секретов |
| GET | `/debug/pprof/*` | – | Стандартные эндпоинты для профилирования Go |
| POST | `/set_config` | `file` (multipart/form-data) | Загружает новый конфигурационный файл (`settings.yml`) и применяет его без перезапуска |
| POST | `/set_binarypath` | тело запроса содержит URL | Устанавливает новый путь к бинарному файлу в конфигурации WinSW (используется для обновления) |
| POST | `/shutdown_emulate` | `exit_code` (опционально) | Аварийно завершает процесс с указанным кодом выхода (для триггера перезапуска WinSW) |
| POST | `/set_secrets` | JSON `{"encrypted_data": "base64..."}` | Принимает зашифрованные секреты, расшифровывает их и сохраняет в памяти |
| POST | `/encrypt-secrets` | JSON с открытыми секретами | Принимает открытые секреты, шифрует их публичным ключом и возвращает зашифрованный файл (бинарные данные) |

### Примеры использования

#### Через браузер (GET-запросы)

- **Главная страница** – отображает информацию о сервисе и список доступных эндпоинтов в формате JSON.  
  Откройте в браузере: `http://localhost:9091/`

- **Метрики Prometheus**  
  - Общие метрики: `http://localhost:9091/metrics`  
  - Метрики операционной системы: `http://localhost:9091/metrics_os`  
  - Метрики RAC: `http://localhost:9091/metrics_rac`  
  В браузере отобразится текстовый вывод в формате Prometheus.

- **Публичный ключ** – для получения PEM-ключа:  
  `http://localhost:9091/public-key`  
  Браузер покажет содержимое ключа (текст).

- **Чтение логов** – пример для просмотра последних 50 строк лога:  
  `http://localhost:9091/log?mode=last&n=50`  
  (можно менять параметры прямо в адресной строке)

- **Профилирование Go** – стандартные эндпоинты pprof:  
  `http://localhost:9091/debug/pprof/` – список профилей  
  `http://localhost:9091/debug/pprof/heap` – профиль памяти  
  и т.д. (доступны все стандартные pprof-маршруты)

- **Приостановка и возобновление сбора метрик** (параметры передаются в строке запроса):  
  Приостановить сбор метрик `processes,connections` на 5 минут:  
  `http://localhost:9091/Pause?metricNames=processes,connections&offsetMin=5`  
  Возобновить сбор метрик `disk_metrics`:  
  `http://localhost:9091/Continue?metricNames=disk_metrics`

#### Через curl (Linux, WSL, macOS)

- **Получение публичного ключа**
  ```bash
  curl http://localhost:9091/public-key > exporter.pub
  ```

- **Шифрование секретов с помощью эндпоинта `/encrypt-secrets`**
  ```bash
  curl -X POST http://localhost:9091/encrypt-secrets \
      -H "Content-Type: application/json" \
      -d @secrets.json \
      --output secrets.json.enc
  ```

- **Отправка зашифрованных секретов**
  ```bash
  curl -X POST http://localhost:9091/set_secrets \
      -H "Content-Type: application/json" \
      -d '{"encrypted_data": "'$(base64 -w0 secrets.json.enc)'"}'
  ```

- **Установка нового пути к бинарнику** (для обновления через WinSW)
  ```bash
  curl -X POST http://localhost:9091/set_binarypath \
      -H "Content-Type: text/plain" \
      --data "https://gitlab.example.com/path/to/new/exporter.exe"
  ```

- **Эмуляция аварийного завершения**
  ```bash
  curl -X POST "http://localhost:9091/shutdown_emulate?exit_code=0"
  ```

- **Чтение логов** (последние 50 строк)
  ```bash
  curl "http://localhost:9091/log?mode=last&n=50"
  ```

- **Загрузка нового конфигурационного файла** (с применением новых настроек)
  ```bash
  curl -X POST http://localhost:9091/set_config -F "file=@settings.yml"
  ```

- **Приостановка сбора метрик**
  ```bash
  curl "http://localhost:9091/Pause?metricNames=processes,connections&offsetMin=5"
  ```

- **Возобновление сбора метрик**
  ```bash
  curl "http://localhost:9091/Continue?metricNames=disk_metrics"
  ```

#### Через cmd (Windows)

Для Windows рекомендуется использовать команду `curl` (встроена в Windows 10/11) и встроенные утилиты `certutil`, `findstr` для работы с base64.

- **Получение публичного ключа**
  ```cmd
  curl -o exporter.pub http://localhost:9091/public-key
  ```

- **Кодирование зашифрованного файла в base64** (для ручной отправки)
  ```cmd
  certutil -encode secrets.json.enc secrets.tmp >nul && findstr /v /c:"BEGIN CERTIFICATE" /c:"END CERTIFICATE" secrets.tmp > secrets.b64 && del secrets.tmp
  ```
  После выполнения содержимое файла `secrets.b64` (без переноса строк) можно использовать как значение поля `encrypted_data`.

- **Отправка зашифрованных секретов**
  ```cmd
  set /p ENC=<secrets.b64
  curl -X POST http://localhost:9091/set_secrets -H "Content-Type: application/json" -d "{\"encrypted_data\": \"%ENC%\"}"
  ```

- **Установка нового пути к бинарнику** (для обновления через WinSW)
  ```cmd
  curl -X POST http://localhost:9091/set_binarypath -H "Content-Type: text/plain" --data "https://gitlab.example.com/path/to/new/exporter.exe"
  ```

- **Эмуляция аварийного завершения**
  ```cmd
  curl -X POST "http://localhost:9091/shutdown_emulate?exit_code=0"
  ```

- **Чтение логов** (последние 50 строк)
  ```cmd
  curl "http://localhost:9091/log?mode=last&n=50"
  ```

- **Загрузка нового конфигурационного файла** (с применением новых настроек)
  ```cmd
  curl -X POST http://localhost:9091/set_config -F "file=@settings.yml"
  ```

- **Приостановка сбора метрик**
  ```cmd
  curl "http://localhost:9091/Pause?metricNames=processes,connections&offsetMin=5"
  ```

- **Возобновление сбора метрик**
  ```cmd
  curl "http://localhost:9091/Continue?metricNames=disk_metrics"
  ```

#### Шифрование секретов с помощью готовых скриптов (рекомендуемый способ)

Для шифрования секретов без передачи открытых данных по сети подготовлены два скрипта: `encode.bat` (Windows) и `encrypt.sh` (Linux). Они используют публичный ключ экспортера и создают JSON-файл, совместимый с эндпоинтом `/set_secrets`. Скрипты требуют наличия `openssl` и базовых утилит.

**Скрипт `encode.bat` (Windows)** – сохраните как `encode.bat` в папку с `exporter.pub` и `secrets.json`:

```batch
@echo off
setlocal

set INPUT=secrets.json
set OUTPUT=secrets.json.enc
if not "%~1"=="" set INPUT=%~1
if not "%~2"=="" set OUTPUT=%~2

where openssl >nul 2>&1 || (echo ERROR: openssl not found & exit /b 1)
if not exist exporter.pub (echo ERROR: exporter.pub not found & exit /b 1)
if not exist "%INPUT%" (echo ERROR: input file "%INPUT%" not found & exit /b 1)

echo Generating AES key and IV...
openssl rand -hex 32 > aes_key.hex
openssl rand -hex 16 > iv.hex
if errorlevel 1 exit /b 1

set /p AES_KEY=<aes_key.hex
set /p IV=<iv.hex

echo Encrypting data with AES-256-CBC...
openssl enc -aes-256-cbc -K %AES_KEY% -iv %IV% -in "%INPUT%" -out encrypted_data.bin
if errorlevel 1 exit /b 1

echo Encrypting AES key with RSA...
openssl rsautl -encrypt -pubin -inkey exporter.pub -in aes_key.hex -out encrypted_key.bin
if errorlevel 1 exit /b 1

echo Encoding components to base64...
certutil -encode encrypted_key.bin enc_key.tmp >nul
findstr /v /c:"BEGIN CERTIFICATE" /c:"END CERTIFICATE" enc_key.tmp > encrypted_key.b64
del enc_key.tmp

certutil -encode iv.hex iv.tmp >nul
findstr /v /c:"BEGIN CERTIFICATE" /c:"END CERTIFICATE" iv.tmp > iv.b64
del iv.tmp

certutil -encode encrypted_data.bin data.tmp >nul
findstr /v /c:"BEGIN CERTIFICATE" /c:"END CERTIFICATE" data.tmp > encrypted_data.b64
del data.tmp

del aes_key.hex iv.hex encrypted_key.bin encrypted_data.bin

set /p ENCRYPTED_KEY=<encrypted_key.b64
set /p IV_B64=<iv.b64
set /p DATA_B64=<encrypted_data.b64

del encrypted_key.b64 iv.b64 encrypted_data.b64

(
echo {
echo   "encrypted_key": "%ENCRYPTED_KEY%",
echo   "iv": "%IV_B64%",
echo   "data": "%DATA_B64%"
}
) > "%OUTPUT%"

echo Done. Encrypted file: %OUTPUT%
```

**Скрипт `encrypt.sh` (Linux)** – сохраните как `encrypt.sh`, сделайте исполняемым (`chmod +x encrypt.sh`):

```bash
#!/bin/bash
set -e

INPUT="${1:-secrets.json}"
OUTPUT="${2:-secrets.json.enc}"

command -v openssl >/dev/null 2>&1 || { echo "ERROR: openssl not found"; exit 1; }
command -v base64 >/dev/null 2>&1 || { echo "ERROR: base64 not found"; exit 1; }
command -v xxd >/dev/null 2>&1 || { echo "ERROR: xxd not found. Install it (e.g., apt install xxd)."; exit 1; }

if [ ! -f exporter.pub ]; then
    echo "ERROR: exporter.pub not found"
    exit 1
fi

if [ ! -f "$INPUT" ]; then
    echo "ERROR: input file '$INPUT' not found"
    exit 1
fi

echo "Generating AES key and IV..."
openssl rand -out aes_key.bin 32
openssl rand -out iv.bin 16

AES_KEY_HEX=$(xxd -p -c 256 aes_key.bin)
IV_HEX=$(xxd -p -c 256 iv.bin)

echo "Encrypting data with AES-256-CBC..."
openssl enc -aes-256-cbc -K "$AES_KEY_HEX" -iv "$IV_HEX" -in "$INPUT" -out encrypted_data.bin

echo "Encrypting AES key with RSA..."
openssl pkeyutl -encrypt -pubin -inkey exporter.pub -in aes_key.bin -out encrypted_key.bin

echo "Encoding components to base64..."
ENCRYPTED_KEY_B64=$(base64 -w0 encrypted_key.bin)
IV_B64=$(base64 -w0 iv.bin)
DATA_B64=$(base64 -w0 encrypted_data.bin)

cat > "$OUTPUT" <<EOF
{
  "encrypted_key": "$ENCRYPTED_KEY_B64",
  "iv": "$IV_B64",
  "data": "$DATA_B64"
}
EOF

rm -f aes_key.bin iv.bin encrypted_key.bin encrypted_data.bin

echo "Done. Encrypted file: $OUTPUT"
```

**Использование скриптов:**
1. Получите публичный ключ экспортера:  
   `curl -o exporter.pub http://<exporter-address>:<port>/public-key`
2. Подготовьте файл `secrets.json` с открытыми секретами (UTF-8).
3. Запустите скрипт:
   - Windows: `encode.bat secrets.json` (или `encode.bat my.json my.enc`)
   - Linux: `./encrypt.sh secrets.json` (или `./encrypt.sh my.json my.enc`)
4. Полученный файл `secrets.json.enc` (JSON) закоммитьте в соответствующую ветку GitLab.

## 📊 Метрики

### Основные категории

| Категория      | Метрики                       | Эндпоинт |
|----------------|-------------------------------|----------|
| Системные      | CPU, память, диски             | `/metrics_os` |
| RAC-метрики    | Лицензии, соединения, сеансы   | `/metrics_rac` |
| Композиционные | Все метрики                    | `/metrics` |

### Детализация метрик

| Метрика | Описание | Тип данных |
|---------|----------|------------|
| `available_performance` | Доступная производительность хоста | SummaryVec |
| `sessions_data` | Показатели сессий из кластера 1С | SummaryVec |
| `session` | Сессии 1С | SummaryVec и/или GaugeVec |
| `connect` | Соединения 1С | SummaryVec |
| `client_lic` | Клиентские лицензии 1С | SummaryVec |
| `shedule_job` | Состояние галки "блокировка регламентных заданий", если галка установлена значение будет 1 иначе 0 или метрика будет отсутствовать | Gauge |
| `cpu` | Метрики CPU (общий процент загрузки процессора) | SummaryVec |
| `processes` | Метрики CPU/памяти в разрезе процессов | SummaryVec |
| `disk` | Показатели дисков | SummaryVec |

## 📈 Примеры запросов PromQL

- Клиентские лицензии:
  ```
  sum by (licSRV) (client_lic{quantile="0.99", licSRV=~"(?i).+sys.+"})
  ```

- Средняя загрузка CPU:
  ```
  avg_over_time(cpu{quantile="0.99"} [1m])
  ```

- Загрузка CPU в разрезе процессов:
  ```
  topk(10, sum(avg_over_time(processes{quantile="0.99", metrics="cpu"}[1m])) by (procName) )
  ```

- Загрузка ОЗУ в разрезе процессов:
  ```
  topk(10, sum(avg_over_time(processes{quantile="0.99", metrics="memoryRSS"}[1m])) by (procName) )
  ```

- Доступная производительность 1С:
  ```
  avg_over_time(available_performance{quantile="0.99"}[10m])
  ```

- Количество сеансов в 1С:
  ```
  session{quantile="0.99"}
  ```

- CPU time (консоль 1С)
  ```
  rate(sessions_data{quantile="0.99", datatype="cputimetotal"}[5m])
  ```

## ⚠️ Локализация ошибок

При возникновении проблем проверьте:
- Доступность RAC-утилиты
- Права на чтение конфигурационного файла
- Открытые порты в firewall
- Логи приложения (режим отладки через установку уровня логирования `LogLevel: 5` в конфигурационном файле)

## 🔄 Интеграция с self-hosted GitLab

Для централизованного управления конфигурацией и секретами экспортер может взаимодействовать с GitLab CI/CD. Это позволяет автоматически обновлять настройки и безопасно доставлять учетные данные для баз 1С.

### 1. Общие настройки

Перед настройкой автоматизации выполните следующие шаги:

1. **Создайте отдельный GitLab-проект**, в котором будут храниться конфигурационные файлы для каждого экземпляра экспортера.
2. **Для каждого экземпляра создайте отдельную ветку** (например, `web`, `rls`, `trade`). В каждой ветке будут находиться:
   * `settings.yml` – основной конфигурационный файл.
   * `secrets.json.enc` – зашифрованные секреты (опционально).
   * `exporter-vars.yml` – файл с переменными для CI/CD (обязателен).
3. **Содержимое `exporter-vars.yml`**:
   ```yaml
   variables:
     SERVER_URL: "http://server-web:9091"   # базовый URL экспортера (без пути)
     # FILE_TO_UPLOAD: "settings.yml"       # опционально: имя файла конфигурации, по умолчанию settings.yml
     # SECRETS_FILE: "secrets.json.enc"     # опционально: имя файла секретов, по умолчанию secrets.json.enc
   ```

4. **В каждой ветке** создайте файл `main-pipeline.yml` с общим кодом пайплайна (см. ниже). Поскольку пайплайн берется из текущей ветки, при необходимости можно иметь разные версии, но рекомендуется синхронизировать их через merge из `main`.
5. **В настройках CI/CD проекта** (Settings → CI/CD → General pipelines) в поле **CI/CD configuration file** (Файл конфигурации CI/CD) укажите имя файла пайплайна:  
   `main-pipeline.yml`  
   (без указания ветки, так как файл должен присутствовать во всех ветках).

**Пример `main-pipeline.yml`:**

```yaml
include:
  - local: 'exporter-vars.yml'

stages:
  - upload

default:
  image: alpine:latest
  before_script:
    - apk add --no-cache curl

upload_file:
  stage: upload
  rules:
    - if: $CI_COMMIT_BRANCH != "main"
      changes:
        - settings.yml
      when: always
    - when: never
  script:
    - FILE_TO_UPLOAD=${FILE_TO_UPLOAD:-settings.yml}
    - echo "Uploading $FILE_TO_UPLOAD to $SERVER_URL"
    - curl -X POST -F "file=@$FILE_TO_UPLOAD" "$SERVER_URL/set_config" --fail

send_secrets:
  stage: upload
  rules:
    - if: $CI_PIPELINE_SOURCE == "trigger"
    - changes:
        - secrets.json.enc
      when: always
  script:
    - SECRETS_FILE=${SECRETS_FILE:-secrets.json.enc}
    - if [ ! -f "$SECRETS_FILE" ]; then echo "Secrets file not found"; exit 1; fi
    - ENCRYPTED_BASE64=$(base64 -w0 "$SECRETS_FILE")
    - |
      curl -X POST "$SERVER_URL/set_secrets" \
        -H "Content-Type: application/json" \
        -d '{"encrypted_data": "'"$ENCRYPTED_BASE64"'"}' \
        --fail --silent --show-error
    - echo "Secrets delivered"
```

### 2. Обновление конфигурационного файла

При каждом изменении `settings.yml` (или файла, указанного в `FILE_TO_UPLOAD`) в любой ветке (кроме `main`) автоматически запускается job `upload_file`, который отправляет новый конфиг на экспортер через метод `/set_config`. Экспортер применяет новые настройки без перезапуска.

Для корректной работы убедитесь, что:
* В файле `exporter-vars.yml` ветки определена переменная `SERVER_URL`.
* Ветка содержит актуальный `main-pipeline.yml`.

### 3. Безопасное хранение и доставка секретов

Для работы с учетными данными баз 1С (логины/пароли) используется следующий механизм:

* **Генерация ключей** – при первом запуске экспортер создает пару RSA-ключей (3072 бит) и сохраняет их в подкаталоге `keys/<host>_<port>/` рядом с исполняемым файлом. Приватный ключ защищен правами доступа (0600).
* **Шифрование секретов** – администратор может зашифровать секреты с помощью готовых скриптов `encode.bat` (Windows) или `encrypt.sh` (Linux), которые используют публичный ключ экспортера и создают JSON-файл, совместимый с `/set_secrets`. Формат секретов:
  ```json
  {
    "ras": { "login": "admin", "password": "ras_pass" },
    "ibase_default": { "login": "default_user", "password": "default_pass" },
    "ibases": {
      "main_db": { "login": "main_user", "password": "main_pass" }
    }
  }
  ```
* **Размещение в GitLab** – зашифрованный файл (например, `secrets.json.enc`) помещается в соответствующую ветку репозитория.
* **Автоматическая доставка** – при изменении файла секретов или по триггеру от экспортера запускается job `send_secrets`, который отправляет зашифрованные данные на эндпоинт `/set_secrets`. Экспортер расшифровывает их и сохраняет в памяти, а также дублирует на диск (в ту же папку, где лежат ключи) для использования при холодном старте.
* **Использование секретов** – методы `GetLogPass` и `RAC_Login/Pass` автоматически подставляют полученные учетные данные; если секреты не заданы, используется старый механизм (plain).

**Важно:** При первом запуске после настройки GitLab-режима экспортер не имеет секретов. Администратор должен вручную (или через CI) инициировать первый запуск пайплайна, либо дождаться автоматического триггера после коммита файла секретов.

## 📌 Примечания

*   Для работы с секретами требуется наличие публичного ключа экспортера. Получить его можно через эндпоинт `/public-key` (см. примеры выше).
*   В режиме GitLab экспортер не требует периодического обновления секретов – они доставляются при каждом изменении файла в репозитории.
*   При перезагрузке экспортер загружает последние сохраненные секреты с диска, поэтому после перезапуска он сразу готов к работе, даже если GitLab временно недоступен.
*   Ветки экспортеров содержат только файлы настроек и переменных; код пайплайна хранится в каждой ветке и используется из нее. Для централизованного управления рекомендуется поддерживать `main-pipeline.yml` синхронизированным во всех ветках.