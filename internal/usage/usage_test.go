package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://chatgpt.com/", "https://chatgpt.com/backend-api"},
		{"https://chat.openai.com/backend-api/", "https://chat.openai.com/backend-api"},
		{"https://example.com/api/", "https://example.com/api"},
	}
	for _, tc := range tests {
		if got := normalizeBaseURL(tc.in); got != tc.want {
			t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadChatGPTBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`# comment
chatgpt_base_url = "https://chatgpt.com/backend-api/"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, found, err := readChatGPTBaseURL(path)
	if err != nil {
		t.Fatalf("readChatGPTBaseURL: %v", err)
	}
	if !found {
		t.Fatalf("expected base url found")
	}
	if got != "https://chatgpt.com/backend-api/" {
		t.Fatalf("unexpected base url: %q", got)
	}
}

func TestLoadAuth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")

	if err := os.WriteFile(path, []byte(`{"tokens":{}}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, chatgpt, err := loadAuth(path)
	if err != nil {
		t.Fatalf("loadAuth empty tokens: %v", err)
	}
	if chatgpt {
		t.Fatalf("expected non-chatgpt tokens")
	}

	data := []byte(`{"tokens":{"access_token":"tok","account_id":"acct"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	got, chatgpt, err := loadAuth(path)
	if err != nil {
		t.Fatalf("loadAuth: %v", err)
	}
	if !chatgpt {
		t.Fatalf("expected chatgpt tokens")
	}
	if got.AccessToken != "tok" || got.AccountID != "acct" {
		t.Fatalf("unexpected auth tokens: %+v", got)
	}
}
