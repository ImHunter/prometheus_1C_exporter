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

- **Новое: Интеграция с GitLab** – централизованное удаленное управление конфигурацией и секретами, автоматическая доставка изменений, автообновление бинарного файла. Все операции выполняются без доступа на сервер, где работает экспортер.

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

```cmd
1C_exporter.exe -port=9095 --settings=/path/to/settings.yaml
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

## 🛠 Управление сбором метрик

|Метод|URL-формат|Параметры|Описание|
|---|------------|-----------|----------|
| GET | `/` | – | Информационная страница со списком доступных эндпоинтов |
| GET | `/metrics` | – | Основные метрики Prometheus (композиционные) |
| GET | `/metrics_os` | – | Метрики операционной системы (CPU, память, диски) |
| GET | `/metrics_rac` | – | Метрики RAC (лицензии, соединения, сеансы) |
| GET | `/metrics_internal` | – | Метрики работы экспортера |
 GET   | `/Pause`   | `metricNames` (обязательный, разделенный запятыми)<br/>`offsetMin` (опционально, минуты) | Приостанавливает сбор указанных метрик на заданное время (по умолчанию 5 минут). |
| GET   | `/Continue`| `metricNames` (обязательный, разделенный запятыми) | Возобновляет сбор указанных метрик. |

**Примеры:**
```
http://host:9091/Pause?metricNames=processes,connections&offsetMin=5
http://host:9091/Continue?metricNames=disk_metrics
```

## 📊 Метрики

### Основные категории

| Категория      | Метрики                       | Эндпоинт |
|----------------|-------------------------------|----------|
| Системные      | CPU, память, диски             | `/metrics_os` |
| RAC-метрики    | Лицензии, соединения, сеансы   | `/metrics_rac` |
| Композиционные | Все метрики                    | `/metrics` |

### Детализация метрик

|Метрика|Описание|Тип данных|
|---|---|---|
|`available_performance`|Доступная производительность хоста|Summary|
|`sessions_data`|Показатели сессий из кластера 1С|Summary, Gauge, NativeHistogram|
|`session`|Сессии 1С|Summary, Gauge|
|`connect`|Соединения 1С|Summary|
|`client_lic`|Клиентские лицензии 1С|Summary|
|`shedule_job`|Состояние галки "блокировка регламентных заданий" (1 – установлена, иначе 0 или отсутствует)|Gauge|
|`ibinfo`|Состояние галок "Блокировка регламентных заданий", "Запрет начала сеансов" (1 – установлена, иначе 0 или отсутствует)|Gauge|
|`cpu`|Метрики CPU (общий процент загрузки)|Summary|
|`processes`|Метрики CPU/памяти в разрезе процессов|Summary|
|`disk`|Показатели дисков|Summary|

## 📈 Примеры запросов PromQL

Клиентские лицензии:

```PromQL
sum by (licSRV) (client_lic{quantile="0.99", licSRV=~"(?i).+sys.+"})
```

Средняя загрузка CPU:

```PromQL
avg_over_time(cpu{quantile="0.99"} [1m])
```

Загрузка CPU в разрезе процессов:

```PromQL
topk(10, sum(avg_over_time(processes{quantile="0.99", metrics="cpu"}[1m])) by (procName) )
```

Загрузка ОЗУ в разрезе процессов:

```PromQL
topk(10, sum(avg_over_time(processes{quantile="0.99", metrics="memoryRSS"}[1m])) by (procName) )
```

Доступная производительность 1С:

```PromQL
avg_over_time(available_performance{quantile="0.99"}[10m])
```

Количество сеансов в 1С:

```PromQL
session{quantile="0.99"}
```

CPU time (консоль 1С):

```PromQL
rate(sessions_data{quantile="0.99", datatype="cputimetotal"}[5m])
```

## 🔧 Новые HTTP-методы для удаленного управления

Экспортер предоставляет дополнительные эндпоинты для удаленного управления конфигурацией, секретами и жизненным циклом. Все методы доступны без доступа на сервер.

### Управление конфигурацией

|Метод|URL|Параметры / Тело|Описание|
|---|---|---|---|
|POST|`/config/set`|multipart/form-data, поле `file` | Загружает новый конфигурационный файл `settings.yml`. Применяет настройки без перезапуска.|
|GET|`/config/get`|–|Возвращает текущий конфигурационный файл `settings.yml`.|

### Управление секретами

|Метод|URL|Параметры / Тело|Описание|
|---|---|---|---|
|GET|`/secrets/pub_key`|–|Возвращает публичный ключ RSA в формате PEM.|
|POST|`/secrets/encrypt`|JSON с открытыми секретами|Шифрует открытые секреты публичным ключом экспортера, возвращает зашифрованный пакет.|
|POST|`/secrets/set`|Бинарные данные (зашифрованный JSON) | Принимает зашифрованные секреты, расшифровывает и сохраняет их в памяти и на диск.|
|GET|`/secrets`|–|Возвращает информацию о загруженных секретах (метаданные, список баз, время обновления).|

### Управление жизненным циклом и диагностика

|Метод|URL|Параметры / Тело|Описание|
|---|---|---|---|
|POST|`/shutdown_emulate`|`?exit_code=N` (опционально, по умолчанию 1)|Аварийно завершает процесс с указанным кодом.|
|GET|`/log`|`mode=first/last/range`, `n`, `from`|Читает содержимое лога (первые/последние строки или диапазон).|

**Примеры использования:**

Загрузка конфигурации:

```bash
curl -X POST -F "file=@settings.yml" http://exporter:9091/config/set
```

Получение публичного ключа:

```bash
curl http://exporter:9091/secrets/pub_key > exporter.pub
```

Шифрование секретов (резервный способ – см. предупреждение ниже):

```bash
curl -X POST http://exporter:9091/secrets/encrypt -H "Content-Type: application/json" -d @secrets.json --output secrets.json.enc
```

Отправка зашифрованных секретов:

```bash
curl -X POST http://exporter:9091/secrets/set --data-binary @secrets.json.enc
```

Принудительный перезапуск экспортера (для применения обновления):

```bash
curl -X POST "http://exporter:9091/shutdown_emulate?exit_code=1"
```

Чтение последних 50 строк лога:

```bash
curl "http://exporter:9091/log?mode=last&n=50"
```

> **Предупреждение:** Шифрование через эндпоинт `/secrets/encrypt` передает открытые секреты по HTTP. Используйте этот способ только в защищенных сетях (например, через VPN) или как резервный. Для повышения безопасности предпочтительно локальное шифрование (см. раздел «Локальное шифрование»).

## 🔄 Интеграция с GitLab и централизованное удаленное управление

Все новые возможности предназначены для **удаленного управления экспортерами без необходимости доступа на сервер**, где они работают. Администратор может изменять настройки, секреты и обновлять бинарный файл, не заходя на сервер, – через GitLab‑репозиторий и CI/CD.

Для этого используется отдельный GitLab‑проект (далее «проект конфигураций»), в котором для каждого экземпляра экспортера создается отдельная ветка. **Главная ветка должна называться `main`** – это имя используется в скрипте пайплайна. Ветка `main` предназначена **только для хранения и доработки скрипта пайплайна** (`main-pipeline.yml`). В ней не должно быть конфигурационных файлов экспортеров.

### 1. Настройка GitLab в конфигурации экспортера

В файл `settings.yml` добавьте секцию:

```yaml
GitLab:
  Home: "https://gitlab.my-company.ru"      # базовый URL GitLab
  ProjectID: 123456                         # числовой ID проекта конфигураций
  Branch: "ut-prod"                         # ветка, соответствующая этому экспортеру
  SecretsFile: "secrets.json.enc"           # имя файла с секретами (опционально)
