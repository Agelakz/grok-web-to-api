package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Live-proven 2026-08-08 chat; image gen card stream 2026-08-09:
//   wss://grok.com/ws/mgw/?uid=<x-userid>
//   envelope: {session_id?, event:{type,event_id,...}}
//   session.create → session.created + conversation.attached
//   conversation.item.create + response.create → stream → response.done
//   image gen: enable_image_generation + response.grok.output.card_attachment.image_chunk

const assetsHost = "https://assets.grok.com/"

type chatOpts struct {
	fileIDs           []string
	enableImageGen    bool
	imageGenCount     int
}

func (c *Client) chatViaGateway(ctx context.Context, model, prompt string, opts chatOpts) (*Response, error) {
	uid := strings.TrimSpace(c.UserID())
	if uid == "" {
		uid = strings.TrimSpace(c.cookies.Get("x-userid"))
	}
	if uid == "" {
		return nil, fmt.Errorf("missing user id (session probe or x-userid cookie)")
	}
	if model == "" || model == "grok-3" {
		model = "auto"
	}
	imgCount := opts.imageGenCount
	if imgCount <= 0 {
		imgCount = 1
	}
	if imgCount > 4 {
		imgCount = 4 // ponytail: CDN default 2; hard ceiling until HAR shows higher
	}

	base, err := url.Parse(c.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	host := base.Host
	if host == "" {
		host = "grok.com"
	}
	wsURL := "wss://" + host + "/ws/mgw/?uid=" + url.QueryEscape(uid)

	hdr := http.Header{}
	hdr.Set("Cookie", c.cookies.Header())
	hdr.Set("Origin", c.cfg.BaseURL)
	hdr.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, wsURL, hdr)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ws dial %s → %d: %w", wsURL, resp.StatusCode, err)
		}
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	// image gen can take longer than text
	wait := 90 * time.Second
	if opts.enableImageGen {
		wait = 150 * time.Second
	}
	deadline := time.Now().Add(wait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)

	evtInit := "evt_init_" + uuid.NewString()
	sessionObj := map[string]any{
		"model": model,
		"x_grok": map[string]any{
			"protocol_capabilities":   []string{"conversation_attached", "custom_methods_v1"},
			"is_temporary":            true,
			"keep_context":            false,
			"enable_side_by_side":     false,
			"force_side_by_side":      false,
			"enable_image_generation": opts.enableImageGen,
			"image_generation_count":  imgCount,
			"disable_text_follow_ups": true,
			"disable_artifact":        true,
			"force_concise":           true,
			"disable_web_search":      true,
			"disable_x_search":        true,
		},
	}
	if err := c.wsSend(conn, map[string]any{
		"event": map[string]any{
			"type":     "session.create",
			"event_id": evtInit,
			"session":  sessionObj,
		},
	}); err != nil {
		return nil, err
	}

	var (
		sessionID   string
		attached    bool
		turnSent    bool
		doneParts   []string
		deltaParts  []string
		responseID  string
		errUpstream error
		// imageUuid → best snapshot
		images = map[string]*GeneratedImage{}
		order  []string
	)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if errUpstream != nil {
				return nil, errUpstream
			}
			return nil, fmt.Errorf("ws read: %w", err)
		}

		var env map[string]any
		if err := json.Unmarshal(data, &env); err != nil {
			c.log.Debug("ws non-json frame", zap.ByteString("raw", data[:min(len(data), 120)]))
			continue
		}
		ev, _ := env["event"].(map[string]any)
		if ev == nil {
			continue
		}
		typ, _ := ev["type"].(string)
		if typ == "pong" {
			continue
		}
		if typ == "error" {
			errUpstream = fmt.Errorf("gateway error: %v", ev["error"])
			if sessionID == "" {
				return nil, errUpstream
			}
			continue
		}

		if typ == "session.created" {
			if sid, _ := env["session_id"].(string); sid != "" {
				sessionID = sid
			}
			continue
		}
		if typ == "conversation.attached" {
			attached = true
			if sessionID == "" {
				if conv, _ := ev["conversation"].(map[string]any); conv != nil {
					if id, _ := conv["id"].(string); id != "" {
						sessionID = id
					}
				}
			}
		}

		if attached && sessionID != "" && !turnSent {
			turnSent = true
			mid := uuid.NewString()
			// CDN: mentions first, then text. Live probe 2026-08-09 vision OK.
			chunks := make([]map[string]any, 0, len(opts.fileIDs)+1)
			for _, fid := range opts.fileIDs {
				fid = strings.TrimSpace(fid)
				if fid == "" {
					continue
				}
				chunks = append(chunks, map[string]any{
					"mention": map[string]any{
						"fileMention": map[string]any{"fileId": fid},
					},
				})
			}
			chunks = append(chunks, map[string]any{"text": map[string]any{"text": prompt}})
			itemBody := map[string]any{
				"type": "message",
				"role": "user",
				"x_grok": map[string]any{
					"client_message_id": mid,
					"input_chunks":      chunks,
				},
			}
			item := map[string]any{
				"type":     "conversation.item.create",
				"event_id": "evt_item_" + uuid.NewString(),
				"item":     itemBody,
			}
			if len(opts.fileIDs) > 0 {
				ids := make([]string, 0, len(opts.fileIDs))
				for _, fid := range opts.fileIDs {
					if s := strings.TrimSpace(fid); s != "" {
						ids = append(ids, s)
					}
				}
				if len(ids) > 0 {
					itemBody["file_attachment_ids"] = ids
					item["file_attachment_ids"] = ids
				}
			}
			respCreate := map[string]any{
				"type":     "response.create",
				"event_id": "evt_resp_" + uuid.NewString(),
			}
			for _, e := range []map[string]any{item, respCreate} {
				if err := c.wsSend(conn, map[string]any{"session_id": sessionID, "event": e}); err != nil {
					return nil, err
				}
			}
		}

		switch typ {
		case "response.created":
			if r, _ := ev["response"].(map[string]any); r != nil {
				if id, _ := r["id"].(string); id != "" {
					responseID = id
				}
			}
		case "response.output_text.delta":
			if isThinkingEvent(ev) {
				continue
			}
			if d, _ := ev["delta"].(string); d != "" {
				deltaParts = append(deltaParts, d)
			}
		case "response.output_text.done":
			if t, _ := ev["text"].(string); t != "" {
				doneParts = append(doneParts, t)
			}
		case "response.grok.output":
			// live: output.card_attachment.image_chunk streams pixels + final key
			ingestCardAttachment(images, &order, ev)
		case "response.done":
			text := strings.Join(doneParts, "")
			if strings.TrimSpace(text) == "" {
				text = strings.Join(deltaParts, "")
			}
			// strip placeholder render tags for image-gen turns
			if opts.enableImageGen {
				text = stripGrokRender(text)
			}
			imgs := finalizeImages(images, order)
			if strings.TrimSpace(text) == "" && len(imgs) == 0 {
				if errUpstream != nil {
					return nil, errUpstream
				}
				return nil, fmt.Errorf("empty gateway response")
			}
			if responseID == "" {
				if r, _ := ev["response"].(map[string]any); r != nil {
					responseID, _ = r["id"].(string)
				}
			}
			return &Response{
				Text:           text,
				ConversationID: sessionID,
				ResponseID:     responseID,
				Images:         imgs,
			}, nil
		}
	}
}

