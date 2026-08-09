package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"grok-web-to-api/internal/grok"
	"grok-web-to-api/internal/openai/dto"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Handler struct {
	client *grok.Client
	log    *zap.Logger
}

func NewHandler(client *grok.Client, log *zap.Logger) *Handler {
	return &Handler{client: client, log: log}
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	models := h.client.ListModels()
	data := make([]dto.ModelData, 0, len(models))
	for _, m := range models {
		data = append(data, dto.ModelData{
			ID:      m.ID,
			Object:  "model",
			Created: m.Created,
			OwnedBy: m.OwnedBy,
		})
	}
	writeJSON(w, http.StatusOK, dto.ModelListResponse{Object: "list", Data: data})
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}

	var req dto.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "invalid json body")
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "messages required")
		return
	}
	if req.Model == "" {
		req.Model = "auto"
	}

	user := lastUserMessage(req.Messages)
	if user == nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "no user message")
		return
	}
	prompt := strings.TrimSpace(user.Content.Plain())
	images := user.Content.ImageDataURLs()
	if prompt == "" && len(images) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "no user message content")
		return
	}
	if prompt == "" {
		prompt = "Describe the attached image(s)."
	}

	if req.Stream {
		// ponytail: SSE stream after HAR documents Grok stream frames
		writeErr(w, http.StatusNotImplemented, "not_implemented", "stream=true not wired yet; use stream=false")
		return
	}

	// upload images → fileMetadataIds (image only; video not proven)
	var fileIDs []string
	for i, u := range images {
		name, mime, data, err := parseDataURL(u)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("image[%d]: %v", i, err))
			return
		}
		if !strings.HasPrefix(mime, "image/") {
			writeErr(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("image[%d]: only image/* supported (got %s); video not wired", i, mime))
			return
		}
		if name == "" {
			name = fmt.Sprintf("image_%d%s", i, extForMIME(mime))
		}
		up, err := h.client.UploadBytes(r.Context(), name, mime, data)
		if err != nil {
			h.log.Error("upload failed", zap.Error(err))
			writeErr(w, http.StatusBadGateway, "upstream_error", err.Error())
			return
		}
		fileIDs = append(fileIDs, up.FileMetadataID)
	}

	resp, err := h.client.GenerateContentWithFiles(r.Context(), req.Model, prompt, fileIDs)
	if err != nil {
		if errors.Is(err, grok.ErrNotWired) {
			writeErr(w, http.StatusNotImplemented, "not_wired", err.Error())
			return
		}
		h.log.Error("generate failed", zap.Error(err))
		writeErr(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}

	out := dto.ChatCompletionResponse{
		ID:      "chatcmpl-" + uuid.NewString(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []dto.ChatCompletionChoice{{
			Index: 0,
			Message: dto.ChatCompletionMessage{
				Role:    "assistant",
				Content: dto.MessageContent{Text: resp.Text},
			},
			FinishReason: "stop",
		}},
		Usage: dto.Usage{
			PromptTokens:     roughTokens(prompt),
			CompletionTokens: roughTokens(resp.Text),
			TotalTokens:      roughTokens(prompt) + roughTokens(resp.Text),
		},
	}
	writeJSON(w, http.StatusOK, out)
}


func (h *Handler) ImageGenerations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var req dto.ImageGenerationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "invalid json body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "prompt required")
		return
	}
	if req.Model == "" {
		req.Model = "fast"
	}
	n := req.N
	if n <= 0 {
		n = 1
	}
	if n > 4 {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "n must be 1..4")
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.ResponseFormat))
	if format == "" {
		format = "url"
	}
	if format != "url" && format != "b64_json" {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "response_format must be url or b64_json")
		return
	}

	resp, err := h.client.GenerateImages(r.Context(), req.Model, prompt, n)
	if err != nil {
		if errors.Is(err, grok.ErrNotWired) {
			writeErr(w, http.StatusNotImplemented, "not_wired", err.Error())
			return
		}
		h.log.Error("image gen failed", zap.Error(err))
		writeErr(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	if len(resp.Images) == 0 {
		writeErr(w, http.StatusBadGateway, "upstream_error", "provider returned no generated images")
		return
	}

	data := make([]dto.ImageGenerationData, 0, len(resp.Images))
	for _, img := range resp.Images {
		item := dto.ImageGenerationData{RevisedPrompt: img.RevisedPrompt}
		if item.RevisedPrompt == "" {
			item.RevisedPrompt = prompt
		}
		switch format {
		case "b64_json":
			b64 := img.B64
			if b64 == "" && img.URL != "" {
				if strings.HasPrefix(img.URL, "data:") {
					if i := strings.IndexByte(img.URL, ','); i >= 0 {
						b64 = img.URL[i+1:]
					}
				} else {
					var err error
					b64, err = fetchURLBase64(r.Context(), img.URL)
					if err != nil {
						h.log.Error("fetch image b64", zap.Error(err), zap.String("url", img.URL))
						writeErr(w, http.StatusBadGateway, "upstream_error", err.Error())
						return
					}
				}
			}
			if b64 == "" {
				writeErr(w, http.StatusBadGateway, "upstream_error", "image missing b64 payload")
				return
			}
			item.B64JSON = b64
		default:
			u := img.URL
			if u == "" && img.B64 != "" {
				u = "data:image/jpeg;base64," + img.B64
			}
			item.URL = u
		}
		data = append(data, item)
		if len(data) >= n {
			break
		}
	}
	writeJSON(w, http.StatusOK, dto.ImageGenerationResponse{
		Created: time.Now().Unix(),
		Data:    data,
	})
}

func fetchURLBase64(ctx context.Context, raw string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://grok.com/")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asset fetch %s → %d", raw, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 20<<20))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func lastUserMessage(msgs []dto.ChatCompletionMessage) *dto.ChatCompletionMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(msgs[i].Role, "user") {
			return &msgs[i]
		}
	}
	return nil
}

func parseDataURL(u string) (name, mime string, data []byte, err error) {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return "", "", nil, fmt.Errorf("only data:image/...;base64 URLs supported (no remote fetch)")
	}
	if !strings.HasPrefix(u, "data:") {
		return "", "", nil, fmt.Errorf("expected data URL")
	}
	// data:[<mime>][;base64],<data>
	rest := strings.TrimPrefix(u, "data:")
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", "", nil, fmt.Errorf("malformed data URL")
	}
	meta, payload := rest[:comma], rest[comma+1:]
	mime = "application/octet-stream"
	isB64 := false
	for _, p := range strings.Split(meta, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.EqualFold(p, "base64") {
			isB64 = true
			continue
		}
		if strings.Contains(p, "/") {
			mime = p
		}
	}
	if !isB64 {
		return "", "", nil, fmt.Errorf("only base64 data URLs supported")
	}
	data, err = base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// try raw std without padding issues
		data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
		if err != nil {
			return "", "", nil, fmt.Errorf("base64 decode: %w", err)
		}
	}
	return "", mime, data, nil
}

func extForMIME(mime string) string {
	switch strings.ToLower(mime) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
}

func roughTokens(s string) int {
	n := len(strings.Fields(s))
	if n == 0 && s != "" {
		return 1
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, dto.ErrorResponse{
		Error: dto.ErrorBody{Message: msg, Type: typ},
	})
}
