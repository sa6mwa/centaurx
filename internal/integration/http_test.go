package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"pkt.systems/centaurx/httpapi"
	"pkt.systems/centaurx/schema"
)

func TestHTTPPromptFallback(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	ts := newTestServer(t)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	client := ts.login(t, server.URL)

	resp := writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/new demo",
	})
	readJSON(t, resp, &map[string]any{})

	resp = writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "stale-tab",
		"input":  "hello world",
	})
	readJSON(t, resp, &map[string]any{})

	tabResp := struct {
		Tabs      []struct{ ID string }
		ActiveTab string
	}{}
	var err error
	resp, err = client.Get(server.URL + "/api/tabs")
	if err != nil {
		t.Fatal(err)
	}
	readJSON(t, resp, &tabResp)
	if tabResp.ActiveTab == "" {
		raw, _ := json.Marshal(tabResp)
		t.Fatalf("expected active tab, got %s", string(raw))
	}

	bufferResp := struct {
		Buffer struct {
			Lines []string
		}
	}{}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err = client.Get(server.URL + "/api/buffer?tab_id=" + tabResp.ActiveTab + "&limit=200")
		if err != nil {
			t.Fatal(err)
		}
		readJSON(t, resp, &bufferResp)
		joined := strings.Join(bufferResp.Buffer.Lines, "\n")
		if strings.Contains(joined, "mock response: hello world") {
			waitForTabIdle(t, client, server.URL, 5*time.Second)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected mock response, got: %s", joined)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestHTTPStream(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	ts := newTestServer(t)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	client := ts.login(t, server.URL)

	streamReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResp, err := client.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = streamResp.Body.Close() })

	reader := bufio.NewReader(streamResp.Body)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event, err := readSSEvent(ctx, reader)
	if err != nil {
		t.Fatalf("snapshot read failed: %v", err)
	}
	if event.Type != "snapshot" {
		t.Fatalf("expected snapshot, got %s", event.Type)
	}

	resp := writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/new demo",
	})
	readJSON(t, resp, &map[string]any{})

	resp = writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "hello",
	})
	readJSON(t, resp, &map[string]any{})

	found := false
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		event, err := readSSEvent(ctx, reader)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				break
			}
			t.Fatalf("read event: %v", err)
		}
		if event.Type == "output" {
			for _, line := range event.Lines {
				if strings.Contains(line, "mock response: hello") {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("expected output event with mock response")
	}
	waitForTabIdle(t, client, server.URL, 5*time.Second)
}

func TestHTTPQuitLogsOut(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	ts := newTestServer(t)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	client := ts.login(t, server.URL)

	resp := writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/quit",
	})
	readJSON(t, resp, &map[string]any{})

	meResp, err := client.Get(server.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized after /quit, got %d", meResp.StatusCode)
	}
}

func TestHTTPQuitKeepsRunAlive(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	runner := newBlockingRunner()
	ts := newTestServerWithRunner(t, runner)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	client := ts.login(t, server.URL)
	resp := writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/new demo",
	})
	readJSON(t, resp, &map[string]any{})

	resp = writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "hello",
	})
	readJSON(t, resp, &map[string]any{})
	waitForGateReady(t, runner.runGate, 5*time.Second)

	resp = writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/quit",
	})
	readJSON(t, resp, &map[string]any{})

	time.Sleep(200 * time.Millisecond)
	if err := runner.RunContextErr(); err != nil {
		t.Fatalf("expected run context to remain active after /quit, got %v", err)
	}
	runner.runGate.Release()

	client, err := loginWithPassword(t, server.URL, ts.user, ts.password, ts.totp)
	if err != nil {
		t.Fatalf("re-login failed: %v", err)
	}
	tabID := waitForTabIDByName(t, client, server.URL, "demo", 5*time.Second)
	waitForBufferContains(t, client, server.URL, tabID, "blocking response: hello", 5*time.Second)
}