func ingestCardAttachment(images map[string]*GeneratedImage, order *[]string, ev map[string]any) {
	out, _ := ev["output"].(map[string]any)
	if out == nil {
		return
	}
	ca, _ := out["card_attachment"].(map[string]any)
	if ca == nil {
		return
	}
	cardType, _ := ca["cardType"].(string)
	typ, _ := ca["type"].(string)
	if cardType != "generated_image_card" && typ != "render_generated_image" {
		return
	}
	ic, _ := ca["image_chunk"].(map[string]any)
	if ic == nil {
		return
	}
	uuidStr, _ := ic["imageUuid"].(string)
	if uuidStr == "" {
		if mid, _ := ic["mediaId"].(string); mid != "" {
			uuidStr = mid
		}
	}
	if uuidStr == "" {
		return
	}
	g, ok := images[uuidStr]
	if !ok {
		g = &GeneratedImage{UUID: uuidStr}
		images[uuidStr] = g
		*order = append(*order, uuidStr)
	}
	if m, _ := ic["imageModel"].(string); m != "" {
		g.Model = m
	}
	if prompt, _ := ic["imagePrompt"].(map[string]any); prompt != nil {
		if p, _ := prompt["prompt"].(string); p != "" {
			g.RevisedPrompt = p
		}
		if up, _ := prompt["upsampledPrompt"].(string); up != "" {
			// prefer upsampled as revised when present
			g.RevisedPrompt = up
		}
	}
	// card-level prompt before chunk fills in
	if g.RevisedPrompt == "" {
		if p, _ := ca["prompt"].(string); p != "" {
			g.RevisedPrompt = p
		}
	}
	urlStr, _ := ic["imageUrl"].(string)
	if urlStr == "" {
		return
	}
	if strings.HasPrefix(urlStr, "data:image") {
		// mid-stream inline; keep as b64 fallback
		if comma := strings.IndexByte(urlStr, ','); comma >= 0 {
			g.B64 = urlStr[comma+1:]
		}
		// only set URL to data if no final asset yet
		if g.URL == "" {
			g.URL = urlStr
		}
		return
	}
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		g.URL = urlStr
		return
	}
	// relative key users/<uid>/generated/<uuid>/image.jpg
	key := strings.TrimPrefix(urlStr, "/")
	g.URL = assetsHost + key
}

func finalizeImages(images map[string]*GeneratedImage, order []string) []GeneratedImage {
	out := make([]GeneratedImage, 0, len(order))
	for _, id := range order {
		g := images[id]
		if g == nil {
			continue
		}
		if g.URL == "" && g.B64 == "" {
			continue
		}
		out = append(out, *g)
	}
	return out
}

func stripGrokRender(s string) string {
	// remove <grok:render ...>...</grok:render> placeholders
	for {
		start := strings.Index(s, "<grok:render")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "</grok:render>")
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+len("</grok:render>"):]
	}
	return strings.TrimSpace(s)
}

func isThinkingEvent(ev map[string]any) bool {
	xg, _ := ev["x_grok"].(map[string]any)
	if xg == nil {
		return false
	}
	if b, ok := xg["is_thinking"].(bool); ok && b {
		return true
	}
	return false
}

func (c *Client) wsSend(conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
