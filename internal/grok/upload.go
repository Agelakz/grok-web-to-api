package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// UploadFileResult is the live-proven direct upload response subset.
type UploadFileResult struct {
	FileMetadataID string
	FileName       string
	MimeType       string
	FileURI        string
	UploadID       string
}

type uploadDirectResp struct {
	UploadID     string `json:"uploadId"`
	TerminalErr  string `json:"terminalError"`
	FileMetadata *struct {
		FileMetadataID string `json:"fileMetadataId"`
		FileMimeType   string `json:"fileMimeType"`
		FileName       string `json:"fileName"`
		FileURI        string `json:"fileUri"`
	} `json:"fileMetadata"`
}

// UploadBytes posts multipart to /http/upload-file-v2/direct (CDN + live probe 2026-08-09).
// Image min size: server rejects tiny dims (1x1). Prefer >= ~64x64 for images.
func (c *Client) UploadBytes(ctx context.Context, name, mime string, data []byte) (*UploadFileResult, error) {
	if !c.cookies.HasAuth() {
		return nil, fmt.Errorf("%w: set GROK_COOKIE_FILE", ErrNotWired)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "upload.bin"
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = "application/octet-stream"
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filepath.Base(name))))
	h.Set("Content-Type", mime)
	pw, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := pw.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	url := c.cfg.BaseURL + "/http/upload-file-v2/direct"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Origin", c.cfg.BaseURL)
	req.Header.Set("Referer", c.cfg.BaseURL+"/")
	req.Header.Set("Cookie", c.cookies.Header())
	req.Header.Set("x-xai-request-id", uuid.NewString())

	cli := c.http
	if cli.Timeout > 0 && cli.Timeout < 90*time.Second {
		cli = &http.Client{Timeout: 90 * time.Second}
	}
	res, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("upload direct → %d: %s", res.StatusCode, truncate(string(b), 200))
	}
	var out uploadDirectResp
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode upload: %w", err)
	}
	if out.TerminalErr != "" {
		return nil, fmt.Errorf("upload rejected: %s", out.TerminalErr)
	}
	if out.FileMetadata == nil || out.FileMetadata.FileMetadataID == "" {
		return nil, fmt.Errorf("upload missing fileMetadataId: %s", truncate(string(b), 200))
	}
	return &UploadFileResult{
		FileMetadataID: out.FileMetadata.FileMetadataID,
		FileName:       out.FileMetadata.FileName,
		MimeType:       out.FileMetadata.FileMimeType,
		FileURI:        out.FileMetadata.FileURI,
		UploadID:       out.UploadID,
	}, nil
}

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
