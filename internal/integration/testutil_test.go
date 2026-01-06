package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/httpapi"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/auth"
	"pkt.systems/centaurx/internal/command"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/schema"
)

type mockRunner struct {
	counter uint64
}

func (r *mockRunner) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	threadID := req.ResumeSessionID
	if threadID == "" {
		id := atomic.AddUint64(&r.counter, 1)
		threadID = schema.SessionID(fmt.Sprintf("test-thread-%d", id))
	}
	events := []schema.ExecEvent{
		{Type: schema.EventThreadStarted, ThreadID: threadID},
		{
			Type: schema.EventItemCompleted,
			Item: &schema.ItemEvent{
				Type: schema.ItemAgentMessage,
				Text: fmt.Sprintf("mock response: %s", req.Prompt),
			},
		},
		{Type: schema.EventTurnCompleted},
	}
	return &mockHandle{events: events}, nil
}

func (r *mockRunner) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	outputs := []core.CommandOutput{
		{Stream: core.CommandStreamStdout, Text: fmt.Sprintf("mock command: %s", req.Command)},
	}
	return &mockCommandHandle{outputs: outputs}, nil
}

type mockHandle struct {
	events []schema.ExecEvent
}

func (h *mockHandle) Events() core.EventStream {
	return &mockStream{events: h.events}
}

func (h *mockHandle) Signal(ctx context.Context, sig core.ProcessSignal) error {
	_ = ctx
	_ = sig
	return nil
}

func (h *mockHandle) Wait(ctx context.Context) (core.RunResult, error) {
	_ = ctx
	return core.RunResult{ExitCode: 0}, nil
}

func (h *mockHandle) Close() error {
	return nil
}

type mockStream struct {
	events []schema.ExecEvent
	index  int
}

func (s *mockStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	if ctx.Err() != nil {
		return schema.ExecEvent{}, ctx.Err()
	}
	if s.index >= len(s.events) {
		return schema.ExecEvent{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *mockStream) Close() error {
	return nil
}

type mockCommandHandle struct {
	outputs []core.CommandOutput
}

func (h *mockCommandHandle) Outputs() core.CommandStream {
	return &mockCommandStream{outputs: h.outputs}
}

func (h *mockCommandHandle) Signal(ctx context.Context, sig core.ProcessSignal) error {
	_ = ctx
	_ = sig
	return nil
}

func (h *mockCommandHandle) Wait(ctx context.Context) (core.RunResult, error) {
	_ = ctx
	return core.RunResult{ExitCode: 0}, nil
}

func (h *mockCommandHandle) Close() error {
	return nil
}

type mockCommandStream struct {
	outputs []core.CommandOutput
	index   int
}

func (s *mockCommandStream) Next(ctx context.Context) (core.CommandOutput, error) {
	if ctx.Err() != nil {
		return core.CommandOutput{}, ctx.Err()
	}
	if s.index >= len(s.outputs) {
		return core.CommandOutput{}, io.EOF
	}
	output := s.outputs[s.index]
	s.index++
	return output, nil
}

func (s *mockCommandStream) Close() error {
	return nil
}

type blockingRunner struct {
	runGate *blockingGate
	cmdGate *blockingGate

	mu     sync.Mutex
	runCtx context.Context
	cmdCtx context.Context
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{
		runGate: newBlockingGate(),
		cmdGate: newBlockingGate(),
	}
}

func (r *blockingRunner) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	r.mu.Lock()
	r.runCtx = ctx
	r.mu.Unlock()
	r.runGate.markReady()
	threadID := req.ResumeSessionID
	if threadID == "" {
		threadID = "blocking-thread"
	}
	events := []schema.ExecEvent{
		{Type: schema.EventThreadStarted, ThreadID: threadID},
		{
			Type: schema.EventItemCompleted,
			Item: &schema.ItemEvent{
				Type: schema.ItemAgentMessage,
				Text: fmt.Sprintf("blocking response: %s", req.Prompt),
			},
		},
		{Type: schema.EventTurnCompleted},
	}
	return &blockingHandle{ctx: ctx, gate: r.runGate, events: events}, nil
}

func (r *blockingRunner) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	if !req.UseShell {
		return &mockCommandHandle{outputs: nil}, nil
	}
	r.mu.Lock()
	r.cmdCtx = ctx
	r.mu.Unlock()
	r.cmdGate.markReady()
	outputs := []core.CommandOutput{
		{Stream: core.CommandStreamStdout, Text: fmt.Sprintf("blocking command: %s", req.Command)},
	}
	return &blockingCommandHandle{ctx: ctx, gate: r.cmdGate, outputs: outputs}, nil
}

func (r *blockingRunner) RunContextErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runCtx == nil {
		return nil
	}
	return r.runCtx.Err()
}

func (r *blockingRunner) CommandContextErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmdCtx == nil {
		return nil
	}
	return r.cmdCtx.Err()
}

type blockingGate struct {
	readyOnce   sync.Once
	releaseOnce sync.Once
	ready       chan struct{}
	release     chan struct{}
}

