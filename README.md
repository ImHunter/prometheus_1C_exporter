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

#### Через curl (для всех методов)

- **Получение публичного ключа**
  ```bash
  curl http://localhost:9091/public-key > exporter.pub
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

- **Приостановка сбора метрик** (аналогично браузерному варианту)
  ```bash
  curl "http://localhost:9091/Pause?metricNames=processes,connections&offsetMin=5"
  ```

- **Возобновление сбора метрик**
  ```bash
  curl "http://localhost:9091/Continue?metricNames=disk_metrics"
  ```

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

Для централизованного управления конфигурацией и секретами экспортер может взаимодействовать с GitLab CI/CD. Это позволяет автоматически обновлять настройки и безопасно доставлять учётные данные для баз 1С.

### 1. Общие настройки

Перед настройкой автоматизации выполните следующие шаги:

1. **Создайте отдельный GitLab-проект**, в котором будут храниться конфигурационные файлы для каждого экземпляра экспортера.
2. **Для каждого экземпляра создайте отдельную ветку** (например, `web`, `rls`, `trade`). В каждой ветке будут находиться:
   * `settings.yml` – основной конфигурационный файл.
   * (опционально) `secrets.json.enc` – зашифрованные секреты.
3. **В настройках CI/CD каждой ветки задайте переменные**:
   * `SERVER_URL` – базовый URL экспортера (например, `http://server-web:9091`). **Важно:** URL должен быть без пути, так как методы добавляются автоматически.
   * `FILE_TO_UPLOAD` – имя файла конфигурации (обычно `settings.yml`).
   * (опционально) `SECRETS_FILE` – имя файла с зашифрованными секретами (по умолчанию `secrets.json.enc`).
4. **Настройте CI/CD-пайплайн**, используя шаблон, описанный ниже. Рекомендуется хранить общий код пайплайна в ветке `main` и подключать его через `include` (чтобы избежать дублирования).

**Пример шаблона пайплайна (в ветке `main`) – файл `ci-templates.yml`**

```yaml
default:
  image: alpine/curl:8.17.0

stages:
  - upload

.upload_file_template:
  stage: upload
  rules:
    - if: $CI_COMMIT_BRANCH != "main"
      changes:
        - settings.yml
      when: always
    - when: never
  script:
    - echo "Uploading $FILE_TO_UPLOAD to $SERVER_URL"
    - response=$(curl -s -w "\n%{http_code}" -X POST -F "file=@$FILE_TO_UPLOAD" "$SERVER_URL/set_config")
    - http_code=$(echo "$response" | tail -n1)
    - response_body=$(echo "$response" | sed '$d')
    - echo "Response HTTP Code: $http_code"
    - echo "Response Body: $response_body"
    - if [[ $http_code -ge 200 && $http_code -lt 300 ]]; then echo "Success"; else exit 1; fi

.send_secrets_template:
  stage: upload
  rules:
    - if: $CI_PIPELINE_SOURCE == "trigger"
    - changes:
        - secrets.json.enc
      when: always
  script:
    - SECRETS_FILE=${SECRETS_FILE:-secrets.json.enc}
    - if [ ! -f "$SECRETS_FILE" ]; then echo "Secrets file not found"; exit 1; fi
    - if [ -z "$SERVER_URL" ]; then echo "SERVER_URL not set"; exit 1; fi
    - ENCRYPTED_BASE64=$(base64 -w0 "$SECRETS_FILE")
    - curl -X POST "$SERVER_URL/set_secrets" -H "Content-Type: application/json" -d "{\"encrypted_data\": \"$ENCRYPTED_BASE64\"}" --fail
    - echo "Secrets delivered"
```

**В каждой ветке экспортера** файл `.gitlab-ci.yml` будет минимальным:

```yaml
include:
  - project: 'your-group/your-config-project'
    file: 'ci-templates.yml'
    ref: main

variables:
  SERVER_URL: "http://server-web:9091"
  FILE_TO_UPLOAD: "settings.yml"
  # SECRETS_FILE: "secrets.json.enc"  # если имя отличается

upload_file:
  extends: .upload_file_template

send_secrets:
  extends: .send_secrets_template
```

### 2. Обновление конфигурационного файла

При каждом изменении `settings.yml` в любой ветке (кроме `main`) автоматически запускается пайплайн, который отправляет новый конфиг на соответствующий экспортер через метод `/set_config`. Экспортер применяет новые настройки без перезапуска.

Для корректной работы убедитесь, что:
* В ветке определена переменная `SERVER_URL`.
* Пайплайн настроен согласно шаблону выше.

### 3. Безопасное хранение и доставка секретов

Для работы с учётными данными баз 1С (логины/пароли) используется следующий механизм:

* **Генерация ключей** – при первом запуске экспортер создаёт пару RSA-ключей (3072 бит) и сохраняет их в подкаталоге `keys/<host>_<port>/` рядом с исполняемым файлом. Приватный ключ защищён правами доступа (0600).
* **Шифрование секретов** – администратор локально подготавливает JSON-файл с секретами следующего формата:
  ```json
  {
    "ras": { "login": "admin", "password": "ras_pass" },
    "ibase_default": { "login": "default_user", "password": "default_pass" },
    "ibases": {
      "main_db": { "login": "main_user", "password": "main_pass" }
    }
  }
  ```
  Для шифрования используется публичный ключ экспортера (полученный через `/public-key`). Можно воспользоваться утилитой `openssl` или встроенной командой экспортера (планируется в будущем).
* **Размещение в GitLab** – зашифрованный файл (например, `secrets.json.enc`) помещается в соответствующую ветку репозитория.
* **Автоматическая доставка** – при изменении файла секретов или по триггеру от экспортера запускается job `send_secrets`, который отправляет зашифрованные данные на эндпоинт `/set_secrets`. Экспортер расшифровывает их и сохраняет в памяти, а также дублирует на диск (в ту же папку, где лежат ключи) для использования при холодном старте.
* **Использование секретов** – методы `GetLogPass` и `RAC_Login/Pass` автоматически подставляют полученные учётные данные; если секреты не заданы, используется старый механизм (plain).

**Важно:** При первом запуске после настройки GitLab-режима экспортер не имеет секретов. Администратор должен вручную (или через CI) инициировать первый запуск пайплайна, либо дождаться автоматического триггера после коммита файла секретов.

## 📌 Примечания

*   Для работы с секретами требуется наличие публичного ключа экспортера. Получить его можно через эндпоинт `/public-key` (см. пример выше).
*   В режиме GitLab экспортер не требует периодического обновления секретов – они доставляются при каждом изменении файла в репозитории.
*   При перезагрузке экспортер загружает последние сохранённые секреты с диска, поэтому после перезапуска он сразу готов к работе, даже если GitLab временно недоступен.