```

**Токены доступа** не указываются в `settings.yml`. Они хранятся в отдельном файле `tokens.yml`, расположенном в той же папке, что и бинарник экспортера. Структура `tokens.yml`:

```yaml
TriggerToken: "glptt-..."   # Trigger token для запуска pipeline (создается в настройках CI/CD)
ProjectToken: "glpat-..."   # Project Access Token с правами api и write_repository
```

При первом запуске экспортер попытается загрузить токены из `tokens.yml`. Если файл отсутствует, экспортер продолжит работу без интеграции с GitLab. Токены также могут быть переданы через HTTP-заголовки `TRIGGER-TOKEN` и `PROJECT-TOKEN` при вызовах эндпоинтов `/config/set`, `/secrets/set`, `/shutdown_emulate` – они будут сохранены в `tokens.yml` автоматически.

### 2. Режимы получения секретов

Экспортер поддерживает два режима получения учетных данных для информационных баз 1С:

- **`external`** (по умолчанию) – секреты запрашиваются с внешнего HTTP-сервиса, указанного в `DBCredentials.URL`.
- **`internal`** – секреты хранятся в зашифрованном виде и доставляются через GitLab (или вручную через эндпоинт `/secrets/set`).

Режим задается в секции `DBCredentials`:

```yaml
DBCredentials:
  Source: "internal"   # или "external"
  # URL, User, Password – используются только в режиме external
