package usage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/pslog"
)

const defaultBaseURL = "https://chatgpt.com/backend-api/"

type authFile struct {
	Tokens authTokens `json:"tokens"`
}

type authTokens struct {
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id"`
}

type rateLimitStatusPayload struct {
	RateLimit *rateLimitStatusDetails `json:"rate_limit"`
}

type rateLimitStatusDetails struct {
	PrimaryWindow   *rateLimitWindow `json:"primary_window"`
	SecondaryWindow *rateLimitWindow `json:"secondary_window"`
}

type rateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

// Fetch retrieves ChatGPT usage windows based on the codex auth.json file.
func Fetch(ctx context.Context) (core.UsageInfo, error) {
	log := pslog.Ctx(ctx)
	dotcodex := resolveDotCodexDir()
	if dotcodex == "" {
		log.Debug("usage fetch skipped", "reason", "dotcodex not found")
		return core.UsageInfo{}, nil
	}
	authPath := filepath.Join(dotcodex, "auth.json")
	configPath := filepath.Join(dotcodex, "config.toml")
	log.Debug("usage fetch start", "dotcodex", dotcodex)
	auth, chatgpt, err := loadAuth(authPath)
	info := core.UsageInfo{ChatGPT: chatgpt}
	if err != nil {
		log.Warn("usage auth load failed", "err", err)
		return info, err
	}
	if !chatgpt {
		log.Debug("usage fetch skipped", "reason", "non-chatgpt auth")
		return info, nil
	}
	baseURL, err := resolveBaseURL(configPath)
	if err != nil {
		log.Warn("usage base url failed", "err", err)
		return info, err
	}
	log.Debug("usage base url resolved", "base_url", baseURL)
	started := time.Now()
	payload, err := fetchRateLimits(ctx, baseURL, auth)
	if err != nil {
		log.Warn("usage fetch failed", "err", err, "duration_ms", time.Since(started).Milliseconds())
		return info, err
	}
	if payload.RateLimit != nil {
		info.Primary = toUsageWindow(payload.RateLimit.PrimaryWindow)
		info.Secondary = toUsageWindow(payload.RateLimit.SecondaryWindow)
	}
	log.Debug("usage fetch completed", "has_primary", info.Primary != nil, "has_secondary", info.Secondary != nil, "duration_ms", time.Since(started).Milliseconds())
	return info, nil
}

func resolveDotCodexDir() string {
	if value := strings.TrimSpace(os.Getenv("CENTAURX_DOTCODEX_DIR")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("CODEX_DOTCODEX_DIR")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func resolveBaseURL(configPath string) (string, error) {
	baseURL := ""
	if configPath != "" {
		if value, found, err := readChatGPTBaseURL(configPath); err != nil {
			return normalizeBaseURL(defaultBaseURL), fmt.Errorf("read chatgpt_base_url from %s: %w", configPath, err)
		} else if found {
			baseURL = value
		}
	}

	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return normalizeBaseURL(baseURL), nil
}

func readChatGPTBaseURL(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "chatgpt_base_url") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, "\"'")
		if value == "" {
			return "", false, nil
		}
		return value, true, nil
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func normalizeBaseURL(baseURL string) string {
	base := strings.TrimSpace(baseURL)
	for strings.HasSuffix(base, "/") {
		base = strings.TrimSuffix(base, "/")
	}
	if (strings.HasPrefix(base, "https://chatgpt.com") || strings.HasPrefix(base, "https://chat.openai.com")) &&
		!strings.Contains(base, "/backend-api") {
		base = base + "/backend-api"
	}
	return base
}

func loadAuth(path string) (authTokens, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return authTokens{}, false, nil
		}
		return authTokens{}, false, fmt.Errorf("read auth file %s: %w", path, err)
	}

	var parsed authFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return authTokens{}, false, fmt.Errorf("parse auth file %s: %w", path, err)
	}
	if strings.TrimSpace(parsed.Tokens.AccessToken) == "" || strings.TrimSpace(parsed.Tokens.AccountID) == "" {
		return authTokens{}, false, nil
	}
	return parsed.Tokens, true, nil
}

func fetchRateLimits(ctx context.Context, baseURL string, auth authTokens) (*rateLimitStatusPayload, error) {
	url := strings.TrimRight(baseURL, "/") + "/wham/usage"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	req.Header.Set("ChatGPT-Account-Id", auth.AccountID)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("request %s failed: %s; body=%s", url, resp.Status, strings.TrimSpace(string(body)))
	}

	var payload rateLimitStatusPayload
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &payload, nil
}

func toUsageWindow(window *rateLimitWindow) *core.UsageWindow {
	if window == nil {
		return nil
	}
	return &core.UsageWindow{
		UsedPercent:        window.UsedPercent,
		LimitWindowSeconds: window.LimitWindowSeconds,
		ResetAt:            window.ResetAt,
	}
}
