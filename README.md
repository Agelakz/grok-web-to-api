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
# set GROK_COOKIE_FILE ke Netscape cookies.txt (login https://grok.com)

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

## Cookies / auth

1. Login `https://grok.com`
2. Export **Netscape** cookies (ext: Get cookies.txt LOCALLY, dll.)
3. `.env`: `GROK_COOKIE_FILE=/path/to/grok.com_cookies.txt`

Wajib berguna: `sso`, `sso-rw`. Berguna: `x-userid`, `cf_clearance`, `__cf_bm`.

Alternatif: `GROK_COOKIES` = full `Cookie` header string.

Boot **soft** tanpa cookie: HTTP surface hidup, `grok_ready=false`, chat/image gagal sampai cookie valid.

Cache runtime: `.cookies/<sha>.json` (gitignore).

`GROK_AUTH_TOKEN` / `GROK_CT0` = legacy placeholder X — **tidak** dipakai path grok.com.

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
| `CookieStore` | AuthToken/CT0 legacy, Raw header, File Netscape, Pairs map, cache |
| `LoadCache` / `SaveCache` | `.cookies/<hash>.json` |
| `LoadSources` | gabung file + env raw + legacy |
| `HasAuth` | cukup buat request? (`sso` pair) |
| `Header` | string `Cookie:` siap kirim |
| `Get` / `String` | baca nama cookie / debug ringkas |
| parser Netscape + `k=v; k2=v2` | isi `Pairs` |

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
| `GROK_COOKIES` | — | raw Cookie header |
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