```

При `Source: "internal"` экспортер игнорирует внешний REST-сервис и полагается на переданные зашифрованные секреты.

### 3. Управление секретами (шифрование)

Экспортер генерирует пару RSA-ключей (3072 бит) при первом запуске. Ключи хранятся в подкаталоге `keys/<host>_<port>/` рядом с бинарником. Публичный ключ можно получить через `/secrets/pub_key`.

**Формат открытых секретов (JSON):**

```json
{
  "ras": { "login": "admin", "password": "ras_pass" },
  "ibase_default": { "login": "default_user", "password": "default_pass" },
  "ibases": {
    "main_db": { "login": "main_user", "password": "main_pass" }
  }
}
```

- `ras` – учетные данные для кластера (переопределяют `RAC.Login`/`RAC.Pass` из `settings.yml`).
- `ibase_default` – логин/пароль по умолчанию для всех информационных баз.
- `ibases` – переопределения для конкретных баз (ключ – имя базы).

#### Шифрование секретов через экспортер (резервный способ)

Этот способ удобен, но передает открытые секреты по HTTP. Используйте его только в доверенных сетях или как резервный.

```bash
curl -X POST http://exporter:9091/secrets/encrypt \
     -H "Content-Type: application/json" \
     -d @secrets.json --output secrets.json.enc
```

#### Локальное шифрование (рекомендовано)

Для сред без доступа к экспортеру или при работе в изолированной сети можно использовать bash‑скрипт `encrypt.sh`. Он работает локально, не передавая открытые секреты по сети.

1. Получите публичный ключ экспортера: `curl http://exporter:9091/secrets/pub_key > exporter.pub`
2. Подготовьте `secrets.json` с открытыми секретами.
3. Создайте `encrypt.sh` (см. ниже) и выполните `./encrypt.sh secrets.json`. **Внимание**. Это скрипт bash. Для выполнения скрипта в Windows, следует пользоваться консолью bash - например, из поставки клиента Git.

Код `encrypt.sh`:

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
openssl pkeyutl -encrypt -pubin -inkey exporter.pub -in aes_key.bin -out encrypted_key.bin -pkeyopt rsa_padding_mode:oaep -pkeyopt rsa_oaep_md:sha256

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

**После шифрования** полученный файл `secrets.json.enc` помещается в соответствующую ветку репозитория конфигураций.

### 4. Автообновление экспортера (self-update)

Экспортер может автоматически обновляться до новой версии, если в GitLab-проекте создан **релиз** с бинарными файлами, имена которых соответствуют шаблону:

- `1C_exporter_linux_amd64`
- `1C_exporter_windows_amd64.exe`

**Процесс обновления:**

1. При запуске экспортер проверяет наличие новой версии (по тегу) в указанном проекте.
2. Если найдена более новая версия, экспортер заменяет свой бинарник и завершается с кодом `1`.
3. Служба (systemd или Windows SCM) должна быть настроена на автоматический перезапуск после сбоя. После перезапуска экспортер работает уже с новой версией.

**Настройка перезапуска службы:**

- **Windows (sc):**  
  ```cmd
  sc failure prometheus_1C_exporter reset= 86400 actions= restart/5000
  ```
- **Linux (systemd):** в файле службы `/etc/systemd/system/1c_exporter.service` добавьте:
  ```ini
  [Service]
  Restart=on-failure
  RestartSec=5
  ```

