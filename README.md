# Grok Web To API

Proxy lokal: session browser **Grok** (`https://grok.com`) → REST mirip **OpenAI**.

> Research / educational only. Tidak berafiliasi dengan xAI / X. Reverse cookie UI bisa langgar ToS — risiko sendiri.

Default port: **`4982`** (gemini sibling biasanya `:4981`).

---

## Apa yang project ini lakukan

| Kebutuhan | Endpoint | Status |
|---|---|---|
| Health / session ready | `GET /health` | done |
| List mode model | `GET /openai/v1/models` (+ `/v1/models`) | done |
| Chat text | `POST /openai/v1/chat/completions` | done (non-stream) |
| Vision (baca gambar) | chat + multimodal `image_url` data URI | done |
| Image generate | `POST /openai/v1/images/generations` | done |
| SSE stream | `stream=true` | **501** belum |
| Video | — | belum |
| Remote `http(s)` image fetch di chat | — | sengaja tidak (data URI only) |

Setiap chat/image-gen call = **conversation temporary** baru (`is_temporary: true`). Bukan multi-turn shared history.

---

## Quick start

```bash
cp .env.example .env
# isi cookie — path (A) file, (B) per-cookie env, atau (C) GROK_COOKIES
# lihat ## Cookies / auth

go run ./cmd/server
# atau: go build -o server ./cmd/server && ./server
```

Smoke:

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
    {\"type\":\"text\",\"text\":\"apa di gambar?\"},
    {\"type\":\"image_url\",\"image_url\":{\"url\":\"data:image/png;base64,$B64\"}}
  ]}]}" | jq

# image generate
curl -s localhost:4982/openai/v1/images/generations \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","prompt":"a simple blue circle on white","n":1,"response_format":"url"}' | jq
# response_format: url | b64_json ; n 1..4 ; size diabaikan
```

Python smoke: `python3 examples/openai_client.py`  
(`GROK_API_BASE` default `http://127.0.0.1:4982`)

---

## Cara pakai (script)

Base default: `http://127.0.0.1:4982`  
Alias path: `/openai/v1/*` **dan** `/v1/*` (sama).

### 1. Start server + cookie

```bash
cd /path/to/grok-web-to-api
cp .env.example .env

# path B (enak script) — paste value dari browser DevTools → Application → Cookies → grok.com
# atau path A: GROK_COOKIE_FILE=/path/to/grok.com_cookies.txt

cat >> .env <<'EOF'
GROK_SSO=...
GROK_SSO_RW=...
GROK_USER_ID=...
GROK_CF_CLEARANCE=...
EOF

go build -o server ./cmd/server
./server
# log: listening :4982 ; health must show grok_ready=true
```

One-shot tanpa `.env` file:

```bash
export GROK_SSO='...' GROK_SSO_RW='...' GROK_USER_ID='...' GROK_CF_CLEARANCE='...'
./server
```

Cek ready:

```bash
curl -s localhost:4982/health | jq '{grok_ready,has_cookies,user_id,reverse}'
# harap: grok_ready=true
```

### 2. curl

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

# image generate → file lokal (b64)
curl -s "$BASE/openai/v1/images/generations" \
  -H 'Content-Type: application/json' \
  -d '{"model":"fast","prompt":"red square","n":1,"response_format":"b64_json"}' \
  | jq -r '.data[0].b64_json' | base64 -d > out.jpg
