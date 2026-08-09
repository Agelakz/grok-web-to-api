# Grok Web To API

OpenAI-compatible local proxy for the Grok web session at `https://grok.com`.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go)](https://go.dev/)
[![Port](https://img.shields.io/badge/default%20port-4982-informational)](#configuration)

## Disclaimer

This project is for research and educational use. It is not affiliated with xAI or X. It depends on reverse-engineered browser session cookies and UI protocols that may violate third-party terms of service. You assume all risk of use.

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage Examples](#usage-examples)
- [API Reference](#api-reference)
- [Architecture](#architecture)
- [Project Structure](#project-structure)
- [Limitations](#limitations)
- [Contributing](#contributing)
- [License](#license)

## Features

Local HTTP surface that speaks OpenAI-shaped REST while driving a live Grok browser session (REST bootstrap + WebSocket gateway). Default listen port: `4982` (sibling Gemini proxy commonly uses `:4981`).

| Capability | Endpoint | Status |
|---|---|---|
| Health / session ready | `GET /health` | ✅ |
| List modes / models | `GET /openai/v1/models` (+ `/v1/models`) | ✅ |
| Chat (text) | `POST /openai/v1/chat/completions` | ✅ (non-stream) |
| Vision (image read) | chat + multimodal `image_url` data URI | ✅ |
| Image generation | `POST /openai/v1/images/generations` | ✅ |
| SSE streaming | `stream=true` | 🚧 **501** not implemented |
| Video | — | 🚧 not implemented |
| Remote `http(s)` image fetch in chat | — | intentionally unsupported (data URI only) |

Each chat or image-generation call opens a **new temporary conversation** (`is_temporary: true`). There is no shared multi-turn history across requests.

## Quick Start

```bash
cp .env.example .env
# set cookies — path (A) file, (B) per-cookie env, or (C) GROK_COOKIES
# see Configuration → Authentication

go run ./cmd/server
# or: go build -o server ./cmd/server && ./server
```

Smoke checks:

```bash
curl -s localhost:4982/health | jq
curl -s localhost:4982/openai/v1/models | jq

# text
curl -s localhost:4982/openai/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","messages":[{"role":"user","content":"hi"}]}' | jq

# vision (data URI only; min ~64x64)
B64=$(base64 -w0 /path/to.png)
curl -s localhost:4982/openai/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"fast\",\"messages\":[{\"role\":\"user\",\"content\":[
    {\"type\":\"text\",\"text\":\"what is in this image?\"},
    {\"type\":\"image_url\",\"image_url\":{\"url\":\"data:image/png;base64,$B64\"}}
  ]}]}" | jq

# image generate
curl -s localhost:4982/openai/v1/images/generations \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","prompt":"a simple blue circle on white","n":1,"response_format":"url"}' | jq
# response_format: url | b64_json ; n 1..4 ; size is ignored
```

Python smoke client: `python3 examples/openai_client.py`  
(`GROK_API_BASE` defaults to `http://127.0.0.1:4982`)

## Installation

### Local

Requirements: Go `1.22+`.

```bash
git clone https://github.com/Agelakz/grok-web-to-api.git
cd grok-web-to-api
cp .env.example .env
# configure authentication (see Configuration)

go build -o server ./cmd/server
./server
```

One-shot without a `.env` file (per-cookie env path B):

```bash
export GROK_SSO='...' GROK_SSO_RW='...' GROK_USER_ID='...' GROK_CF_CLEARANCE='...'
./server
```

Confirm readiness:

```bash
curl -s localhost:4982/health | jq '{grok_ready,has_cookies,user_id,reverse}'
# expect: grok_ready=true
```

### Docker

```bash
docker build -t grok-web-to-api .

# path B — individual cookie env vars
docker run --rm -p 4982:4982 \
  -e GROK_SSO -e GROK_SSO_RW -e GROK_USER_ID -e GROK_CF_CLEARANCE \
  grok-web-to-api

# path A — Netscape cookie file
docker run --rm -p 4982:4982 --env-file .env \
  -v /path/to/grok.com_cookies.txt:/cookies.txt:ro \
  -e GROK_COOKIE_FILE=/cookies.txt \
  grok-web-to-api
```

`Dockerfile` multi-stage: Go `1.22` alpine build → static binary, non-root runtime user, `EXPOSE 4982`, `ENV PORT=4982 APP_ENV=production`.

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `4982` | Listen port |
| `LOG_LEVEL` | `info` | zap log level |
| `APP_ENV` | — | Documentation / Docker sets `production` |
| `GROK_BASE_URL` | `https://grok.com` | Origin + REST/WS host |
| `GROK_COOKIE_FILE` | — | Path to Netscape cookies file |
| `GROK_COOKIES` | — | Raw `Cookie` header (overrides per-cookie compose) |
| `GROK_SSO` | — | Cookie `sso` (**required** for path B) |
| `GROK_SSO_RW` | — | Cookie `sso-rw` |
| `GROK_USER_ID` | — | Cookie `x-userid` |
| `GROK_CF_CLEARANCE` | — | Cookie `cf_clearance` |
| `GROK_CF_BM` | — | Cookie `__cf_bm` (optional) |
| `GROK_DEVICE_ID` | — | Cookie `grok_device_id` (optional) |
| `GROK_REFRESH_INTERVAL` | `30` | Reserved / retry spacing |
| `GROK_MAX_RETRIES` | `3` | Reserved retries |
| `RATE_LIMIT_ENABLED` | `false` | In-process rate limiter on/off |
| `RATE_LIMIT_WINDOW_MS` | `60000` | Rate-limit window |
| `RATE_LIMIT_MAX_REQUESTS` | `30` | Max requests per window |

`GROK_AUTH_TOKEN` / `GROK_CT0` are legacy X placeholders and are **not** used on the `grok.com` path.

### Authentication

Sign in at `https://grok.com`, then choose **one** path in `.env` or the process environment.

#### Path B — Per-Cookie Environment Variables (script-friendly)

```bash
# .env — or export in shell / docker -e
GROK_SSO=eyJ...                 # required
GROK_SSO_RW=eyJ...              # practically required
GROK_USER_ID=72d0b43d-....      # x-userid
GROK_CF_CLEARANCE=....          # recommended
# optional:
# GROK_CF_BM=....
# GROK_DEVICE_ID=....
```

```bash
export GROK_SSO='...'
export GROK_SSO_RW='...'
export GROK_USER_ID='...'
export GROK_CF_CLEARANCE='...'
./server
```

Config composes automatically: `sso=...; sso-rw=...; x-userid=...; cf_clearance=...`  
(`composeCookiesFromEnv` in `internal/config/config.go`)

#### Path C — Single Cookie Header String

```bash
GROK_COOKIES='sso=...; sso-rw=...; x-userid=...; cf_clearance=...'
```

If `GROK_COOKIES` is set, it **wins** over per-cookie compose.

#### Path A — Netscape Cookie File

```bash
GROK_COOKIE_FILE=/path/to/grok.com_cookies.txt
```

Full browser export is fine (e.g. extension **Get cookies.txt LOCALLY**). Runtime filters to essential names only.

#### Env → Cookie Map

| Environment variable | Cookie name | Required? | Role |
|---|---|---|---|
| `GROK_SSO` | `sso` | **yes** | Login session; `HasAuth` gate |
| `GROK_SSO_RW` | `sso-rw` | practically yes | SSO twin |
| `GROK_USER_ID` | `x-userid` | practically yes | WS `uid`; falls back to session probe |
| `GROK_CF_CLEARANCE` | `cf_clearance` | recommended | Cloudflare clearance |
| `GROK_CF_BM` | `__cf_bm` | optional | CF bot management (short TTL) |
| `GROK_DEVICE_ID` | `grok_device_id` | optional | Device fingerprint |
| `GROK_COOKIES` | (all names in string) | alternative | Raw header; filtered to essential |
| `GROK_COOKIE_FILE` | (all names in file) | alternative | Netscape; `grok.com` domain only |

**Minimum** for chat / vision / image-gen: `sso` + `sso-rw` + `x-userid` + `cf_clearance`.

#### Essential Cookie Whitelist

Outbound `Cookie` headers only include:

`sso`, `sso-rw`, `x-userid`, `cf_clearance`, `__cf_bm`, `grok_device_id`

Dropped automatically (analytics / ads / consent noise):

`Optanon*`, `mp_*_mixpanel`, `__stripe_*`, `_gcl_au`, `_twpid`, `__cuid`, `i18nextLng`, …

Example: a 16-line export typically becomes ~6 cookies on the wire.

#### Cookie Load Flow

```text
GROK_COOKIE_FILE (Netscape) ──► parse domain grok.com
                                      │
GROK_COOKIES  ──────────────────────► Pairs map ──► Header() = essential only
                                      │
GROK_SSO / GROK_SSO_RW / ... ─────────┘
   (composeCookiesFromEnv when GROK_COOKIES is empty)

legacy GROK_AUTH_TOKEN / GROK_CT0 ignored on grok.com path
```

- File loads first; raw / `GROK_COOKIES` / compose override into `Pairs`.
- `Header()` always applies the essential whitelist.
- Soft boot without cookies: HTTP surface stays up with `grok_ready=false`.
- Runtime cache: `.cookies/<sha>.json` (gitignored).
- Expired session → upstream `401`/`403`; re-export or re-set env and restart.

#### Cookie Tips

- Export from a tab already logged into `grok.com` (not `x.com`).
- Without `sso` → `HasAuth=false`.
- `cf_clearance` / `__cf_bm` expire quickly under Cloudflare.
- Never commit `.env`, `*cookies*.txt`, or `.cookies/`.

## Usage Examples

Base URL: `http://127.0.0.1:4982`  
Path aliases: `/openai/v1/*` and `/v1/*` are equivalent.

### cURL

```bash
BASE=http://127.0.0.1:4982

# models
curl -s "$BASE/openai/v1/models" | jq

# chat text
curl -s "$BASE/openai/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","messages":[{"role":"user","content":"Reply with exactly: PONG"}]}' \
  | jq -r '.choices[0].message.content'

# image generate → URL
curl -s "$BASE/openai/v1/images/generations" \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","prompt":"a simple blue circle on white","n":1,"response_format":"url"}' \
  | jq -r '.data[0].url'

# image generate → local file (b64)
curl -s "$BASE/openai/v1/images/generations" \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","prompt":"red square","n":1,"response_format":"b64_json"}' \
  | jq -r '.data[0].b64_json' | base64 -d > out.jpg
```

### Vision (Data URI)

Prefer building the multimodal body in Python (image ≥ ~64×64):

```bash
python3 - <<'PY'
import base64, json, os, urllib.request
BASE = os.environ.get("GROK_API_BASE", "http://127.0.0.1:4982")
b = open("sample.png", "rb").read()
uri = "data:image/png;base64," + base64.b64encode(b).decode()
body = {
    "model": "fast",
    "messages": [{"role": "user", "content": [
        {"type": "text", "text": "what is in this image?"},
        {"type": "image_url", "image_url": {"url": uri}},
    ]}],
}
req = urllib.request.Request(
    BASE + "/openai/v1/chat/completions",
    data=json.dumps(body).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
print(json.load(urllib.request.urlopen(req))["choices"][0]["message"]["content"])
PY
```

### Python (stdlib)

```bash
export GROK_API_BASE=http://127.0.0.1:4982
python3 examples/openai_client.py
```

Inline:

```python
import json, os, urllib.request

BASE = os.environ.get("GROK_API_BASE", "http://127.0.0.1:4982")

def post(path, body):
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req) as r:
        return json.load(r)

# chat
r = post("/openai/v1/chat/completions", {
    "model": "fast",
    "messages": [{"role": "user", "content": "hi"}],
})
print(r["choices"][0]["message"]["content"])

# image gen
r = post("/openai/v1/images/generations", {
    "model": "fast",
    "prompt": "blue circle",
    "n": 1,
    "response_format": "url",
})
print(r["data"][0]["url"])
```

### OpenAI Python SDK

```bash
pip install openai
```

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:4982/openai/v1",  # or .../v1
    api_key="not-needed",  # proxy does not validate keys
)

# text
print(client.chat.completions.create(
    model="fast",
    messages=[{"role": "user", "content": "hi"}],
).choices[0].message.content)

# vision (data URI)
import base64
b64 = base64.b64encode(open("sample.png", "rb").read()).decode()
print(client.chat.completions.create(
    model="fast",
    messages=[{"role": "user", "content": [
        {"type": "text", "text": "what is in this image?"},
        {"type": "image_url", "image_url": {"url": f"data:image/png;base64,{b64}"}},
    ]}],
).choices[0].message.content)

# image generate
img = client.images.generate(
    model="fast",
    prompt="a simple blue circle on white",
    n=1,
    response_format="url",
)
print(img.data[0].url)
```

### Client Notes

| Topic | Detail |
|---|---|
| Health first | `grok_ready=false` means cookies are missing, invalid, or expired |
| Models | `fast` / `auto` / `expert` from `/models`; other strings are often mapped to `auto` |
| Vision | **data URI only**; remote `https://` images are not supported |
| Vision size | min ~64×64; 1×1 fails |
| Image gen | `n` 1..4; `response_format` `url` \| `b64_json`; `size` is a no-op |
| Streaming | `stream=true` → **501** |
| Multi-turn | each call is a new temporary conversation |
| Timeouts | image gen can take 30–90s; use a long client timeout |
| Proxy auth | no `Authorization` header required (unless you add a front proxy) |

## API Reference

Routes are registered under both `/openai/v1/*` and `/v1/*`.

### `GET /health`

```json
{
  "status": "ok",
  "service": "grok-web-to-api",
  "grok_ready": true,
  "has_cookies": true,
  "user_id": "<uuid>",
  "reverse": "rest_ok_chat_ws_image_gen_wired"
}
```

### `GET /openai/v1/models` · `/v1/models`

Modes from `POST https://grok.com/rest/modes` (fallback: `auto`, `fast`, `expert`).

### `POST /openai/v1/chat/completions` · `/v1/chat/completions`

Minimal body:

```json
{
  "model": "fast",
  "messages": [{"role": "user", "content": "hello"}],
  "stream": false
}
```

| Field | Behavior |
|---|---|
| `content` | string **or** OpenAI part array: `text` + `image_url` |
| `image_url.url` | must be `data:image/...;base64,...` (not `https://...`) |
| `stream` | `true` → **501** |
| `model` | empty / `grok-3` mapped to `auto` at the gateway |
| response | non-stream: `choices[0].message.content` string |

### `POST /openai/v1/images/generations` · `/v1/images/generations`

```json
{
  "model": "fast",
  "prompt": "blue circle on white",
  "n": 1,
  "response_format": "url"
}
```

| Field | Notes |
|---|---|
| `prompt` | required |
| `model` | default `fast` |
| `n` | 1..4 → `image_generation_count` |
| `response_format` | `url` (default) or `b64_json` |
| `size` | accepted, **ignored** (Grok chooses aspect) |

- `url` → `https://assets.grok.com/users/<uid>/generated/<uuid>/image.jpg`
- `b64_json` → base64 from stream `data:` URI or by fetching the asset

## Architecture

### REST Bootstrap

| Call | Purpose |
|---|---|
| `GET /api/auth/session` | Verify login; obtain `userId` |
| `POST /rest/modes` `{"locale":"en"}` | Mode catalog |
| `POST /http/upload-file-v2/direct` multipart `file` | Vision upload |
| `GET /rest/app-chat/upload-file-v2/status?uploadId=` | Poll upload when needed |

### WebSocket Gateway (Chat / Vision / Image Gen)

```text
wss://grok.com/ws/mgw/?uid=<x-userid>
```

Envelope: `{ "session_id"?, "event": { "type", "event_id", ... } }`

Sequence:

1. `session.create` (model + `x_grok` flags)
2. `session.created` + `conversation.attached`
3. optional upload → `fileMetadataId`
4. `conversation.item.create` + `response.create`
5. stream events → `response.done`

| Path | Mechanism |
|---|---|
| Text out | `response.output_text.delta` / `.done` (skip `x_grok.is_thinking`) |
| Vision in | upload → `file_attachment_ids` + `input_chunks` mention, then text |
| Image gen out | `session.x_grok.enable_image_generation=true` → `response.grok.output.output.card_attachment` (`cardType=generated_image_card`, `image_chunk.imageUrl` data: then relative key) |

Pitfall: 1×1 image → `Image dimensions are insufficient`. Use ≥ ~64×64.

Timeouts in `ws_chat.go`: text ~90s; image-gen ~150s (or context deadline).

### Local HTTP Surface

`internal/server/server.go` registers:

- `/health`
- `/openai/v1/models`, `/v1/models`
- `/openai/v1/chat/completions`, `/v1/chat/completions`
- `/openai/v1/images/generations`, `/v1/images/generations`

Middleware: CORS `*`, `OPTIONS` → `204`, optional in-memory rate limit.

## Project Structure

```text
cmd/server/main.go          process entrypoint
internal/config/config.go   env load + soft validation
internal/server/server.go   HTTP mux, CORS, rate limit, health
internal/openai/handler.go  OpenAI surface handlers
internal/openai/dto/dto.go  request/response JSON shapes
internal/grok/client.go     Grok client facade
internal/grok/cookies.go    cookie load/cache/header
internal/grok/upload.go     multipart vision upload
internal/grok/ws_chat.go    WebSocket chat + image card parse
pkg/logger/logger.go        zap logger
examples/openai_client.py   Python smoke client
.env.example                tracked config template
.env / .cookies / server    local only (gitignored)
Dockerfile                  multi-stage Go binary :4982
```

### `cmd/server/main.go`

| Symbol | Purpose |
|---|---|
| `main` | load config → logger → `grok.Client.Init` → HTTP server → SIGINT/SIGTERM graceful shutdown 10s |

### `internal/config/config.go`

| Item | Purpose |
|---|---|
| `Config` / `ServerConfig` / `GrokConfig` / `RateLimitConfig` | settings structs |
| `New()` | `godotenv.Load` + env defaults (`PORT=4982`, `GROK_BASE_URL=https://grok.com`, …) |
| `composeCookiesFromEnv` | build Cookie header from `GROK_SSO` / friends when `GROK_COOKIES` empty |
| `Validate()` | numeric port only; cookies **not** required to boot |
| `HasGrokAuth()` | cookie file / raw cookies / legacy token pair present? |
| `getEnv` / `getEnvInt` / `getEnvBool` | env helpers |

### `internal/server/server.go`

| Item | Purpose |
|---|---|
| `Server` | cfg, log, grok client, `http.Server`, rate-limit counter |
| `New` | constructor |
| `Handler()` | register OpenAI routes + health |
| `withMiddleware` | CORS `*`, OPTIONS 204, optional rate limit |
| `allow()` | in-memory window counter |
| `health` | JSON status + `grok_ready` + `user_id` + reverse tag |
| `Start` / `Shutdown` / `Addr` | listen lifecycle + graceful stop |

### `internal/openai/dto/dto.go`

| Type | Purpose |
|---|---|
| `ChatCompletionRequest` | model, messages, stream |
| `ChatCompletionMessage` | role + `MessageContent` |
| `MessageContent` | string **or** part array; `UnmarshalJSON` / `MarshalJSON` |
| `ContentPart` / `ImageURL` | `type=text\|image_url` |
| `Plain()` | join text parts |
| `ImageDataURLs()` | list image URLs from parts |
| `ChatCompletionResponse` / `Choice` / `Usage` | chat response shape |
| `ModelListResponse` / `ModelData` | models shape |
| `ErrorResponse` / `ErrorBody` | OpenAI-ish error JSON |
| `ImageGenerationRequest` | prompt, n, response_format, size (ignored) |
| `ImageGenerationResponse` / `ImageGenerationData` | url / b64_json / revised_prompt |

### `internal/openai/handler.go`

| Function | Purpose |
|---|---|
| `NewHandler` | bind grok client + logger |
| `Models` | list models → OpenAI JSON |
| `ChatCompletions` | parse body → last user msg → upload data-URI images → `GenerateContentWithFiles` → chat.completion |
| `ImageGenerations` | parse body → `GenerateImages` → map to `url` or `b64_json` |
| `lastUserMessage` | last `role=user` message |
| `parseDataURL` | decode `data:[mime];base64,...` (reject remote http) |
| `extForMIME` | `.png` / `.jpg` / … for upload filename |
| `roughTokens` | coarse usage (word count) |
| `fetchURLBase64` | GET asset URL → base64 (for `b64_json` when stream only has relative key) |
| `writeJSON` / `writeErr` | response helpers |

### `internal/grok/client.go`

| Item | Purpose |
|---|---|
| `ErrNotWired` | error when cookie/session missing |
| `Client` | HTTP client + cookie store + health + models + userID |
| `ModelInfo` | id/created/owned_by for model list |
| `GeneratedImage` | imagine result: UUID, URL, B64, revised prompt, model |
| `Response` | text + conversation/response id + `Images[]` |
| `NewClient` | constructor + fallback model list |
| `Init` | load cookie cache/sources → probe session → refresh modes → healthy |
| `Close` | close idle HTTP |
| `IsHealthy` / `UserID` / `CookieStore` / `ListModels` | accessors |
| `GenerateContent` | chat text only |
| `GenerateContentWithFiles` | chat + fileMetadataIds (vision) |
| `GenerateImages` | chat path + `enable_image_generation` |
| `probeSession` | `GET /api/auth/session` |
| `refreshModes` | `POST /rest/modes` → update catalog |
| `doJSON` | REST JSON helper to grok.com + cookie header |

### `internal/grok/cookies.go`

| Item | Purpose |
|---|---|
| `essentialCookieNames` | whitelist: `sso`, `sso-rw`, `x-userid`, `cf_clearance`, `__cf_bm`, `grok_device_id` |
| `CookieStore` | AuthToken/CT0 legacy, Raw header, File Netscape, Pairs map, cache |
| `LoadCache` / `SaveCache` | `.cookies/<hash>.json` |
| `LoadSources` | merge file + env raw + legacy → `Pairs` |
| `HasAuth` | is `sso` present? (request gate) |
| `Header` | `Cookie:` **filtered** via `joinEssential` |
| `Get` / `String` | read one name / compact debug |
| Netscape + `k=v; k2=v2` parsers | fill `Pairs` (`grok.com` domain only) |

### `internal/grok/upload.go`

| Item | Purpose |
|---|---|
| `UploadResult` | uploadId + fileMetadataId + mime/name/uri |
| `UploadBytes` | `POST /http/upload-file-v2/direct` multipart field `file` + Origin/Cookie |

Used on the vision path only (not image generation).

### `internal/grok/ws_chat.go`

| Item | Purpose |
|---|---|
| `chatOpts` | fileIDs, enableImageGen, imageGenCount |
| `chatViaGateway` | dial WS, session.create, item+response, read stream until `response.done` |
| `ingestCardAttachment` | parse `response.grok.output` card → `GeneratedImage` map |
| `finalizeImages` | order card results into a slice |
| `stripGrokRender` | strip `<grok:render ...>` placeholders from image-gen text |
| `isThinkingEvent` | skip thinking deltas |
| `wsSend` | JSON text frame |
| `assetsHost` | prefix `https://assets.grok.com/` for relative keys |

Timeout: text ~90s; image-gen ~150s (or context deadline).

### `pkg/logger/logger.go`

| Function | Purpose |
|---|---|
| `New(level)` | zap production/development logger from `LOG_LEVEL` |

### Other Artifacts

| Path | Purpose |
|---|---|
| `examples/openai_client.py` | stdlib smoke: health → models → one text chat `POST`; env `GROK_API_BASE` |
| `Dockerfile` | static Go 1.22 alpine binary → non-root runtime, `EXPOSE 4982` |
| `.gitignore` | ignores `/server`, `.env`, `.cookies/`, `*cookies*.txt`, `*.har`, logs, media probes; keeps `.env.example`, source, README |

## Limitations

- Not an official xAI API; depends on cookie + UI wire
- Expired cookies → upstream `403`/`401`; re-export cookies
- `stream=true` not implemented (**501**)
- Video not implemented
- Chat vision: **data URI only**
- Image gen `n` clamped 1..4; `size` is a no-op
- Multi-turn same conversation / tools / memory not implemented
- Rate limit is in-memory per process only

## Contributing

1. Fork and create a feature branch.
2. Keep diffs minimal; preserve existing behavior unless the change is intentional.
3. Do not commit secrets: `.env`, cookie files, `.cookies/`, HAR captures, or binaries.
4. Verify with `go build ./cmd/server` and smoke `/health` plus the endpoints you touch.
5. Open a PR with a clear description of the wire or API change.

## License

MIT — see `LICENSE`.
