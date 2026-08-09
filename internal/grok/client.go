package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"grok-web-to-api/internal/config"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrNotWired kept for old callers; chat path is wired via WS gateway.
var ErrNotWired = errors.New("grok chat not wired: need cookies + valid session")

// Client talks to grok.com using browser cookies.
// REST: session + modes. Chat: WebSocket gateway /ws/mgw/.
type Client struct {
	cfg     *config.GrokConfig
	cookies *CookieStore
	log     *zap.Logger
	http    *http.Client

	mu      sync.RWMutex
	healthy bool
	models  []ModelInfo
	userID  string
	email   string
}

type ModelInfo struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// GeneratedImage is one Grok imagine card result (live 2026-08-09).
type GeneratedImage struct {
	UUID          string `json:"uuid,omitempty"`
	URL           string `json:"url,omitempty"`            // assets.grok.com or data:
	B64           string `json:"b64,omitempty"`            // raw base64 (no data: prefix)
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	Model         string `json:"model,omitempty"`
}

type Response struct {
	Text           string           `json:"text"`
	ConversationID string           `json:"conversation_id,omitempty"`
	ResponseID     string           `json:"response_id,omitempty"`
	Images         []GeneratedImage `json:"images,omitempty"`
}

func NewClient(cfg *config.Config, log *zap.Logger) *Client {
	return &Client{
		cfg: &cfg.Grok,
		cookies: &CookieStore{
			AuthToken: cfg.Grok.AuthToken,
			CT0:       cfg.Grok.CT0,
			Raw:       cfg.Grok.Cookies,
			File:      cfg.Grok.CookieFile,
			Pairs:     map[string]string{},
			UpdatedAt: time.Now(),
		},
		log: log,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		// fallback catalog if /rest/modes fails
		models: []ModelInfo{
			{ID: "auto", Created: time.Now().Unix(), OwnedBy: "xai"},
			{ID: "fast", Created: time.Now().Unix(), OwnedBy: "xai"},
			{ID: "expert", Created: time.Now().Unix(), OwnedBy: "xai"},
		},
	}
}

func (c *Client) Init(ctx context.Context) error {
	// cache first (soft), then env/file sources win via LoadSources order
	if err := c.cookies.LoadCache(); err == nil {
		c.log.Info("loaded cookie cache metadata")
	}
	if err := c.cookies.LoadSources(); err != nil {
		c.log.Warn("cookie load incomplete", zap.Error(err))
	}

	if !c.cookies.HasAuth() {
		c.log.Warn("no Grok cookies yet — set GROK_COOKIE_FILE or GROK_COOKIES")
		c.setHealthy(false)
		return nil
	}

	if err := c.probeSession(ctx); err != nil {
		c.setHealthy(false)
		c.log.Warn("session probe failed", zap.Error(err))
		// soft: still boot; health=false
		return nil
	}

	if err := c.refreshModes(ctx); err != nil {
		c.log.Warn("modes probe failed; keeping fallback model list", zap.Error(err))
	}

	_ = c.cookies.SaveCache()
	c.setHealthy(true)
	c.log.Info("grok client ready (REST + WS chat)",
		zap.String("cookies", c.cookies.String()),
		zap.String("user_id", c.userID),
		zap.Int("models", len(c.ListModels())),
	)
	return nil
}

func (c *Client) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

func (c *Client) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

func (c *Client) ListModels() []ModelInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ModelInfo, len(c.models))
	copy(out, c.models)
	return out
}

// GenerateContent sends one user turn over the Grok web WS gateway.
func (c *Client) GenerateContent(ctx context.Context, model, prompt string) (*Response, error) {
	return c.GenerateContentWithFiles(ctx, model, prompt, nil)
}

// GenerateContentWithFiles sends text + optional uploaded fileMetadataIds (vision).
func (c *Client) GenerateContentWithFiles(ctx context.Context, model, prompt string, fileIDs []string) (*Response, error) {
	if !c.cookies.HasAuth() {
		return nil, fmt.Errorf("%w: set GROK_COOKIE_FILE=/path/to/cookies.txt", ErrNotWired)
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" && len(fileIDs) == 0 {
		return nil, fmt.Errorf("empty prompt")
	}
	if prompt == "" {
		prompt = "Describe the attached image(s)."
	}
	return c.chatViaGateway(ctx, model, prompt, chatOpts{fileIDs: fileIDs})
}

// GenerateImages runs Grok imagine via gateway (enable_image_generation).
// n maps to image_generation_count (clamped 1..4).
func (c *Client) GenerateImages(ctx context.Context, model, prompt string, n int) (*Response, error) {
	if !c.cookies.HasAuth() {
		return nil, fmt.Errorf("%w: set GROK_COOKIE_FILE=/path/to/cookies.txt", ErrNotWired)
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("empty prompt")
	}
	if n <= 0 {
		n = 1
	}
	// ask model to generate; flags do the real work
	return c.chatViaGateway(ctx, model, prompt, chatOpts{
		enableImageGen: true,
		imageGenCount:  n,
	})
}

func (c *Client) setHealthy(v bool) {
	c.mu.Lock()
	c.healthy = v
	c.mu.Unlock()
}

func (c *Client) CookieStore() *CookieStore { return c.cookies }

func (c *Client) UserID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userID
}

// --- REST probes (HAR-proven) ---

type sessionResp struct {
	Status  string `json:"status"`
	Session struct {
		UserID    string `json:"userId"`
		Email     string `json:"email"`
		SessionID string `json:"sessionId"`
	} `json:"session"`
}

func (c *Client) probeSession(ctx context.Context) error {
	var out sessionResp
	if err := c.doJSON(ctx, http.MethodGet, "/api/auth/session", nil, &out); err != nil {
		return err
	}
	if !strings.EqualFold(out.Status, "authenticated") {
		return fmt.Errorf("session status=%q (want authenticated)", out.Status)
	}
	c.mu.Lock()
	c.userID = out.Session.UserID
	c.email = out.Session.Email
	c.mu.Unlock()
	return nil
}

type modesResp struct {
	Modes []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"modes"`
}

func (c *Client) refreshModes(ctx context.Context) error {
	var out modesResp
	body := map[string]string{"locale": "en"}
	if err := c.doJSON(ctx, http.MethodPost, "/rest/modes", body, &out); err != nil {
		return err
	}
	if len(out.Modes) == 0 {
		return fmt.Errorf("empty modes")
	}
	now := time.Now().Unix()
	models := make([]ModelInfo, 0, len(out.Modes))
	for _, m := range out.Modes {
		if m.ID == "" {
			continue
		}
		// skip paywalled-only names? keep all ids; availability checked by web later
		models = append(models, ModelInfo{ID: m.ID, Created: now, OwnedBy: "xai"})
	}
	if len(models) == 0 {
		return fmt.Errorf("no mode ids")
	}
	c.mu.Lock()
	c.models = models
	c.mu.Unlock()
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, reqBody any, out any) error {
	var rdr io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	url := c.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, */*")
	req.Header.Set("Origin", c.cfg.BaseURL)
	req.Header.Set("Referer", c.cfg.BaseURL+"/")
	req.Header.Set("Cookie", c.cookies.Header())
	req.Header.Set("x-xai-request-id", uuid.NewString())
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%s %s → %d: %s", method, path, res.StatusCode, truncate(string(b), 200))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