```

Vision (data URI; image ≥ ~64×64) — body JSON lebih gampang lewat Python:

```bash
python3 - <<'PY'
import base64, json, os, urllib.request
BASE = os.environ.get("GROK_API_BASE", "http://127.0.0.1:4982")
b = open("sample.png", "rb").read()
uri = "data:image/png;base64," + base64.b64encode(b).decode()
body = {
    "model": "fast",
    "messages": [{"role": "user", "content": [
        {"type": "text", "text": "apa di gambar?"},
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

### 3. Python stdlib (no deps)

```bash
export GROK_API_BASE=http://127.0.0.1:4982
python3 examples/openai_client.py
```

Atau inline:

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

### 4. OpenAI Python SDK (point ke proxy)

```bash
pip install openai
```

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:4982/openai/v1",  # atau .../v1
    api_key="not-needed",  # proxy gak cek key
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
        {"type": "text", "text": "apa di gambar?"},
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

### 5. Docker

```bash
docker build -t grok-web-to-api .

# path B env
docker run --rm -p 4982:4982 \
  -e GROK_SSO -e GROK_SSO_RW -e GROK_USER_ID -e GROK_CF_CLEARANCE \
  grok-web-to-api

# path A file
docker run --rm -p 4982:4982 --env-file .env \
  -v /path/to/grok.com_cookies.txt:/cookies.txt:ro \
  -e GROK_COOKIE_FILE=/cookies.txt \
  grok-web-to-api
```

### Catatan script

| tip | detail |
|---|---|
| health dulu | `grok_ready=false` → cookie belum valid / expired |
| model | `fast` / `auto` / `expert` (dari `/models`); string lain sering di-map `auto` |
| vision | **data URI only**; remote `https://` image **belum** |
| vision size | min ~64×64; 1×1 gagal |
| image gen | `n` 1..4; `response_format` `url` \| `b64_json`; `size` no-op |
| stream | `stream=true` → **501** |
| multi-turn | tiap call = conversation temporary baru |
| timeout | image gen bisa 30–90s; set client timeout long |
| auth proxy | gak butuh `Authorization` header (kecuali lo tambah sendiri di depan) |

---

## Cookies / auth

Login `https://grok.com` dulu. Pilih **1 path** di `.env` / shell.

### Path B — per-cookie env (enak buat script)

```bash
# .env  — atau export di shell / docker -e
GROK_SSO=eyJ...                 # wajib
GROK_SSO_RW=eyJ...              # praktis wajib
GROK_USER_ID=72d0b43d-....      # x-userid
GROK_CF_CLEARANCE=....          # recommended
# optional:
# GROK_CF_BM=....
# GROK_DEVICE_ID=....
```

Script one-shot (no file):

```bash
export GROK_SSO='...'
export GROK_SSO_RW='...'
export GROK_USER_ID='...'
export GROK_CF_CLEARANCE='...'
./server
```

Docker:

```bash
docker run --rm -p 4982:4982 \
  -e GROK_SSO \
  -e GROK_SSO_RW \
  -e GROK_USER_ID \
  -e GROK_CF_CLEARANCE \
  grok-web-to-api
```

Config compose otomatis: `sso=...; sso-rw=...; x-userid=...; cf_clearance=...`  
(`composeCookiesFromEnv` di `internal/config/config.go`)

### Path C — satu string Cookie

```bash
GROK_COOKIES='sso=...; sso-rw=...; x-userid=...; cf_clearance=...'
```

Kalau `GROK_COOKIES` terisi, **menang** atas compose per-cookie env.

### Path A — file Netscape

```bash
GROK_COOKIE_FILE=/path/to/grok.com_cookies.txt
```

Export full (ext: **Get cookies.txt LOCALLY**) OK — runtime filter essential.

### Map env → cookie name

| env | cookie | wajib? | role |
|---|---|---|---|
| `GROK_SSO` | `sso` | **ya** | login; gate `HasAuth` |
| `GROK_SSO_RW` | `sso-rw` | ya (praktis) | twin SSO |
| `GROK_USER_ID` | `x-userid` | ya (praktis) | WS `uid`; fallback probe session |
| `GROK_CF_CLEARANCE` | `cf_clearance` | recommended | Cloudflare pass |
| `GROK_CF_BM` | `__cf_bm` | optional | CF bot (TTL pendek) |
| `GROK_DEVICE_ID` | `grok_device_id` | optional | device fingerprint |
| `GROK_COOKIES` | (semua di string) | alt | raw header, di-filter essential |
| `GROK_COOKIE_FILE` | (semua di file) | alt | Netscape, domain `grok.com` only |

**Minimal** biar chat/vision/image-gen jalan: `sso` + `sso-rw` + `x-userid` + `cf_clearance`.

### Dibuang otomatis (noise)

Analytics / ads / consent — **tidak** dikirim:

`Optanon*`, `mp_*_mixpanel`, `__stripe_*`, `_gcl_au`, `_twpid`, `__cuid`, `i18nextLng`, ...

Contoh export 16 baris → kirim ~6.

### Alur load

```
GROK_COOKIE_FILE (Netscape) ──► parse domain grok.com
                                      │
GROK_COOKIES  ──────────────────────► Pairs map ──► Header() = essential only
                                      │
GROK_SSO / GROK_SSO_RW / ... ─────────┘
   (composeCookiesFromEnv, kalau GROK_COOKIES kosong)

legacy GROK_AUTH_TOKEN/CT0 diabaikan path grok.com
```

Prioritas value: file load dulu, raw/`GROK_COOKIES`/compose override ke `Pairs`.  
`Header()` **selalu** whitelist essential — noise drop.

- Boot soft tanpa cookie: surface hidup, `grok_ready=false`
- Cache: `.cookies/<sha>.json` (gitignore)
- Expired → 401/403 upstream; re-export / re-set env + restart

### Tips

- Ambil dari tab **login** grok.com (bukan x.com)
- Tanpa `sso` → `HasAuth=false`
- `cf_clearance` / `__cf_bm` gampang basi (CF)
- Jangan commit `.env` / `*cookies*.txt` / `.cookies/`

---

## API surface (OpenAI-compatible)

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

Mode dari `POST https://grok.com/rest/modes` (fallback: `auto`, `fast`, `expert`).

### `POST /openai/v1/chat/completions` · `/v1/chat/completions`

Body minimal:

```json
{
  "model": "fast",
  "messages": [{"role": "user", "content": "hello"}],
  "stream": false
}
```

- `content` string **atau** array part OpenAI: `text` + `image_url`
- `image_url.url` harus `data:image/...;base64,...` (bukan `https://...`)
- `stream: true` → **501**
- model kosong / `grok-3` → dipetakan `auto` di gateway
- response non-stream: `choices[0].message.content` string

### `POST /openai/v1/images/generations` · `/v1/images/generations`

```json
{
  "model": "fast",
  "prompt": "blue circle on white",
  "n": 1,
  "response_format": "url"
}
```

| field | notes |
|---|---|
| `prompt` | wajib |
| `model` | default `fast` |
| `n` | 1..4 → `image_generation_count` |
| `response_format` | `url` (default) atau `b64_json` |
| `size` | diterima, **diabaikan** (Grok pilih aspect) |

`url` → `https://assets.grok.com/users/<uid>/generated/<uuid>/image.jpg`  
`b64_json` → base64 dari stream data: atau fetch asset.

---

## Wire Grok (bukti live, bukan tebakan)

### REST bootstrap

| Call | Fungsi |
|---|---|
| `GET /api/auth/session` | cek login, ambil `userId` |
| `POST /rest/modes` `{"locale":"en"}` | katalog mode |
| `POST /http/upload-file-v2/direct` multipart `file` | upload vision |
| `GET /rest/app-chat/upload-file-v2/status?uploadId=` | poll upload (kalau perlu) |

### Chat / vision / image-gen = WebSocket gateway

```text
wss://grok.com/ws/mgw/?uid=<x-userid>
```

Envelope: `{ "session_id"?, "event": { "type", "event_id", ... } }`

Alur:

1. `session.create` (model + `x_grok` flags)
2. `session.created` + `conversation.attached`
3. optional upload → `fileMetadataId`
4. `conversation.item.create` + `response.create`
5. stream events → `response.done`

**Text out:** `response.output_text.delta` / `.done` (skip `x_grok.is_thinking`)

**Vision in:** upload → `file_attachment_ids` + `input_chunks` mention dulu lalu text

**Image gen out:** `session.x_grok.enable_image_generation=true` →  
`response.grok.output.output.card_attachment`  
(`cardType=generated_image_card`, `image_chunk.imageUrl` data: lalu relative key)

Pitfall: 1×1 image → `Image dimensions are insufficient`. Pakai ≥ ~64×64.

---

## Layout repo + fungsi tiap file

```
cmd/server/main.go          entrypoint proses
internal/config/config.go   load env + validasi soft
internal/server/server.go   HTTP mux, CORS, rate limit, health
internal/openai/handler.go  handler OpenAI surface
internal/openai/dto/dto.go  request/response JSON shapes
internal/grok/client.go     facade client Grok
internal/grok/cookies.go    cookie load/cache/header
internal/grok/upload.go     multipart upload vision
internal/grok/ws_chat.go    WebSocket chat + parse image cards
pkg/logger/logger.go        zap logger
examples/openai_client.py   smoke Python
.env.example                template config (tracked)
.env / .cookies / server    local only (gitignore)
Dockerfile                  multi-stage Go binary :4982
```

### `cmd/server/main.go`

| Fungsi | Kegunaan |
|---|---|
| `main` | load config → logger → `grok.Client.Init` → HTTP server → SIGINT/SIGTERM graceful shutdown 10s |

### `internal/config/config.go`

| Item | Kegunaan |
|---|---|
| `Config` / `ServerConfig` / `GrokConfig` / `RateLimitConfig` | struktur settings |
| `New()` | `godotenv.Load` + env defaults (`PORT=4982`, `GROK_BASE_URL=https://grok.com`, …) |
| `Validate()` | port numeric only; cookie **tidak** wajib boot |
| `HasGrokAuth()` | ada cookie file / raw cookies / legacy token pair? |
| `getEnv` / `getEnvInt` / `getEnvBool` | helper env |

### `internal/server/server.go`

| Item | Kegunaan |
|---|---|
| `Server` | pegang cfg, log, grok client, `http.Server`, counter rate limit |
| `New` | konstruktor |
| `Handler()` | register routes OpenAI + health |
| `withMiddleware` | CORS `*`, OPTIONS 204, optional rate limit |
| `allow()` | window counter in-memory |
| `health` | JSON status + `grok_ready` + `user_id` + reverse tag |
| `Start` / `Shutdown` / `Addr` | lifecycle listen + graceful stop |

Routes:

- `/health`
- `/openai/v1/models`, `/v1/models`
- `/openai/v1/chat/completions`, `/v1/chat/completions`
- `/openai/v1/images/generations`, `/v1/images/generations`

### `internal/openai/dto/dto.go`

| Type | Kegunaan |
|---|---|
| `ChatCompletionRequest` | model, messages, stream |
| `ChatCompletionMessage` | role + `MessageContent` |
| `MessageContent` | string **atau** array part; `UnmarshalJSON` / `MarshalJSON` |
| `ContentPart` / `ImageURL` | `type=text\|image_url` |
| `Plain()` | gabung text parts |
| `ImageDataURLs()` | list URL gambar dari parts |
| `ChatCompletionResponse` / `Choice` / `Usage` | shape balasan chat |
| `ModelListResponse` / `ModelData` | shape models |
| `ErrorResponse` / `ErrorBody` | error JSON OpenAI-ish |
| `ImageGenerationRequest` | prompt, n, response_format, size(ignored) |
| `ImageGenerationResponse` / `ImageGenerationData` | url / b64_json / revised_prompt |

### `internal/openai/handler.go`

| Fungsi | Kegunaan |
|---|---|
| `NewHandler` | bind grok client + logger |
| `Models` | list model → JSON OpenAI |
| `ChatCompletions` | parse body → last user msg → upload data-URI images → `GenerateContentWithFiles` → chat.completion |
| `ImageGenerations` | parse body → `GenerateImages` → map ke `url` atau `b64_json` |
| `lastUserMessage` | ambil message `role=user` terakhir |
| `parseDataURL` | decode `data:[mime];base64,...` (tolak remote http) |
| `extForMIME` | `.png` / `.jpg` / … untuk nama upload |
| `roughTokens` | usage kasar (word count) |
| `fetchURLBase64` | GET asset URL → base64 (untuk `b64_json` kalau stream cuma relative key) |
| `writeJSON` / `writeErr` | helper response |

### `internal/grok/client.go`

| Item | Kegunaan |
|---|---|
| `ErrNotWired` | error “butuh cookie/session” |
| `Client` | HTTP client + cookie store + health + models + userID |
| `ModelInfo` | id/created/owned_by untuk list models |
| `GeneratedImage` | hasil imagine: UUID, URL, B64, revised prompt, model |
| `Response` | text + conversation/response id + `Images[]` |
| `NewClient` | konstruktor + fallback model list |
| `Init` | load cookie cache/sources → probe session → refresh modes → healthy |
| `Close` | close idle HTTP |
| `IsHealthy` / `UserID` / `CookieStore` / `ListModels` | accessors |
| `GenerateContent` | chat text only |
| `GenerateContentWithFiles` | chat + fileMetadataIds (vision) |
| `GenerateImages` | chat path + `enable_image_generation` |
| `probeSession` | `GET /api/auth/session` |
| `refreshModes` | `POST /rest/modes` → update catalog |
| `doJSON` | helper REST JSON ke grok.com + cookie header |

### `internal/grok/cookies.go`

| Item | Kegunaan |
|---|---|
| `essentialCookieNames` | whitelist: `sso`, `sso-rw`, `x-userid`, `cf_clearance`, `__cf_bm`, `grok_device_id` |
| `CookieStore` | AuthToken/CT0 legacy, Raw header, File Netscape, Pairs map, cache |
| `LoadCache` / `SaveCache` | `.cookies/<hash>.json` |
| `LoadSources` | gabung file + env raw + legacy → `Pairs` |
| `HasAuth` | ada `sso`? (gate request) |
| `Header` | `Cookie:` **filtered** lewat `joinEssential` |
| `Get` / `String` | baca 1 nama / debug ringkas |
| parser Netscape + `k=v; k2=v2` | isi `Pairs` (domain `grok.com` only) |

### `internal/grok/upload.go`

| Item | Kegunaan |
|---|---|
| `UploadResult` | uploadId + fileMetadataId + mime/name/uri |
| `UploadBytes` | `POST /http/upload-file-v2/direct` multipart field `file` + Origin/Cookie |

Dipakai vision path saja (bukan image-gen).

### `internal/grok/ws_chat.go`

| Item | Kegunaan |
|---|---|
| `chatOpts` | fileIDs, enableImageGen, imageGenCount |
| `chatViaGateway` | dial WS, session.create, item+response, baca stream sampai `response.done` |
| `ingestCardAttachment` | parse `response.grok.output` card → map `GeneratedImage` |
| `finalizeImages` | urutkan hasil card jadi slice |
| `stripGrokRender` | buang placeholder `<grok:render ...>` dari text image-gen |
| `isThinkingEvent` | skip delta thinking |
| `wsSend` | JSON text frame |
| `assetsHost` | prefix `https://assets.grok.com/` untuk key relatif |

Timeout: text ~90s; image-gen ~150s (atau deadline context).

### `pkg/logger/logger.go`

| Fungsi | Kegunaan |
|---|---|
| `New(level)` | zap production/development logger dari `LOG_LEVEL` |

### `examples/openai_client.py`

Smoke stdlib: health → models → satu chat `POST` (text). Env `GROK_API_BASE`.

### `Dockerfile`

Build static binary Go 1.22 alpine → image runtime user non-root, `EXPOSE 4982`.

### `.gitignore` (penting)

Abaikan: `/server`, `.env`, `.cookies/`, `*cookies*.txt`, `*.har`, logs, media probe, editor junk.  
Tetap track: `.env.example`, source, README.

---

## Env reference

| Var | Default | Fungsi |
|---|---|---|
| `PORT` | `4982` | listen port |
| `LOG_LEVEL` | `info` | zap level |
| `APP_ENV` | — | dokumentasi / Docker set `production` |
| `GROK_BASE_URL` | `https://grok.com` | origin + REST/WS host |
| `GROK_COOKIE_FILE` | — | path Netscape cookies |
| `GROK_COOKIES` | — | raw Cookie header (override compose) |
| `GROK_SSO` | — | cookie `sso` (**wajib** path B) |
| `GROK_SSO_RW` | — | cookie `sso-rw` |
| `GROK_USER_ID` | — | cookie `x-userid` |
| `GROK_CF_CLEARANCE` | — | cookie `cf_clearance` |
| `GROK_CF_BM` | — | cookie `__cf_bm` (optional) |
| `GROK_DEVICE_ID` | — | cookie `grok_device_id` (optional) |
| `GROK_REFRESH_INTERVAL` | `30` | reserved/retry spacing |
| `GROK_MAX_RETRIES` | `3` | reserved retries |
| `RATE_LIMIT_ENABLED` | `false` | on/off limiter |
| `RATE_LIMIT_WINDOW_MS` | `60000` | window |
| `RATE_LIMIT_MAX_REQUESTS` | `30` | max per window |

---

## Build / run / Docker

```bash
go build -o server ./cmd/server
./server

# Docker
docker build -t grok-web-to-api .
docker run --rm -p 4982:4982 --env-file .env \
  -v /path/to/grok.com_cookies.txt:/cookies.txt:ro \
  -e GROK_COOKIE_FILE=/cookies.txt \
  grok-web-to-api
```

---

## Batasan jujur

- Bukan official xAI API; tergantung cookie + UI wire
- Cookie expired → 403/401 upstream; re-export cookies
- `stream=true` belum
- Video belum
- Chat vision: **data URI only**
- Image gen `n` clamp 1..4; `size` no-op
- Multi-turn same conversation / tools / memory belum
- Rate limit in-memory per process only

---

## License

MIT — lihat `LICENSE`.