### 5. Централизованное управление через GitLab CI/CD

В проекте конфигураций создаются:

- Файл `exporter-vars.yml` с переменными для конкретного экспортера (соответственно, располагать в нужной ветке репозитория):
  ```yaml
  variables:
    SERVER_URL: "http://exporter-host:9091"
    FILE_TO_UPLOAD: "settings.yml"
    SECRETS_FILE: "secrets.json.enc"
  ```
- Файл `main-pipeline.yml` – скрипт пайплайна (полный код приведен ниже).

Пайплайн выполняет следующие задачи:

- При коммите `settings.yml` в ветку (кроме `main`) – отправляет новый конфиг на `/config/set`.
- При коммите `secrets.json.enc` или по триггеру от экспортера – отправляет секреты на `/secrets/set`.
- При создании тега (релиза) – перезапускает все экспортеры через `/shutdown_emulate`.
- При изменении `main-pipeline.yml` в ветке `main` – копирует его во все остальные ветки.

**Для работы пайплайна** в проекте GitLab должны быть созданы токены доступа:

- Токен вида Project Access Token (права `api`, `write_repository`, роль Developer)
- Токен вида Trigger token, создается в настройках CI/CD

**Для работы пайплайна** в проекте GitLab должны быть определены переменные CI/CD:

- `PROJECT_TOKEN` – Хранит токен вида Project Access Token, см.выше
- `TRIGGER_TOKEN` – Хранит токена вид Trigger token, см.выше

#### Полный листинг `main-pipeline.yml`

