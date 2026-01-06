package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestWebUI(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	ts := newTestServer(t)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx); err != nil {
		t.Fatalf("chromedp failed to start: %v", err)
	}

	var terminalText string
	var alerts []string
	var loginDisplay string
	var terminalDisplay string
	var loginDisplayAfterLogout string
	var terminalDisplayAfterLogout string
	var loginDisplayAfterRelogin string
	var promptPinned bool
	var activeElementID string
	var rotateModalVisible string
	var rotateErrorText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.Evaluate(`window.__alerts = []; window.alert = function(msg) { window.__alerts.push(String(msg)); };`, nil),
		chromedp.WaitVisible(`#login-form`, chromedp.ByID),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('terminal-panel')).display`, &terminalDisplay),
		chromedp.SetValue(`#login-username`, ts.user, chromedp.ByID),
		chromedp.SetValue(`#login-password`, ts.password, chromedp.ByID),
		chromedp.SetValue(`#login-totp`, currentTOTP(ts.totp), chromedp.ByID),
		chromedp.Click(`#login-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#terminal-panel`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForActiveElementID(ctx, "prompt-input", 5*time.Second)
		}),
		chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &activeElementID),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('login-panel')).display`, &loginDisplay),
		chromedp.Evaluate(`(() => {
			const panel = document.getElementById('terminal-panel');
			const prompt = document.getElementById('prompt-form');
			if (!panel || !prompt) return false;
			const panelRect = panel.getBoundingClientRect();
			const promptRect = prompt.getBoundingClientRect();
			const pad = parseFloat(getComputedStyle(panel).paddingBottom || '0');
			return Math.abs((panelRect.bottom - pad) - promptRect.bottom) <= 2;
		})()`, &promptPinned),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "no active tab", 5*time.Second)
		}),
		chromedp.SendKeys(`#prompt-input`, "/help\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "Commands", 5*time.Second)
		}),
		chromedp.SendKeys(`#prompt-input`, "/rotatesshkey\n", chromedp.ByID),
		chromedp.WaitVisible(`#rotatesshkey-modal`, chromedp.ByID),
		chromedp.SetValue(`#rotatesshkey-confirm`, "NO", chromedp.ByID),
		chromedp.Click(`#rotatesshkey-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('rotatesshkey-error').textContent`, &rotateErrorText),
		chromedp.SetValue(`#rotatesshkey-confirm`, "YES", chromedp.ByID),
		chromedp.Click(`#rotatesshkey-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "ssh key rotated", 5*time.Second)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForDisplay(ctx, "#rotatesshkey-modal", "none", 5*time.Second)
		}),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('rotatesshkey-modal')).display`, &rotateModalVisible),
		chromedp.SendKeys(`#prompt-input`, "/new demo\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "repo created: demo", 5*time.Second)
		}),
		chromedp.SendKeys(`#prompt-input`, "/new demo2\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "repo created: demo2", 5*time.Second)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForActiveTab(ctx, "demo2", 5*time.Second)
		}),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true, cancelable: true}));`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForActiveTab(ctx, "demo", 5*time.Second)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForActiveElementID(ctx, "prompt-input", 5*time.Second)
		}),
		chromedp.SendKeys(`#prompt-input`, "hello\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "mock response", 5*time.Second)
		}),
		chromedp.Text(`#terminal`, &terminalText, chromedp.ByID),
		chromedp.SendKeys(`#prompt-input`, "/quit\n", chromedp.ByID),
		chromedp.WaitVisible(`#login-form`, chromedp.ByID),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('terminal-panel')).display`, &terminalDisplayAfterLogout),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('login-panel')).display`, &loginDisplayAfterLogout),
		chromedp.SetValue(`#login-username`, ts.user, chromedp.ByID),
		chromedp.SetValue(`#login-password`, ts.password, chromedp.ByID),
		chromedp.SetValue(`#login-totp`, currentTOTP(ts.totp), chromedp.ByID),
		chromedp.Click(`#login-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#terminal-panel`, chromedp.ByID),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('login-panel')).display`, &loginDisplayAfterRelogin),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTabs(ctx, []string{"demo", "demo2"}, 5*time.Second)
		}),
		chromedp.Evaluate(`window.__alerts`, &alerts),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("chromedp timed out: %v", err)
		}
		t.Fatalf("chromedp failed: %v", err)
	}

	if terminalText == "" || !containsAll(terminalText, []string{"repo", "tab", "mock response"}) {
		t.Fatalf("unexpected terminal output: %s", terminalText)
	}
	if terminalDisplay != "none" {
		t.Fatalf("expected terminal panel hidden before login, got display=%q", terminalDisplay)
	}
	if loginDisplay != "none" {
		t.Fatalf("expected login panel hidden after login, got display=%q", loginDisplay)
	}
	if activeElementID != "prompt-input" {
		t.Fatalf("expected prompt input focused after login, got active=%q", activeElementID)
	}
	if terminalDisplayAfterLogout != "none" {
		t.Fatalf("expected terminal panel hidden after logout, got display=%q", terminalDisplayAfterLogout)
	}
	if loginDisplayAfterLogout == "none" {
		t.Fatalf("expected login panel visible after logout, got display=%q", loginDisplayAfterLogout)
	}
	if loginDisplayAfterRelogin != "none" {
		t.Fatalf("expected login panel hidden after re-login, got display=%q", loginDisplayAfterRelogin)
	}
	if !promptPinned {
		t.Fatalf("expected prompt pinned to bottom of terminal panel")
	}
	if !strings.Contains(rotateErrorText, "type YES") {
		t.Fatalf("expected rotatesshkey confirmation error, got %q", rotateErrorText)
	}
	if rotateModalVisible != "none" {
		t.Fatalf("expected rotate ssh key modal closed, got display=%q", rotateModalVisible)
	}
	if len(alerts) > 0 {
		t.Fatalf("unexpected alert(s): %v", alerts)
	}
}

func TestWebUILogoutKeepsRunAlive(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	runner := newBlockingRunner()
	ts := newTestServerWithRunner(t, runner)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	ctx, cancel := newChromedpContext(t)
	defer cancel()

	if err := chromedp.Run(ctx); err != nil {
		t.Fatalf("chromedp failed to start: %v", err)
	}

	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible(`#login-form`, chromedp.ByID),
		chromedp.SetValue(`#login-username`, ts.user, chromedp.ByID),
		chromedp.SetValue(`#login-password`, ts.password, chromedp.ByID),
		chromedp.SetValue(`#login-totp`, currentTOTP(ts.totp), chromedp.ByID),
		chromedp.Click(`#login-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#terminal-panel`, chromedp.ByID),
		chromedp.SendKeys(`#prompt-input`, "/new demo\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "repo created: demo", 5*time.Second)
		}),
		chromedp.SendKeys(`#prompt-input`, "hello\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			waitForGateReady(t, runner.runGate, 5*time.Second)
			return nil
		}),
		chromedp.SendKeys(`#prompt-input`, "/quit\n", chromedp.ByID),
		chromedp.WaitVisible(`#login-form`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			time.Sleep(200 * time.Millisecond)
			if err := runner.RunContextErr(); err != nil {
				return fmt.Errorf("run context canceled after /quit: %w", err)
			}
			runner.runGate.Release()
			return nil
		}),
		chromedp.SetValue(`#login-username`, ts.user, chromedp.ByID),
		chromedp.SetValue(`#login-password`, ts.password, chromedp.ByID),
		chromedp.SetValue(`#login-totp`, currentTOTP(ts.totp), chromedp.ByID),
		chromedp.Click(`#login-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#terminal-panel`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "blocking response:", 5*time.Second)
		}),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("chromedp timed out: %v", err)
		}
		t.Fatalf("chromedp failed: %v", err)
	}
}