func TestHTTPQuitKeepsCommandAlive(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	runner := newBlockingRunner()
	ts := newTestServerWithRunner(t, runner)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	client := ts.login(t, server.URL)
	resp := writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/new demo",
	})
	readJSON(t, resp, &map[string]any{})

	resp = writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "! echo hello",
	})
	readJSON(t, resp, &map[string]any{})
	waitForGateReady(t, runner.cmdGate, 5*time.Second)

	resp = writeJSON(t, client, server.URL+"/api/prompt", map[string]string{
		"tab_id": "",
		"input":  "/quit",
	})
	readJSON(t, resp, &map[string]any{})

	time.Sleep(200 * time.Millisecond)
	if err := runner.CommandContextErr(); err != nil {
		t.Fatalf("expected command context to remain active after /quit, got %v", err)
	}
	runner.cmdGate.Release()

	client, err := loginWithPassword(t, server.URL, ts.user, ts.password, ts.totp)
	if err != nil {
		t.Fatalf("re-login failed: %v", err)
	}
	tabID := waitForTabIDByName(t, client, server.URL, "demo", 5*time.Second)
	waitForBufferContains(t, client, server.URL, tabID, "blocking command: echo hello", 5*time.Second)
}

func TestHTTPChangePassword(t *testing.T) {
	requireLong(t)
	ts := newTestServer(t)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	client := ts.login(t, server.URL)
	resp := writeJSON(t, client, server.URL+"/api/chpasswd", map[string]string{
		"current_password": ts.password,
		"totp":             mustTOTP(t, ts.totp),
		"new_password":     "new-password",
		"confirm_password": "new-password",
	})
	readJSON(t, resp, &map[string]any{})

	if _, err := loginWithPassword(t, server.URL, ts.user, ts.password, ts.totp); err == nil {
		t.Fatalf("expected old password login to fail")
	}
	if _, err := loginWithPassword(t, server.URL, ts.user, "new-password", ts.totp); err != nil {
		t.Fatalf("expected new password login to succeed: %v", err)
	}
}

func waitForTabIdle(t *testing.T, client *http.Client, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/tabs")
		if err != nil {
			t.Fatal(err)
		}
		payload := struct {
			Tabs []struct {
				Status string `json:"status"`
			} `json:"tabs"`
		}{}
		readJSON(t, resp, &payload)
		idle := true
		for _, tab := range payload.Tabs {
			if tab.Status == "running" {
				idle = false
				break
			}
		}
		if idle {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for tabs to become idle")
}

func readSSEvent(ctx context.Context, reader *bufio.Reader) (httpapi.StreamEvent, error) {
	var dataLines []string
	for {
		select {
		case <-ctx.Done():
			return httpapi.StreamEvent{}, ctx.Err()
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return httpapi.StreamEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) == 0 {
		return httpapi.StreamEvent{}, errors.New("no data in SSE event")
	}
	payload := strings.Join(dataLines, "\n")
	var event httpapi.StreamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return httpapi.StreamEvent{}, err
	}
	return event, nil
}

func loginWithPassword(t *testing.T, baseURL, username, password, totpSecret string) (*http.Client, error) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar}
	payload := map[string]string{
		"username": username,
		"password": password,
		"totp":     mustTOTP(t, totpSecret),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := client.Post(baseURL+"/api/login", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("login failed")
	}
	return client, nil
}

func mustTOTP(t *testing.T, secret string) string {
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		if t != nil {
			t.Fatalf("generate totp: %v", err)
		}
		return ""
	}
	return code
}

func waitForTabIDByName(t *testing.T, client *http.Client, baseURL, name string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/tabs")
		if err != nil {
			t.Fatal(err)
		}
		var tabs schema.ListTabsResponse
		readJSON(t, resp, &tabs)
		for _, tab := range tabs.Tabs {
			if string(tab.Name) == name {
				return string(tab.ID)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for tab %q", name)
	return ""
}

func waitForBufferContains(t *testing.T, client *http.Client, baseURL, tabID, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/buffer?tab_id=" + tabID + "&limit=200")
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Buffer struct {
				Lines []string `json:"lines"`
			} `json:"buffer"`
		}
		readJSON(t, resp, &payload)
		if strings.Contains(strings.Join(payload.Buffer.Lines, "\n"), needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for buffer %q", needle)
}