```yaml
include:
  - local: 'exporter-vars.yml'

stages:
  - upload
  - restart
  - propagate

default:
  image: alpine/curl:8.17.0

upload_file:
  stage: upload
  rules:
    - if: $CI_PIPELINE_SOURCE == "push" && $CI_COMMIT_TAG == null && $CI_COMMIT_BRANCH != "main"
      changes:
        - settings.yml
      when: always
    - when: never
  script:
    - |
      FILE_TO_UPLOAD=${FILE_TO_UPLOAD:-settings.yml}
      echo "Uploading $FILE_TO_UPLOAD to $SERVER_URL"
      curl -X POST -F "file=@$FILE_TO_UPLOAD" "$SERVER_URL/config/set" --header "TRIGGER-TOKEN: $TRIGGER_TOKEN" --header "PROJECT-TOKEN: $PROJECT_TOKEN" --fail

send_secrets:
  stage: upload
  rules:
    - if: $CI_PIPELINE_SOURCE == "trigger"
      when: always
    - if: $CI_PIPELINE_SOURCE == "push" && $CI_COMMIT_TAG == null && $CI_COMMIT_BRANCH != "main"
      changes:
        - secrets.json.enc
      when: always
    - when: never
  script:
    - |
      echo "CI_PIPELINE_SOURCE: $CI_PIPELINE_SOURCE CI_COMMIT_TAG: $CI_COMMIT_TAG CI_COMMIT_BRANCH: $CI_COMMIT_BRANCH"
      SECRETS_FILE=${SECRETS_FILE:-secrets.json.enc}
      if [ ! -f "$SECRETS_FILE" ]; then echo "Secrets file not found"; exit 1; fi
      curl -X POST "$SERVER_URL/secrets/set" --data-binary "@$SECRETS_FILE" --header "TRIGGER-TOKEN: $TRIGGER_TOKEN" --header "PROJECT-TOKEN: $PROJECT_TOKEN" --fail
      echo "Secrets delivered"

restart_exporters:
  stage: restart
  rules:
    - if: $CI_COMMIT_TAG != null
      when: always
    - if: $CI_PIPELINE_SOURCE == "web"
      when: manual
    - when: never
  script:
    - |
      echo "Fetching branches..."
      URL="$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/branches?per_page=100"
      curl --silent --show-error --header "PRIVATE-TOKEN: $PROJECT_TOKEN" "$URL" > branches.json

      if grep -q '"error"' branches.json; then
        echo "ERROR: API returned error:"
        cat branches.json
        exit 1
      fi

      BRANCHES=$(grep -o '"name":"[^"]*"' branches.json | sed 's/"name":"//;s/"//' | grep -v '^main$')

      for branch in $BRANCHES; do
        echo "Processing branch: $branch"

        RAW_CONTENT=$(curl --silent --show-error --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
          "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/files/exporter-vars.yml/raw?ref=$branch" \
          --fail 2>&1) || {
            echo "Failed to fetch exporter-vars.yml for $branch (exit $?)"
            continue
          }

        SERVER_URL=$(echo "$RAW_CONTENT" \
          | grep -E '^[[:space:]]*SERVER_URL:' \
          | sed -E 's/[[:space:]]*#.*//' \
          | sed -E 's/.*SERVER_URL:[[:space:]]*"?([^"]*)"?/\1/' \
          | xargs)

        if [ -z "$SERVER_URL" ]; then
          echo "No SERVER_URL found in $branch, skipping"
          continue
        fi

        echo "Restarting exporter for $branch at $SERVER_URL"
        curl -X POST "$SERVER_URL/shutdown_emulate?exit_code=1" \
          --header "TRIGGER-TOKEN: $TRIGGER_TOKEN" --header "PROJECT-TOKEN: $PROJECT_TOKEN" \
          --fail || echo "Failed to restart $branch"
      done

propagate_pipeline:
  stage: propagate
  rules:
    - if: $CI_PIPELINE_SOURCE == "push" && $CI_COMMIT_BRANCH == "main"
      changes:
        - main-pipeline.yml
      when: always
    - when: never
  script:
    - |
      curl --silent --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
        "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/files/main-pipeline.yml/raw?ref=$CI_COMMIT_SHA" \
        > main-pipeline.yml

      if [ ! -s main-pipeline.yml ]; then
        echo "Failed to fetch main-pipeline.yml"
        exit 1
      fi

      BRANCHES=$(curl --silent --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
        "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/branches?per_page=100" \
        | grep -o '"name":"[^"]*"' | sed 's/"name":"//;s/"//' | grep -v '^main$')

      for branch in $BRANCHES; do
        echo "Updating main-pipeline.yml in branch $branch"

        BRANCH_INFO=$(curl --silent --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
          "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/branches/$branch")
        BRANCH_SHA=$(echo "$BRANCH_INFO" | grep -o '"commit":{[^}]*"id":"[^"]*"' | sed 's/.*"id":"\([^"]*\)".*/\1/')

        FILE_INFO=$(curl --silent --write-out "%{http_code}" --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
          "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/files/main-pipeline.yml?ref=$branch" \
          -o /dev/null 2>&1)
        if [ "$FILE_INFO" = "200" ]; then
          FILE_SHA=$(curl --silent --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
            "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/files/main-pipeline.yml?ref=$branch" \
            | grep -o '"last_commit_id":"[^"]*"' | sed 's/"last_commit_id":"//;s/"//')
          curl --request POST --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
            --form "branch=$branch" \
            --form "commit_message=Update main-pipeline.yml from main" \
            --form "actions[][action]=update" \
            --form "actions[][file_path]=main-pipeline.yml" \
            --form "actions[][content]=<main-pipeline.yml" \
            --form "actions[][last_commit_id]=$FILE_SHA" \
            "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/commits" \
            --fail
        else
          curl --request POST --header "PRIVATE-TOKEN: $PROJECT_TOKEN" \
            --form "branch=$branch" \
            --form "commit_message=Add main-pipeline.yml from main" \
            --form "actions[][action]=create" \
            --form "actions[][file_path]=main-pipeline.yml" \
            --form "actions[][content]=<main-pipeline.yml" \
            "$CI_SERVER_URL/api/v4/projects/$CI_PROJECT_ID/repository/commits" \
            --fail
        fi
      done
```

### 6. Выпуск релиза (ручная сборка)

Для создания нового релиза (тега) используется набор bat‑скриптов, которые выполняются на рабочей станции разработчика. Они клонируют репозиторий экспортера, собирают бинарники, загружают их в Package Registry GitLab и создают релиз (публикуют тег). Все скрипты должны находиться в одной папке. Ниже приведены их полные листинги.

Для выпуска релиза запустите `_all.bat`.

#### `_all.bat`