func TestWebUILogoutKeepsCommandAlive(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	runner := newBlockingRunner()
	ts := newTestServerWithRunner(t, runner)

	server := httptest.NewServer(ts.httpSrv.Handler())
	t.Cleanup(server.Close)

	ctx, cancel := newChromedpContext(t)
	defer cancel()

	if err := chromedp.Run(ctx); err != nil {
		t.Fatalf("chromedp failed to start: %v", err)
	}

	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible(`#login-form`, chromedp.ByID),
		chromedp.SetValue(`#login-username`, ts.user, chromedp.ByID),
		chromedp.SetValue(`#login-password`, ts.password, chromedp.ByID),
		chromedp.SetValue(`#login-totp`, currentTOTP(ts.totp), chromedp.ByID),
		chromedp.Click(`#login-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#terminal-panel`, chromedp.ByID),
		chromedp.SendKeys(`#prompt-input`, "/new demo\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "repo created: demo", 5*time.Second)
		}),
		chromedp.SendKeys(`#prompt-input`, "! echo hello\n", chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			waitForGateReady(t, runner.cmdGate, 5*time.Second)
			return nil
		}),
		chromedp.SendKeys(`#prompt-input`, "/quit\n", chromedp.ByID),
		chromedp.WaitVisible(`#login-form`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			time.Sleep(200 * time.Millisecond)
			if err := runner.CommandContextErr(); err != nil {
				return fmt.Errorf("command context canceled after /quit: %w", err)
			}
			runner.cmdGate.Release()
			return nil
		}),
		chromedp.SetValue(`#login-username`, ts.user, chromedp.ByID),
		chromedp.SetValue(`#login-password`, ts.password, chromedp.ByID),
		chromedp.SetValue(`#login-totp`, currentTOTP(ts.totp), chromedp.ByID),
		chromedp.Click(`#login-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#terminal-panel`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForTerminal(ctx, "blocking command: echo hello", 5*time.Second)
		}),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("chromedp timed out: %v", err)
		}
		t.Fatalf("chromedp failed: %v", err)
	}
}

func newChromedpContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	ctx, timeoutCancel := context.WithTimeout(ctx, 30*time.Second)
	return ctx, func() {
		timeoutCancel()
		cancel()
		allocCancel()
	}
}

func waitForTerminal(ctx context.Context, needle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		var text string
		if err := chromedp.Text(`#terminal`, &text, chromedp.ByID).Do(ctx); err == nil {
			last = text
			if strings.Contains(text, needle) {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	var status string
	_ = chromedp.Evaluate(`(document.getElementById('status')||{}).textContent||''`, &status).Do(ctx)
	return fmt.Errorf("timeout waiting for terminal to include %q (last=%q status=%q)", needle, last, status)
}

func waitForActiveTab(ctx context.Context, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		var text string
		if err := chromedp.Evaluate(`(() => {
			const el = document.querySelector('.tab.active');
			return el ? el.textContent : '';
		})()`, &text).Do(ctx); err == nil {
			last = text
			if text == expected {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for active tab %q (last=%q)", expected, last)
}

func waitForDisplay(ctx context.Context, selector string, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		var display string
		script := fmt.Sprintf(`getComputedStyle(document.querySelector(%q)).display`, selector)
		if err := chromedp.Evaluate(script, &display).Do(ctx); err == nil {
			last = display
			if display == expected {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s display=%q (last=%q)", selector, expected, last)
}

func waitForTabs(ctx context.Context, expected []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last []string
	for time.Now().Before(deadline) {
		var names []string
		if err := chromedp.Evaluate(`Array.from(document.querySelectorAll('.tab')).map(el => el.textContent)`, &names).Do(ctx); err == nil {
			last = names
			if containsAllStrings(names, expected) {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for tabs %v (last=%v)", expected, last)
}

func waitForActiveElementID(ctx context.Context, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		var id string
		if err := chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &id).Do(ctx); err == nil {
			last = id
			if id == expected {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for active element %q (last=%q)", expected, last)
}

func containsAllStrings(values []string, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, value := range expected {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
