package grok

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CookieStore holds grok.com web session cookies.
// Proven names from live export: sso, sso-rw, x-userid, cf_clearance, ...
type CookieStore struct {
	// Raw = full Cookie header (preferred if set).
	Raw string `json:"raw,omitempty"`
	// File = Netscape cookie file path (optional source).
	File string `json:"file,omitempty"`
	// Parsed name→value after load from file/raw.
	Pairs map[string]string `json:"-"`

	// Legacy X placeholders (kept for old .env; not used by grok.com).
	AuthToken string `json:"auth_token,omitempty"`
	CT0       string `json:"ct0,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	mu        sync.RWMutex
}

func (s *CookieStore) HasAuth() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hasSSOLocked() {
		return true
	}
	// legacy X pair
	return strings.TrimSpace(s.AuthToken) != "" && strings.TrimSpace(s.CT0) != ""
}

func (s *CookieStore) hasSSOLocked() bool {
	if v := strings.TrimSpace(s.Pairs["sso"]); v != "" {
		return true
	}
	if strings.Contains(s.Raw, "sso=") {
		return true
	}
	return false
}

// Get returns a single cookie value by name.
func (s *CookieStore) Get(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v := strings.TrimSpace(s.Pairs[name]); v != "" {
		return v
	}
	if raw := strings.TrimSpace(s.Raw); raw != "" {
		return strings.TrimSpace(parseCookieHeader(raw)[name])
	}
	return ""
}

// Header builds Cookie header for outbound grok.com requests.
func (s *CookieStore) Header() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if raw := strings.TrimSpace(s.Raw); raw != "" {
		return raw
	}
	if len(s.Pairs) > 0 {
		parts := make([]string, 0, len(s.Pairs))
		for k, v := range s.Pairs {
			if k == "" || v == "" {
				continue
			}
			parts = append(parts, k+"="+v)
		}
		return strings.Join(parts, "; ")
	}
	// legacy
	parts := make([]string, 0, 2)
	if s.AuthToken != "" {
		parts = append(parts, "auth_token="+s.AuthToken)
	}
	if s.CT0 != "" {
		parts = append(parts, "ct0="+s.CT0)
	}
	return strings.Join(parts, "; ")
}

// LoadSources fills pairs from Cookie file and/or Raw/legacy env.
func (s *CookieStore) LoadSources() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Pairs == nil {
		s.Pairs = map[string]string{}
	}

	if f := strings.TrimSpace(s.File); f != "" {
		pairs, err := parseNetscapeCookies(f)
		if err != nil {
			return fmt.Errorf("cookie file %s: %w", f, err)
		}
		for k, v := range pairs {
			s.Pairs[k] = v
		}
		// materialize Raw if empty so Header is stable
		if strings.TrimSpace(s.Raw) == "" {
			s.Raw = joinPairs(s.Pairs)
		}
		s.UpdatedAt = time.Now()
	}

	if raw := strings.TrimSpace(s.Raw); raw != "" {
		for k, v := range parseCookieHeader(raw) {
			// file values already present; raw overrides
			s.Pairs[k] = v
		}
	}

	// legacy env tokens → pairs only if no sso yet
	if s.Pairs["sso"] == "" && s.AuthToken != "" {
		s.Pairs["auth_token"] = s.AuthToken
	}
	if s.Pairs["ct0"] == "" && s.CT0 != "" {
		s.Pairs["ct0"] = s.CT0
	}
	return nil
}

func joinPairs(pairs map[string]string) string {
	parts := make([]string, 0, len(pairs))
	for k, v := range pairs {
		if k == "" || v == "" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func parseCookieHeader(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// parseNetscapeCookies reads a Mozilla/Netscape cookie file.
// Domain filter: grok.com only (ignore unrelated hosts if present).
func parseNetscapeCookies(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	// cookie values can be long
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// name\tvalue form sometimes; standard is 7 tab fields
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := fields[0]
		// path := fields[2]
		name := fields[5]
		value := fields[6]
		if name == "" {
			continue
		}
		d := strings.TrimPrefix(domain, ".")
		if d != "grok.com" && !strings.HasSuffix(d, ".grok.com") {
			continue
		}
		// later lines override earlier (same name on grok.com + .grok.com)
		out[name] = value
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no grok.com cookies in file")
	}
	return out, nil
}

func (s *CookieStore) cachePath() string {
	key := s.Raw
	if key == "" {
		key = s.File
	}
	if key == "" {
		key = s.AuthToken
	}
	if key == "" {
		key = "default"
	}
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(".cookies", name)
}

func (s *CookieStore) SaveCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdatedAt = time.Now()
	if err := os.MkdirAll(".cookies", 0o700); err != nil {
		return err
	}
	// don't dump Pairs map with secrets into a different shape; Raw is enough
	type wire struct {
		Raw       string    `json:"raw,omitempty"`
		File      string    `json:"file,omitempty"`
		AuthToken string    `json:"auth_token,omitempty"`
		CT0       string    `json:"ct0,omitempty"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	b, err := json.MarshalIndent(wire{
		Raw: s.Raw, File: s.File, AuthToken: s.AuthToken, CT0: s.CT0, UpdatedAt: s.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cachePath(), b, 0o600)
}

func (s *CookieStore) LoadCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.cachePath())
	if err != nil {
		return err
	}
	var cached struct {
		Raw       string    `json:"raw,omitempty"`
		File      string    `json:"file,omitempty"`
		AuthToken string    `json:"auth_token,omitempty"`
		CT0       string    `json:"ct0,omitempty"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(b, &cached); err != nil {
		return err
	}
	if s.AuthToken == "" {
		s.AuthToken = cached.AuthToken
	}
	if s.CT0 == "" {
		s.CT0 = cached.CT0
	}
	if s.Raw == "" {
		s.Raw = cached.Raw
	}
	if s.File == "" {
		s.File = cached.File
	}
	s.UpdatedAt = cached.UpdatedAt
	return nil
}

func (s *CookieStore) Update(authToken, ct0, raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if authToken != "" {
		s.AuthToken = authToken
	}
	if ct0 != "" {
		s.CT0 = ct0
	}
	if raw != "" {
		s.Raw = raw
		if s.Pairs == nil {
			s.Pairs = map[string]string{}
		}
		for k, v := range parseCookieHeader(raw) {
			s.Pairs[k] = v
		}
	}
	s.UpdatedAt = time.Now()
}

func (s *CookieStore) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("sso=%t file=%t raw=%t pairs=%d updated=%s",
		s.hasSSOLocked(), s.File != "", s.Raw != "", len(s.Pairs), s.UpdatedAt.Format(time.RFC3339))
}