```bat
@echo off
setlocal

rem Если передан параметр --skip-build, устанавливаем SKIP_BUILD=true
if "%1"=="--skip-build" set SKIP_BUILD=true

call 1_config.bat
if errorlevel 1 exit /b 1

call 2_clone_exporter.bat || exit /b 1
call 3_clone_config.bat || exit /b 1
call 4_build.bat || exit /b 1
call 5_upload.bat || exit /b 1
call 6_create_release.bat || exit /b 1
echo All done.
```

#### `1_config.bat`

```bat
@echo off

rem ===== Настройки версии =====
set VERSION=v1.5.3

rem ===== Настройки репозитория экспортера =====
set EXPORTER_REPO_URL=https://gitlab.my-company.ru/devops/prometheus_1C_exporter.git
set EXPORTER_BRANCH=develop

rem ===== Настройки репозитория конфигураций =====
set CONFIG_REPO_URL=https://gitlab.my-company.ru/devops/1c_exporter_config.git
set CONFIG_BRANCH=main

rem ===== Настройки GitLab =====
set GITLAB_URL=gitlab.my-company.ru
set PROJECT_ID=11
rem Токен и информация о нем – задаются в 1_config_private.bat
rem set GITLAB_TOKEN_KEY=
rem set GITLAB_TOKEN_INFO=

rem ===== Пути для клонирования (относительно текущей папки) =====
set EXPORTER_LOCAL_DIR=exporter_src
set CONFIG_LOCAL_DIR=config_src

rem ===== Подключение приватных настроек (токен и переопределения) =====
if exist 1_config_private.bat call 1_config_private.bat

rem Вывод информации о токене, если задана
if defined GITLAB_TOKEN_INFO echo Token info: %GITLAB_TOKEN_INFO%
```

#### `1_config_private.bat` (приватный, не коммитить)

```bat
@echo off
rem ===== Приватные настройки, дополняющие и/или переопределяющие 1_config.bat =====
set GITLAB_TOKEN_KEY=glpat-******** 
set GITLAB_TOKEN_INFO=Project-access token, для проекта конфигураций. Истекает 2026-12-31. Требуется роль Developer, с правами api, write_repository.

rem При необходимости можно переопределить и другие параметры, например:
rem set PROJECT_ID=987654
rem set GITLAB_URL=https://gitlab.company.com
```

#### `2_clone_exporter.bat`

```bat
@echo off
setlocal
call 1_config.bat
if "%EXPORTER_REPO_URL%"=="" echo EXPORTER_REPO_URL not set & exit /b 1
if "%EXPORTER_BRANCH%"=="" echo EXPORTER_BRANCH not set & exit /b 1
if "%EXPORTER_LOCAL_DIR%"=="" echo EXPORTER_LOCAL_DIR not set & exit /b 1

if not exist "%EXPORTER_LOCAL_DIR%" (
    echo Cloning exporter repository...
    git clone --branch %EXPORTER_BRANCH% "%EXPORTER_REPO_URL%" "%EXPORTER_LOCAL_DIR%"
    if errorlevel 1 exit /b 1
) else (
    echo Updating exporter repository...
    cd "%EXPORTER_LOCAL_DIR%"
    git fetch
    git checkout %EXPORTER_BRANCH%
    if errorlevel 1 (
        echo Failed to checkout branch %EXPORTER_BRANCH%
        exit /b 1
    )
    git pull
    cd ..
)
echo Current branch in %EXPORTER_LOCAL_DIR%:
cd "%EXPORTER_LOCAL_DIR%"
git branch --show-current
cd ..
echo Done.
```

#### `3_clone_config.bat`

```bat
@echo off
setlocal
call 1_config.bat
if "%CONFIG_REPO_URL%"=="" echo CONFIG_REPO_URL not set & exit /b 1
if "%CONFIG_BRANCH%"=="" echo CONFIG_BRANCH not set & exit /b 1
if "%CONFIG_LOCAL_DIR%"=="" echo CONFIG_LOCAL_DIR not set & exit /b 1

if not exist "%CONFIG_LOCAL_DIR%" (
    echo Cloning config repository...
    git clone --branch %CONFIG_BRANCH% "%CONFIG_REPO_URL%" "%CONFIG_LOCAL_DIR%"
) else (
    echo Updating config repository...
    cd "%CONFIG_LOCAL_DIR%"
    git fetch
    git checkout %CONFIG_BRANCH%
    git pull
    cd ..
)
echo Done.
```