func newBlockingGate() *blockingGate {
	return &blockingGate{
		ready:   make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *blockingGate) markReady() {
	g.readyOnce.Do(func() {
		close(g.ready)
	})
}

func (g *blockingGate) wait(ctx context.Context) error {
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *blockingGate) Release() {
	g.releaseOnce.Do(func() {
		close(g.release)
	})
}

func waitForGateReady(t *testing.T, gate *blockingGate, timeout time.Duration) {
	t.Helper()
	select {
	case <-gate.ready:
		return
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for blocking gate ready")
	}
}

type blockingHandle struct {
	ctx    context.Context
	gate   *blockingGate
	events []schema.ExecEvent
}

func (h *blockingHandle) Events() core.EventStream {
	return &blockingStream{ctx: h.ctx, gate: h.gate, events: h.events}
}

func (h *blockingHandle) Signal(context.Context, core.ProcessSignal) error {
	return nil
}

func (h *blockingHandle) Wait(ctx context.Context) (core.RunResult, error) {
	if err := h.gate.wait(ctx); err != nil {
		return core.RunResult{}, err
	}
	return core.RunResult{ExitCode: 0}, nil
}

func (h *blockingHandle) Close() error { return nil }

type blockingStream struct {
	ctx      context.Context
	gate     *blockingGate
	events   []schema.ExecEvent
	released bool
	index    int
}

func (s *blockingStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	if !s.released {
		if err := s.gate.wait(ctx); err != nil {
			return schema.ExecEvent{}, err
		}
		s.released = true
	}
	if s.index >= len(s.events) {
		return schema.ExecEvent{}, io.EOF
	}
	ev := s.events[s.index]
	s.index++
	return ev, nil
}

func (s *blockingStream) Close() error { return nil }

type blockingCommandHandle struct {
	ctx     context.Context
	gate    *blockingGate
	outputs []core.CommandOutput
}

func (h *blockingCommandHandle) Outputs() core.CommandStream {
	return &blockingCommandStream{ctx: h.ctx, gate: h.gate, outputs: h.outputs}
}

func (h *blockingCommandHandle) Signal(context.Context, core.ProcessSignal) error {
	return nil
}

func (h *blockingCommandHandle) Wait(ctx context.Context) (core.RunResult, error) {
	if err := h.gate.wait(ctx); err != nil {
		return core.RunResult{}, err
	}
	return core.RunResult{ExitCode: 0}, nil
}

func (h *blockingCommandHandle) Close() error { return nil }

type blockingCommandStream struct {
	ctx      context.Context
	gate     *blockingGate
	outputs  []core.CommandOutput
	released bool
	index    int
}

func (s *blockingCommandStream) Next(ctx context.Context) (core.CommandOutput, error) {
	if !s.released {
		if err := s.gate.wait(ctx); err != nil {
			return core.CommandOutput{}, err
		}
		s.released = true
	}
	if s.index >= len(s.outputs) {
		return core.CommandOutput{}, io.EOF
	}
	out := s.outputs[s.index]
	s.index++
	return out, nil
}

func (s *blockingCommandStream) Close() error { return nil }

type testServer struct {
	service   core.Service
	handler   *command.Handler
	httpSrv   *httpapi.Server
	authStore *auth.Store
	hub       *httpapi.Hub
	user      string
	password  string
	totp      string
}

func newTestServer(t *testing.T) *testServer {
	return newTestServerWithRunner(t, &mockRunner{})
}

func newTestServerWithRunner(t *testing.T, runner core.Runner) *testServer {
	t.Helper()
	repoRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	userFile := filepath.Join(t.TempDir(), "users.json")

	password := "test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := totp.Generate(totp.GenerateOpts{Issuer: "centaurx", AccountName: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	seed := appconfig.SeedUser{
		Username:     "tester",
		PasswordHash: string(hash),
		TOTPSecret:   secret.Secret(),
	}

	authStore, err := auth.NewStoreWithLogger(userFile, []appconfig.SeedUser{seed}, nil)
	if err != nil {
		t.Fatal(err)
	}

	hub := httpapi.NewHub(1000)
	service, err := core.NewService(schema.ServiceConfig{
		RepoRoot:      repoRoot,
		StateDir:      stateDir,
		DefaultModel:  "gpt-5.2-codex",
		AllowedModels: []schema.ModelID{"gpt-5.2-codex"},
		TabNameMax:    10,
		TabNameSuffix: "$",
	}, core.ServiceDeps{
		RunnerProvider: core.StaticRunnerProvider{Runner: runner},
		EventSink:      hub,
	})
	if err != nil {
		t.Fatal(err)
	}

	keyStore, err := sshkeys.NewStoreWithLogger(
		filepath.Join(t.TempDir(), "keys.bundle"),
		filepath.Join(t.TempDir(), "keys"),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := command.NewHandler(service, core.StaticRunnerProvider{Runner: runner}, command.HandlerConfig{
		AllowedModels: []schema.ModelID{"gpt-5.2-codex"},
		RepoRoot:      repoRoot,
		GitKeyStore:   keyStore,
		GitKeyRotator: keyStore,
	})

	httpSrv := httpapi.NewServer(httpapi.Config{
		Addr:               "127.0.0.1:0",
		SessionCookie:      "centaurx_session",
		SessionTTLHours:    1,
		InitialBufferLines: 200,
	}, service, handler, authStore, hub)

	server := &testServer{
		service:   service,
		handler:   handler,
		httpSrv:   httpSrv,
		authStore: authStore,
		hub:       hub,
		user:      seed.Username,
		password:  password,
		totp:      seed.TOTPSecret,
	}

	return server
}

func (ts *testServer) login(t *testing.T, baseURL string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	code, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{
		"username": ts.user,
		"password": ts.password,
		"totp":     code,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(baseURL+"/api/login", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed: %s", strings.TrimSpace(string(body)))
	}
	return client
}

func writeJSON(t *testing.T, client *http.Client, url string, payload any) *http.Response {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 300 {
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func ensureGitAvailable(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/git"); err == nil {
		return
	}
	if _, err := execLookPath("git"); err != nil {
		t.Fatalf("git not available")
	}
}

func requireLong(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

func execLookPath(binary string) (string, error) {
	return exec.LookPath(binary)
}

func currentTOTP(secret string) string {
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return ""
	}
	return code
}

func containsAll(value string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return true
}
