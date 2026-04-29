# Search Engine

[Русский](#русский) | [English](#english)

## Русский

### О проекте

`search_engine` — учебный поисковый движок с веб-интерфейсом, индексированием локальных документов и краулингом веб-страниц.  
Практическая часть проекта состоит из Go backend, поискового индекса на Bleve и простого frontend на TypeScript + Vite.

Основные возможности:

- полнотекстовый поиск по локально загруженным HTML/PDF документам;
- поиск по страницам, собранным crawler-ом;
- генерация сниппетов и пагинация результатов;
- безопасная загрузка файлов и ограниченный краулинг;
- светлая и тёмная тема интерфейса.

### Архитектура

```text
web/                 frontend (TypeScript + Vite)
cmd/server/          HTTP server и API
internal/engine/     индексация и поиск через Bleve
internal/crawler/    web crawler и URL security checks
internal/models/     общие модели ответа и документов
search.bleve/        локальный индекс Bleve
uploads/             загруженные пользователем файлы
```

Поток данных:

1. Пользователь загружает HTML/PDF или запускает краулинг.
2. Backend извлекает текст и передаёт документ в `internal/engine`.
3. Bleve индексирует документ в `search.bleve`.
4. `/search` возвращает результаты, score, snippet и метаданные.
5. Frontend безопасно отображает результаты без небезопасного HTML-рендера.

### Технологии

- Go 1.26+
- Bleve
- Colly
- goquery
- TypeScript
- Vite

### Требования

Для локального запуска нужны:

- Go `1.26+`
- Node.js `18+`
- `npm`
- `pdftotext` для извлечения текста из PDF

Пример установки `pdftotext`:

- Arch: `sudo pacman -S poppler`
- Ubuntu/Debian: `sudo apt install poppler-utils`

### Локальный запуск

#### 1. Установить зависимости frontend

```bash
cd web
npm install
cd ..
```

#### 2. Собрать проект

```bash
make build
```

#### 3. Запустить backend

```bash
make run
```

По умолчанию сервер слушает:

```text
http://127.0.0.1:8080
```

### Режим разработки

Backend:

```bash
go run ./cmd/server/main.go
```

Frontend:

```bash
cd web
npm run dev
```

В dev-режиме frontend обращается к backend на `http://127.0.0.1:8080`.

### Переменные окружения

Сервер поддерживает:

- `SEARCH_ENGINE_LISTEN_ADDR` — адрес, который слушает backend. По умолчанию `127.0.0.1:8080`.
- `SEARCH_ENGINE_ALLOWED_ORIGINS` — список origin через запятую для CORS.
- `SEARCH_ENGINE_ADMIN_TOKEN` — токен для доступа к `/upload`, `/crawl`, `/crawl/status` извне loopback.

Пример:

```bash
export SEARCH_ENGINE_LISTEN_ADDR=127.0.0.1:8080
export SEARCH_ENGINE_ALLOWED_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
export SEARCH_ENGINE_ADMIN_TOKEN=change-me
```

### API

Основные endpoint-ы:

- `GET /search?q=<query>&page=1&size=10`
- `GET /stats`
- `POST /upload`
- `POST /crawl`
- `GET /crawl/status`

Примечания:

- `/upload` принимает только `.pdf`, `.html`, `.htm`;
- `/crawl` и `/upload` доступны с loopback по умолчанию, либо по `X-Admin-Token` / `Authorization: Bearer <token>`;
- crawler блокирует небезопасные адреса и private/loopback сети;
- `/search` не возвращает полный текст документа, только метаданные и snippet.

### Безопасность

В проекте уже включены базовые меры защиты:

- localhost-only bind по умолчанию;
- ограниченный CORS;
- защита upload по типам файлов и размеру;
- безопасный рендер search results на frontend;
- запрет SSRF для crawler;
- security headers и CSP;
- раздача опасных файлов только как attachment.

### Полезные команды

```bash
make fmt
make test
make lint
make clean
```

### Ограничения

- индекс хранится локально и не рассчитан на production-нагрузку;
- нет полноценной аутентификации пользователей;
- crawler ограничен базовыми правилами и не является распределённым;
- PDF extraction зависит от установленного `pdftotext`.

---

## English

### About

`search_engine` is a small educational search engine with a web UI, local document indexing, and basic web crawling.  
The practical part of the project consists of a Go backend, a Bleve search index, and a lightweight TypeScript + Vite frontend.

Key features:

- full-text search across uploaded HTML/PDF documents;
- search over crawled web pages;
- snippets and paginated results;
- safer file uploads and restricted crawling;
- light and dark UI themes.

### Architecture

```text
web/                 frontend (TypeScript + Vite)
cmd/server/          HTTP server and API
internal/engine/     indexing and search with Bleve
internal/crawler/    web crawler and URL security checks
internal/models/     shared document/response models
search.bleve/        local Bleve index
uploads/             uploaded user files
```

Data flow:

1. A user uploads HTML/PDF files or starts a crawl job.
2. The backend extracts text and passes documents to `internal/engine`.
3. Bleve indexes them in `search.bleve`.
4. `/search` returns matches, scores, snippets, and metadata.
5. The frontend renders results safely without unsafe HTML injection.

### Stack

- Go 1.26+
- Bleve
- Colly
- goquery
- TypeScript
- Vite

### Requirements

Local development requires:

- Go `1.26+`
- Node.js `18+`
- `npm`
- `pdftotext` for PDF text extraction

Example `pdftotext` installation:

- Arch: `sudo pacman -S poppler`
- Ubuntu/Debian: `sudo apt install poppler-utils`

### Local Run

#### 1. Install frontend dependencies

```bash
cd web
npm install
cd ..
```

#### 2. Build the project

```bash
make build
```

#### 3. Run the server

```bash
make run
```

Default address:

```text
http://127.0.0.1:8080
```

### Development Mode

Backend:

```bash
go run ./cmd/server/main.go
```

Frontend:

```bash
cd web
npm run dev
```

In development, the frontend uses `http://127.0.0.1:8080` as the API base.

### Environment Variables

Supported server variables:

- `SEARCH_ENGINE_LISTEN_ADDR` — backend listen address. Default: `127.0.0.1:8080`
- `SEARCH_ENGINE_ALLOWED_ORIGINS` — comma-separated CORS allowlist
- `SEARCH_ENGINE_ADMIN_TOKEN` — token required for `/upload`, `/crawl`, and `/crawl/status` when requests are not coming from loopback

Example:

```bash
export SEARCH_ENGINE_LISTEN_ADDR=127.0.0.1:8080
export SEARCH_ENGINE_ALLOWED_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
export SEARCH_ENGINE_ADMIN_TOKEN=change-me
```

### API

Main endpoints:

- `GET /search?q=<query>&page=1&size=10`
- `GET /stats`
- `POST /upload`
- `POST /crawl`
- `GET /crawl/status`

Notes:

- `/upload` only accepts `.pdf`, `.html`, and `.htm`
- `/crawl` and `/upload` are available from loopback by default, or via `X-Admin-Token` / `Authorization: Bearer <token>`
- the crawler blocks unsafe targets and private/loopback networks
- `/search` returns metadata and snippets, not the full indexed document body

### Security

The project already includes several baseline protections:

- localhost-only bind by default;
- restricted CORS;
- upload type/size validation;
- safe frontend rendering for search results;
- SSRF protection in the crawler;
- security headers and CSP;
- dangerous uploaded files served as attachments.

### Useful Commands

```bash
make fmt
make test
make lint
make clean
```

### Limitations

- the index is local and not designed for production scale;
- there is no full user authentication system;
- the crawler is intentionally simple and not distributed;
- PDF extraction depends on a working `pdftotext` installation.