#### `4_build.bat`

```bat
@echo off
setlocal
call 1_config.bat
if "%EXPORTER_LOCAL_DIR%"=="" echo EXPORTER_LOCAL_DIR not set & exit /b 1
if "%VERSION%"=="" echo VERSION not set & exit /b 1

set BUILD_DIR=executables
if not exist "%BUILD_DIR%" mkdir "%BUILD_DIR%"

cd "%EXPORTER_LOCAL_DIR%"

for /f "delims=" %%i in ('git rev-parse --short HEAD') do set SHORT_COMMIT=%%i

echo Building for Linux...
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -ldflags="-s -w -X main.version=%VERSION% -X main.gitCommit=%SHORT_COMMIT%" -trimpath -o ..\%BUILD_DIR%\1C_exporter_linux_amd64 .

echo Building for Windows...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -ldflags="-s -w -X main.version=%VERSION% -X main.gitCommit=%SHORT_COMMIT%" -trimpath -o ..\%BUILD_DIR%\1C_exporter_windows_amd64.exe .

rem Переходим в папку с бинарниками (без лишнего пробела)
cd ..\%BUILD_DIR%

echo Generating checksums...
if exist checksums.txt del checksums.txt
(for %%f in (1C_exporter_*) do (
    certutil -hashfile "%%f" SHA256 | findstr /v "certutil" >> checksums.tmp
))
ren checksums.tmp checksums.txt

echo Build completed.
```

#### `5_upload.bat`

```bat
@echo off
setlocal
call 1_config.bat
if "%VERSION%"=="" echo VERSION not set & exit /b 1
if "%GITLAB_TOKEN_KEY%"=="" echo GITLAB_TOKEN_KEY not set & exit /b 1
if "%PROJECT_ID%"=="" echo PROJECT_ID not set & exit /b 1
if "%GITLAB_URL%"=="" echo GITLAB_URL not set & exit /b 1

set SCRIPT_DIR=%~dp0
set BUILD_DIR=%SCRIPT_DIR%executables
set LOCAL_DIR=%SCRIPT_DIR%%EXPORTER_LOCAL_DIR%

rem Проверяем наличие бинарников сначала в BUILD_DIR, затем в LOCAL_DIR
set SOURCE_DIR=
if exist "%BUILD_DIR%\1C_exporter_linux_amd64" (
    if exist "%BUILD_DIR%\1C_exporter_windows_amd64.exe" (
        set SOURCE_DIR=%BUILD_DIR%
        echo Using binaries from %BUILD_DIR%
    )
)
if "%SOURCE_DIR%"=="" (
    if exist "%LOCAL_DIR%\1C_exporter_linux_amd64" (
        if exist "%LOCAL_DIR%\1C_exporter_windows_amd64.exe" (
            set SOURCE_DIR=%LOCAL_DIR%
            echo Using binaries from %LOCAL_DIR%
        )
    )
)
if "%SOURCE_DIR%"=="" (
    echo No binaries found in %BUILD_DIR% or %LOCAL_DIR%
    exit /b 1
)

cd "%SOURCE_DIR%"

rem Если checksums.txt отсутствует, генерируем его на основе бинарников
if not exist checksums.txt (
    echo Checksums.txt not found, generating...
    if exist checksums.tmp del checksums.tmp
    for %%f in (1C_exporter_*) do (
        certutil -hashfile "%%f" SHA256 | findstr /v "certutil" >> checksums.tmp
    )
    ren checksums.tmp checksums.txt
)

echo Uploading Linux binary...
curl --fail --header "PRIVATE-TOKEN: %GITLAB_TOKEN_KEY%" --upload-file 1C_exporter_linux_amd64 "%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/packages/generic/1C_exporter/%VERSION%/1C_exporter_linux_amd64"
if errorlevel 1 exit /b 1

echo Uploading Windows binary...
curl --fail --header "PRIVATE-TOKEN: %GITLAB_TOKEN_KEY%" --upload-file 1C_exporter_windows_amd64.exe "%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/packages/generic/1C_exporter/%VERSION%/1C_exporter_windows_amd64.exe"
if errorlevel 1 exit /b 1

echo Uploading checksums...
curl --fail --header "PRIVATE-TOKEN: %GITLAB_TOKEN_KEY%" --upload-file checksums.txt "%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/packages/generic/1C_exporter/%VERSION%/checksums.txt"
if errorlevel 1 exit /b 1

echo Upload completed.
```

#### `6_create_release.bat`

```bat
@echo off
setlocal
call 1_config.bat
if "%VERSION%"=="" echo VERSION not set & exit /b 1
if "%GITLAB_TOKEN_KEY%"=="" echo GITLAB_TOKEN_KEY not set & exit /b 1
if "%PROJECT_ID%"=="" echo PROJECT_ID not set & exit /b 1
if "%GITLAB_URL%"=="" echo GITLAB_URL not set & exit /b 1
if "%CONFIG_BRANCH%"=="" echo CONFIG_BRANCH not set & exit /b 1
if "%CONFIG_LOCAL_DIR%"=="" echo CONFIG_LOCAL_DIR not set & exit /b 1

cd "%CONFIG_LOCAL_DIR%"

rem Получаем commit ID текущей ветки
for /f "delims=" %%i in ('git rev-parse HEAD') do set COMMIT_ID=%%i
echo Using commit ID: %COMMIT_ID%

rem Проверяем, существует ли уже тег
curl --silent --header "PRIVATE-TOKEN: %GITLAB_TOKEN_KEY%" "%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/repository/tags/%VERSION%" > tag.json
findstr /c:"\"name\":\"%VERSION%\"" tag.json >nul
if errorlevel 1 (
    echo Creating tag %VERSION%...
    curl --request POST --header "PRIVATE-TOKEN: %GITLAB_TOKEN_KEY%" "%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/repository/tags" --form "tag_name=%VERSION%" --form "ref=%COMMIT_ID%" --fail
    if errorlevel 1 (
        echo Tag creation failed
        del tag.json
        exit /b 1
    )
) else (
    echo Tag %VERSION% already exists.
)
del tag.json

echo Creating release...
curl --request POST --header "PRIVATE-TOKEN: %GITLAB_TOKEN_KEY%" "%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/releases" ^
    --form "tag_name=%VERSION%" ^
    --form "description=Release %VERSION%" ^
    --form "assets[links][][name]=Linux AMD64" ^
    --form "assets[links][][url]=%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/packages/generic/1C_exporter/%VERSION%/1C_exporter_linux_amd64" ^
    --form "assets[links][][link_type]=package" ^
    --form "assets[links][][name]=Windows AMD64" ^
    --form "assets[links][][url]=%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/packages/generic/1C_exporter/%VERSION%/1C_exporter_windows_amd64.exe" ^
    --form "assets[links][][link_type]=package" ^
    --form "assets[links][][name]=Checksums" ^
    --form "assets[links][][url]=%GITLAB_URL%/api/v4/projects/%PROJECT_ID%/packages/generic/1C_exporter/%VERSION%/checksums.txt" ^
    --form "assets[links][][link_type]=other" || exit /b 1

echo Release created.
```

### 7. Обновление токенов

Поддержка автоматической ротации токенов достигается тем, что при каждом вызове любого HTTP‑метода экспортера (через пайплайн) токены передаются в заголовках `TRIGGER-TOKEN` и `PROJECT-TOKEN`. Экспортер сохраняет их в файл `tokens.yml`, поэтому для смены токена достаточно обновить переменные CI/CD в проекте GitLab – при следующем взаимодействии токены на всех экспортерах будут обновлены автоматически.

## ⚠️ Локализация ошибок

При возникновении проблем проверьте:

- Доступность RAC-утилиты
- Права на чтение конфигурационного файла
- Открытые порты в firewall
- Логи приложения (режим отладки через `LogLevel: 5`)
- Наличие файла `tokens.yml` и правильность токенов (если используется GitLab)
- Права на запись в папку с бинарником (для автообновления)
- Доступность GitLab API (проверьте через `curl` с теми же токенами)