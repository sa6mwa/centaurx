package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/internal/version"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

const defaultCommitModel schema.ModelID = "gpt-5.1-codex-mini"
const usageBarWidth = 10
const modelReasoningEffortUsage = "low|medium|high|xhigh"

// HandlerConfig configures slash command behavior.
type HandlerConfig struct {
	AllowedModels       []schema.ModelID
	CommitModel         schema.ModelID
	RepoRoot            string
	LoginPubKeyStore    LoginPubKeyStore
	GitKeyStore         GitKeyStore
	GitKeyRotator       GitKeyRotator
	DisableAuditLogging bool
}

// LoginPubKeyStore manages SSH login public keys per user.
type LoginPubKeyStore interface {
	AddLoginPubKey(userID schema.UserID, pubKey string) (int, error)
	ListLoginPubKeys(userID schema.UserID) ([]string, error)
	RemoveLoginPubKey(userID schema.UserID, index int) error
}

// GitKeyStore retrieves the user's git SSH public key.
type GitKeyStore interface {
	LoadPublicKey(username string) (string, error)
}

// GitKeyRotator rotates the user's git SSH key.
type GitKeyRotator interface {
	RotateKey(username, keyType string, bits int) (string, error)
}

// Handler routes slash commands to service operations.
type Handler struct {
	service core.Service
	runners core.RunnerProvider
	cfg     HandlerConfig

	usageMu    sync.Mutex
	usageCache map[schema.UserID]usageCacheEntry
	usageTTL   time.Duration
	now        func() time.Time
}

type usageCacheEntry struct {
	fetchedAt time.Time
	info      core.UsageInfo
	err       error
}

// NewHandler constructs a command handler.
func NewHandler(service core.Service, runners core.RunnerProvider, cfg HandlerConfig) *Handler {
	if cfg.CommitModel == "" {
		cfg.CommitModel = defaultCommitModel
	}
	return &Handler{
		service:    service,
		runners:    runners,
		cfg:        cfg,
		usageCache: make(map[schema.UserID]usageCacheEntry),
		usageTTL:   30 * time.Minute,
		now:        time.Now,
	}
}

// Handle inspects input and executes slash commands.
func (h *Handler) Handle(ctx context.Context, userID schema.UserID, tabID schema.TabID, input string) (bool, error) {
	if ctx == nil {
		return false, errors.New("missing context")
	}
	baseLog := logx.WithUserTab(ctx, userID, tabID)
	ctx = logx.ContextWithUserTabLogger(ctx, baseLog, userID, tabID)
	log := baseLog.With("input_len", len(input))
	trimmed := strings.TrimLeft(input, " \t")
	if strings.HasPrefix(trimmed, "!") {
		log.Info("command shell request")
		return true, h.handleShell(ctx, userID, tabID, trimmed)
	}
	cmd, ok := Parse(input)
	if !ok {
		return false, nil
	}
	if !h.cfg.DisableAuditLogging {
		log.Debug("audit command", "command_type", "slash", "command", strings.TrimSpace(input))
	}
	log = log.With("command", cmd.Name, "args", len(cmd.Args))
	log.Info("command slash request")
	switch cmd.Name {
	case "":
		log.Warn("command slash rejected", "reason", "empty")
		return true, fmt.Errorf("invalid command")
	case "new":
		return true, h.handleNew(ctx, userID, tabID, cmd)
	case "listrepos":
		return true, h.handleListRepos(ctx, userID, tabID)
	case "rm":
		return true, h.handleRemove(ctx, userID, tabID, cmd)
	case "close":
		return true, h.handleClose(ctx, userID, tabID, cmd)
	case "help":
		return true, h.handleHelp(ctx, userID, tabID)
	case "model":
		return true, h.handleModel(ctx, userID, tabID, cmd)
	case "stop", "z":
		return true, h.handleStop(ctx, userID, tabID)
	case "renew":
		return true, h.handleRenew(ctx, userID, tabID)
	case "git":
		return true, h.handleGit(ctx, userID, tabID, cmd)
	case "addloginpubkey":
		return true, h.handleAddLoginPubKey(ctx, userID, tabID, cmd)
	case "listloginpubkeys":
		return true, h.handleListLoginPubKeys(ctx, userID, tabID)
	case "rmloginpubkey":
		return true, h.handleRemoveLoginPubKey(ctx, userID, tabID, cmd)
	case "pubkey":
		return true, h.handlePubKey(ctx, userID, tabID)
	case "rotatesshkey":
		return true, h.handleRotateSSHKey(ctx, userID, tabID, cmd)
	case "theme":
		return true, h.handleTheme(ctx, userID, tabID, cmd)
	case "togglefullcommandoutput":
		return true, h.handleToggleFullCommandOutput(ctx, userID, tabID)
	case "status":
		return true, h.handleStatus(ctx, userID, tabID)
	case "version":
		return true, h.handleVersion(ctx, userID, tabID)
	default:
		log.Warn("command slash rejected", "reason", "unknown")
		return true, fmt.Errorf("unknown command: /%s", cmd.Name)
	}
}

func (h *Handler) handleNew(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("usage: /new <repo|git-url>")
	}
	repoArg := cmd.Args[0]
	log := logx.WithUserTab(ctx, userID, tabID).With("repo_arg", repoArg)
	if looksLikeGitURL(repoArg) {
		h.appendStatus(ctx, userID, "", fmt.Sprintf("cloning repo %s", repoArg))
	} else {
		h.appendStatus(ctx, userID, "", fmt.Sprintf("preparing repo %s", repoArg))
	}
	var resp schema.CreateTabResponse
	var err error
	isURL := looksLikeGitURL(repoArg)
	log = log.With("is_url", isURL)
	if isURL {
		resp, err = h.service.CreateTab(ctx, schema.CreateTabRequest{
			UserID:  userID,
			RepoURL: repoArg,
		})
		if err != nil {
			log.Warn("command new failed", "err", err)
			h.appendError(ctx, userID, "", err)
			return err
		}
	} else {
		repoName := schema.RepoName(repoArg)
		resp, err = h.service.CreateTab(ctx, schema.CreateTabRequest{
			UserID:     userID,
			RepoName:   repoName,
			CreateRepo: true,
		})
		if err != nil {
			if errors.Is(err, schema.ErrRepoExists) {
				resp, err = h.service.CreateTab(ctx, schema.CreateTabRequest{
					UserID:     userID,
					RepoName:   repoName,
					CreateRepo: false,
				})
			}
			if err != nil {
				log.Warn("command new failed", "err", err)
				h.appendError(ctx, userID, "", err)
				return err
			}
		}
	}
	_, err = h.service.ActivateTab(ctx, schema.ActivateTabRequest{
		UserID: userID,
		TabID:  resp.Tab.ID,
	})
	if err != nil {
		log.Warn("command new activate failed", "err", err)
		h.appendError(ctx, userID, "", err)
		return err
	}
	lines := []string{}
	if resp.RepoCreated {
		if isURL {
			lines = append(lines, fmt.Sprintf("repo cloned: %s", resp.Tab.Repo.Name))
		} else {
			lines = append(lines, fmt.Sprintf("repo created: %s", resp.Tab.Repo.Name))
		}
	} else {
		lines = append(lines, fmt.Sprintf("repo opened: %s", resp.Tab.Repo.Name))
	}
	lines = append(lines, fmt.Sprintf("tab opened: %s", resp.Tab.Name))
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  resp.Tab.ID,
		Lines:  lines,
	})
	log.Info("command new completed", "tab", resp.Tab.ID, "repo", resp.Tab.Repo.Name, "created", resp.RepoCreated)
	return nil
}

func looksLikeGitURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "git@") || strings.HasPrefix(trimmed, "ssh://") {
		return true
	}
	return strings.Contains(trimmed, "/")
}

func (h *Handler) handleListRepos(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	resp, err := h.service.ListRepos(ctx, schema.ListReposRequest{UserID: userID})
	if err != nil {
		log.Warn("command listrepos failed", "err", err)
		return err
	}
	lines := []string{schema.WorkedForMarker + "Repos"}
	if len(resp.Repos) == 0 {
		lines = append(lines, "no repos found")
	} else {
		for _, repo := range resp.Repos {
			lines = append(lines, fmt.Sprintf("- %s", repo.Name))
		}
	}
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
			UserID: userID,
			Lines:  lines,
		})
		return nil
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  tabID,
		Lines:  lines,
	})
	log.Info("command listrepos completed", "count", len(resp.Repos))
	return nil
}

func (h *Handler) handleRemove(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("usage: /rm <number_or_name>")
	}
	log := logx.WithUserTab(ctx, userID, tabID)
	listResp, err := h.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		log.Warn("command rm list failed", "err", err)
		return err
	}
	targetID, targetName, err := resolveTabRef(cmd.Args[0], listResp.Tabs)
	if err != nil {
		log.Warn("command rm resolve failed", "err", err)
		return err
	}
	_, err = h.service.CloseTab(ctx, schema.CloseTabRequest{
		UserID: userID,
		TabID:  targetID,
	})
	if err != nil {
		log.Warn("command rm failed", "err", err)
		return err
	}
	listResp, err = h.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		return err
	}
	if targetName != "" {
		if listResp.ActiveTab != "" {
			_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
				UserID: userID,
				TabID:  listResp.ActiveTab,
				Lines:  []string{fmt.Sprintf("tab closed: %s", targetName)},
			})
		} else {
			_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
				UserID: userID,
				Lines:  []string{fmt.Sprintf("tab closed: %s", targetName)},
			})
		}
	}
	log.Info("command rm completed", "tab", targetID, "name", targetName)
	return nil
}

func (h *Handler) handleClose(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	if len(cmd.Args) != 0 {
		return fmt.Errorf("usage: /close")
	}
	if tabID == "" {
		return errors.New("no active tab")
	}
	return h.closeTab(ctx, userID, tabID)
}

func (h *Handler) closeTab(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	listResp, err := h.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		log.Warn("command close list failed", "err", err)
		return err
	}
	targetName := nameForTab(tabID, listResp.Tabs)
	_, err = h.service.CloseTab(ctx, schema.CloseTabRequest{
		UserID: userID,
		TabID:  tabID,
	})
	if err != nil {
		log.Warn("command close failed", "err", err)
		return err
	}
	listResp, err = h.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		return err
	}
	if targetName != "" {
		if listResp.ActiveTab != "" {
			_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
				UserID: userID,
				TabID:  listResp.ActiveTab,
				Lines:  []string{fmt.Sprintf("tab closed: %s", targetName)},
			})
		} else {
			_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
				UserID: userID,
				Lines:  []string{fmt.Sprintf("tab closed: %s", targetName)},
			})
		}
	}
	log.Info("command close completed", "name", targetName)
	return nil
}

func (h *Handler) handleModel(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	if len(cmd.Args) < 1 || len(cmd.Args) > 2 {
		return fmt.Errorf("usage: /model <model> [reasoning] (available: %s; reasoning: %s)", strings.Join(formatModels(h.cfg.AllowedModels), ", "), modelReasoningEffortUsage)
	}
	log := logx.WithUserTab(ctx, userID, tabID)
	modelID, err := schema.NormalizeModelID(cmd.Args[0])
	if err != nil {
		log.Warn("command model failed", "err", err)
		return err
	}
	reasoningEffort := schema.ModelReasoningEffort("")
	if len(cmd.Args) == 2 {
		reasoningEffort, err = schema.NormalizeModelReasoningEffort(cmd.Args[1])
		if err != nil {
			log.Warn("command model failed", "err", err)
			return fmt.Errorf("usage: /model <model> [reasoning] (reasoning: %s)", modelReasoningEffortUsage)
		}
	}
	resp, err := h.service.SetModel(ctx, schema.SetModelRequest{
		UserID:               userID,
		TabID:                tabID,
		Model:                modelID,
		ModelReasoningEffort: reasoningEffort,
	})
	if err != nil {
		log.Warn("command model failed", "err", err)
		return err
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  tabID,
		Lines:  []string{fmt.Sprintf("model set to: %s", schema.FormatModelWithReasoning(resp.Tab.Model, resp.Tab.ModelReasoningEffort))},
	})
	log.Info("command model completed", "model", resp.Tab.Model)
	return nil
}

func (h *Handler) handleStop(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	_, err := h.service.StopSession(ctx, schema.StopSessionRequest{
		UserID: userID,
		TabID:  tabID,
	})
	if err != nil {
		log.Warn("command stop failed", "err", err)
		return err
	}
	log.Info("command stop completed")
	return err
}

func (h *Handler) handleRenew(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if tabID == "" {
		log.Warn("command renew rejected", "reason", "no active tab")
		return errors.New("no active tab")
	}
	_, err := h.service.RenewSession(ctx, schema.RenewSessionRequest{
		UserID: userID,
		TabID:  tabID,
	})
	if err != nil {
		log.Warn("command renew failed", "err", err)
		return err
	}
	h.appendLine(ctx, userID, tabID, "session renewed (next prompt starts a new session)")
	log.Info("command renew completed")
	return nil
}

func (h *Handler) handleHelp(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	lines := helpLines(h.cfg.AllowedModels)
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
			UserID: userID,
			Lines:  lines,
		})
		log.Info("command help completed")
		return nil
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  tabID,
		Lines:  lines,
	})
	log.Info("command help completed")
	return nil
}

func (h *Handler) handleAddLoginPubKey(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if h.cfg.LoginPubKeyStore == nil {
		log.Warn("command addloginpubkey rejected", "reason", "login pubkey store not configured")
		return errors.New("login pubkey store not configured")
	}
	if len(cmd.Args) == 0 {
		log.Warn("command addloginpubkey rejected", "reason", "missing pubkey")
		return fmt.Errorf("usage: /addloginpubkey <pubkey>")
	}
	pubKey := strings.TrimSpace(strings.Join(cmd.Args, " "))
	if pubKey == "" {
		log.Warn("command addloginpubkey rejected", "reason", "empty pubkey")
		return fmt.Errorf("usage: /addloginpubkey <pubkey>")
	}
	id, err := h.cfg.LoginPubKeyStore.AddLoginPubKey(userID, pubKey)
	if err != nil {
		log.Warn("command addloginpubkey failed", "err", err)
		return err
	}
	h.appendLine(ctx, userID, tabID, fmt.Sprintf("login pubkey added (id %d)", id))
	log.Info("command addloginpubkey completed", "id", id)
	return nil
}

func (h *Handler) handleListLoginPubKeys(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if h.cfg.LoginPubKeyStore == nil {
		log.Warn("command listloginpubkeys rejected", "reason", "login pubkey store not configured")
		return errors.New("login pubkey store not configured")
	}
	keys, err := h.cfg.LoginPubKeyStore.ListLoginPubKeys(userID)
	if err != nil {
		log.Warn("command listloginpubkeys failed", "err", err)
		return err
	}
	lines := []string{schema.WorkedForMarker + "Login pubkeys"}
	if len(keys) == 0 {
		lines = append(lines, "no login pubkeys")
	} else {
		for i, key := range keys {
			lines = append(lines, fmt.Sprintf("%d) %s", i+1, strings.TrimSpace(key)))
		}
	}
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{UserID: userID, Lines: lines})
		log.Info("command listloginpubkeys completed", "count", len(keys))
		return nil
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{UserID: userID, TabID: tabID, Lines: lines})
	log.Info("command listloginpubkeys completed", "count", len(keys))
	return nil
}

func (h *Handler) handleRemoveLoginPubKey(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if h.cfg.LoginPubKeyStore == nil {
		log.Warn("command rmloginpubkey rejected", "reason", "login pubkey store not configured")
		return errors.New("login pubkey store not configured")
	}
	if len(cmd.Args) < 1 {
		log.Warn("command rmloginpubkey rejected", "reason", "missing id")
		return fmt.Errorf("usage: /rmloginpubkey <id>")
	}
	id, err := strconv.Atoi(cmd.Args[0])
	if err != nil || id <= 0 {
		log.Warn("command rmloginpubkey rejected", "reason", "invalid id", "value", cmd.Args[0])
		return fmt.Errorf("invalid pubkey id")
	}
	if err := h.cfg.LoginPubKeyStore.RemoveLoginPubKey(userID, id); err != nil {
		log.Warn("command rmloginpubkey failed", "err", err)
		return err
	}
	h.appendLine(ctx, userID, tabID, fmt.Sprintf("login pubkey removed (id %d)", id))
	log.Info("command rmloginpubkey completed", "id", id)
	return nil
}

func (h *Handler) handlePubKey(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if h.cfg.GitKeyStore == nil {
		log.Warn("command pubkey rejected", "reason", "git key store not configured")
		return errors.New("git key store not configured")
	}
	key, err := h.cfg.GitKeyStore.LoadPublicKey(string(userID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Warn("command pubkey rejected", "reason", "missing git public key")
			return errors.New("no git public key found")
		}
		log.Warn("command pubkey failed", "err", err)
		return err
	}
	lines := []string{
		schema.WorkedForMarker + "Git public key",
		strings.TrimSpace(key),
	}
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{UserID: userID, Lines: lines})
		log.Info("command pubkey completed")
		return nil
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{UserID: userID, TabID: tabID, Lines: lines})
	log.Info("command pubkey completed")
	return nil
}

func (h *Handler) handleRotateSSHKey(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if len(cmd.Args) == 0 {
		log.Warn("command rotatesshkey rejected", "reason", "missing confirmation")
		return errors.New("confirmation required; run /rotatesshkey affirm")
	}
	if len(cmd.Args) > 1 || cmd.Args[0] != "affirm" {
		log.Warn("command rotatesshkey rejected", "reason", "invalid args")
		return fmt.Errorf("usage: /rotatesshkey [affirm]")
	}
	rotator := h.cfg.GitKeyRotator
	if rotator == nil {
		log.Warn("command rotatesshkey rejected", "reason", "git key store not configured")
		return errors.New("git key store not configured")
	}
	pubKey, err := rotator.RotateKey(string(userID), sshkeys.KeyTypeEd25519, 0)
	if err != nil {
		log.Warn("command rotatesshkey failed", "err", err)
		return err
	}
	lines := []string{"ssh key rotated", "git public key:", strings.TrimSpace(pubKey)}
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{UserID: userID, Lines: lines})
		log.Info("command rotatesshkey completed")
		return nil
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{UserID: userID, TabID: tabID, Lines: lines})
	log.Info("command rotatesshkey completed")
	return nil
}

func (h *Handler) handleTheme(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if len(cmd.Args) == 0 {
		current := "unknown"
		if resp, err := h.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID}); err == nil {
			if resp.Theme != "" {
				current = string(resp.Theme)
			}
		}
		h.appendLine(ctx, userID, tabID, "theme: "+current)
		h.appendLine(ctx, userID, tabID, "available themes: "+strings.Join(formatThemes(schema.AvailableThemes()), ", "))
		log.Info("command theme listed", "current", current)
		return nil
	}
	name, ok := schema.NormalizeThemeName(cmd.Args[0])
	if !ok {
		log.Warn("command theme rejected", "theme", cmd.Args[0])
		return fmt.Errorf("unknown theme %q (available: %s)", cmd.Args[0], strings.Join(formatThemes(schema.AvailableThemes()), ", "))
	}
	if _, err := h.service.SetTheme(ctx, schema.SetThemeRequest{UserID: userID, Theme: name}); err != nil {
		log.Warn("command theme failed", "err", err)
		return err
	}
	h.appendLine(ctx, userID, tabID, fmt.Sprintf("theme set to %s", name))
	log.Info("command theme updated", "theme", name)
	return nil
}

func (h *Handler) handleToggleFullCommandOutput(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	prefs := sessionprefs.FromContext(ctx)
	if prefs == nil {
		log.Warn("command output toggle rejected", "reason", "session preferences unavailable")
		return errors.New("session preferences unavailable")
	}
	prefs.FullCommandOutput = !prefs.FullCommandOutput
	mode := "terse"
	if prefs.FullCommandOutput {
		mode = "full"
	}
	h.appendLine(ctx, userID, tabID, "command output: "+mode)
	log.Info("command output toggled", "mode", mode)
	return nil
}

func (h *Handler) handleStatus(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	if tabID == "" {
		log.Warn("command status rejected", "reason", "no active tab")
		return errors.New("no active tab")
	}
	tab, err := h.lookupTab(ctx, userID, tabID)
	if err != nil {
		log.Warn("command status lookup failed", "err", err)
		return err
	}
	usageResp, err := h.service.GetTabUsage(ctx, schema.GetTabUsageRequest{UserID: userID, TabID: tabID})
	if err != nil {
		log.Warn("command status usage failed", "err", err)
		return err
	}
	tokensUsed := 0
	if usageResp.Usage != nil {
		tokensUsed = usageResp.Usage.InputTokens + usageResp.Usage.OutputTokens
	}

	model := schema.FormatModelWithReasoning(tab.Model, tab.ModelReasoningEffort)
	dir := h.resolveStatusDir(ctx, userID, tabID, tab)
	session := string(tab.SessionID)
	if strings.TrimSpace(session) == "" {
		session = "none"
	}

	usageInfo, usageOK, usageErr := h.lookupUsage(ctx, userID, tabID)
	labels := []string{"Model", "Directory", "Session", "Tokens used"}
	if usageOK && usageInfo.ChatGPT {
		labels = append(labels, "5h limit", "Week limit")
	}
	labelWidth := maxLabelWidth(labels)

	lines := []string{
		schema.WorkedForMarker + "Status",
		formatStatusLine("Model", model, labelWidth),
		formatStatusLine("Directory", dir, labelWidth),
		formatStatusLine("Session", session, labelWidth),
		formatStatusLine("Tokens used", formatTokensUsed(tokensUsed), labelWidth),
	}

	if usageOK && usageInfo.ChatGPT {
		now := h.now()
		lines = append(lines,
			formatStatusLine("5h limit", formatUsageWindow(usageInfo.Primary, usageErr, now), labelWidth),
			formatStatusLine("Week limit", formatUsageWindow(usageInfo.Secondary, usageErr, now), labelWidth),
		)
	}

	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  tabID,
		Lines:  lines,
	})
	log.Info("command status completed", "tokens_used", tokensUsed, "usage_ok", usageOK, "chatgpt", usageInfo.ChatGPT)
	return nil
}

func (h *Handler) handleVersion(ctx context.Context, userID schema.UserID, tabID schema.TabID) error {
	log := logx.WithUserTab(ctx, userID, tabID)
	versionLine := fmt.Sprintf("%s %s", version.Module(), version.Current())
	lines := []string{
		schema.WorkedForMarker + "About",
		schema.AboutVersionMarker + versionLine,
		schema.AboutCopyrightMarker + "Copyright (C) 2025-2026 Michel Blomgren",
		schema.AboutLinkMarker + "https://github.com/sa6mwa/centaurx",
		"",
	}
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
			UserID: userID,
			Lines:  lines,
		})
		log.Info("command version completed")
		return nil
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  tabID,
		Lines:  lines,
	})
	log.Info("command version completed")
	return nil
}

func (h *Handler) handleShell(ctx context.Context, userID schema.UserID, tabID schema.TabID, input string) error {
	baseLog := logx.WithUserTab(ctx, userID, tabID)
	ctx = logx.ContextWithUserTabLogger(ctx, baseLog, userID, tabID)
	log := baseLog
	if h.runners == nil {
		log.Warn("command shell rejected", "reason", "runner not configured")
		return errors.New("runner not configured")
	}
	cmdText := strings.TrimSpace(strings.TrimPrefix(input, "!"))
	if cmdText == "" {
		log.Warn("command shell rejected", "reason", "empty command")
		return fmt.Errorf("usage: ! <cmd>")
	}
	log = log.With("command_len", len(cmdText))
	displayTabID := tabID
	runnerTabID := tabID
	if runnerTabID == "" {
		runnerTabID = schema.TabID("system-shell")
	}
	var tab schema.TabSnapshot
	if displayTabID != "" {
		loaded, err := h.lookupTab(ctx, userID, displayTabID)
		if err != nil {
			log.Warn("command shell lookup failed", "err", err)
			h.appendError(ctx, userID, displayTabID, err)
			return err
		}
		tab = loaded
		sessionLog := logx.WithSession(baseLog, tab.SessionID)
		ctx = logx.ContextWithUserTabLogger(ctx, sessionLog, userID, displayTabID)
		log = logx.WithRepo(sessionLog, core.RepoRefForUser(h.cfg.RepoRoot, userID, tab.Repo.Name)).With("command_len", len(cmdText))
	}
	runCtx, runCancel := detachCommandContext(ctx)
	runnerResp, err := h.runners.RunnerFor(runCtx, core.RunnerRequest{UserID: userID, TabID: runnerTabID})
	if err != nil {
		log.Warn("command shell runner failed", "err", err)
		h.appendError(ctx, userID, displayTabID, err)
		if runCancel != nil {
			runCancel()
		}
		return err
	}
	runner := runnerResp.Runner
	info := runnerResp.Info
	workingDir := info.HomeDir
	if displayTabID != "" {
		workingDir, err = core.RepoPath(h.cfg.RepoRoot, userID, tab.Repo.Name)
		if err != nil {
			log.Warn("command shell repo path failed", "err", err)
			h.appendError(ctx, userID, displayTabID, err)
			return err
		}
		if info.RepoRoot != "" && h.cfg.RepoRoot != "" {
			mapped, err := core.MapRepoPath(h.cfg.RepoRoot, info.RepoRoot, workingDir)
			if err != nil {
				log.Warn("command shell repo map failed", "err", err)
				h.appendError(ctx, userID, displayTabID, err)
				return err
			}
			workingDir = mapped
		}
	} else if strings.TrimSpace(workingDir) == "" {
		workingDir = "/centaurx"
	}
	tracker, _ := h.service.(core.CommandTracker)
	if !h.cfg.DisableAuditLogging {
		log.Debug("audit command", "command_type", "shell", "command", cmdText, "workdir", workingDir)
	}
	started := time.Now()
	handle, err := runner.RunCommand(runCtx, core.RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     cmdText,
		UseShell:    true,
		SSHAuthSock: info.SSHAuthSock,
	})
	if err != nil {
		log.Warn("command shell start failed", "err", err)
		h.appendError(ctx, userID, displayTabID, err)
		if runCancel != nil {
			runCancel()
		}
		return err
	}
	h.appendLine(ctx, userID, displayTabID, "$ "+cmdText)
	log.Trace("command shell started", "workdir", workingDir)
	if tracker != nil && displayTabID != "" {
		tracker.RegisterCommand(runCtx, userID, displayTabID, handle, runCancel)
	}
	go h.streamCommandOutput(runCtx, userID, displayTabID, handle, started, tracker, runCancel)
	return nil
}

func (h *Handler) streamCommandOutput(ctx context.Context, userID schema.UserID, tabID schema.TabID, handle core.CommandHandle, started time.Time, tracker core.CommandTracker, cancel context.CancelFunc) {
	log := logx.WithUserTab(ctx, userID, tabID)
	defer func() {
		if tracker != nil && tabID != "" {
			tracker.UnregisterCommand(userID, tabID, handle)
		}
		if cancel != nil {
			cancel()
		}
		_ = handle.Close()
	}()
	stream := handle.Outputs()
	for {
		output, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Warn("command stream error", "err", err)
			h.appendLine(ctx, userID, tabID, fmt.Sprintf("command error: %v", err))
			break
		}
		line := output.Text
		if output.Stream == core.CommandStreamStderr {
			line = schema.StderrMarker + line
		}
		h.appendLine(ctx, userID, tabID, line)
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		log.Warn("command wait failed", "err", err)
		h.appendLine(ctx, userID, tabID, fmt.Sprintf("command failed: %v", err))
		return
	}
	h.appendLine(ctx, userID, tabID, formatCommandFinishedLine(time.Since(started), result.ExitCode))
	log.Trace("command completed", "exit_code", result.ExitCode, "duration_ms", time.Since(started).Milliseconds())
}

func (h *Handler) handleGit(ctx context.Context, userID schema.UserID, tabID schema.TabID, cmd Command) error {
	if len(cmd.Args) == 0 {
		return fmt.Errorf("usage: /git commit [message]")
	}
	sub := strings.ToLower(cmd.Args[0])
	if sub != "commit" {
		return fmt.Errorf("unsupported /git subcommand: %s", sub)
	}
	if h.runners == nil {
		return errors.New("runner not configured")
	}
	baseLog := logx.WithUserTab(ctx, userID, tabID).With("subcommand", sub)
	ctx = logx.ContextWithUserTabLogger(ctx, baseLog, userID, tabID)
	log := baseLog

	tab, err := h.lookupTab(ctx, userID, tabID)
	if err != nil {
		log.Warn("command git lookup failed", "err", err)
		h.appendError(ctx, userID, tabID, err)
		return err
	}
	if tab.Status == schema.TabStatusRunning {
		log.Warn("command git rejected", "err", schema.ErrTabBusy)
		h.appendError(ctx, userID, tabID, schema.ErrTabBusy)
		return schema.ErrTabBusy
	}
	sessionLog := logx.WithSession(baseLog, tab.SessionID)
	ctx = logx.ContextWithUserTabLogger(ctx, sessionLog, userID, tabID)
	log = logx.WithRepo(sessionLog, core.RepoRefForUser(h.cfg.RepoRoot, userID, tab.Repo.Name)).With("subcommand", sub)

	message := remainderAfterTokens(cmd.Raw, 2)

	if strings.TrimSpace(message) == "" {
		h.appendStatus(ctx, userID, tabID, "generating commit message")
		generated, err := h.generateCommitMessage(ctx, userID, tab, h.cfg.CommitModel)
		if err != nil {
			log.Warn("command git message failed", "err", err)
			h.appendError(ctx, userID, tabID, err)
			return err
		}
		message = generated
	}

	runnerResp, err := h.runners.RunnerFor(ctx, core.RunnerRequest{UserID: userID, TabID: tabID})
	if err != nil {
		log.Warn("command git runner failed", "err", err)
		h.appendError(ctx, userID, tabID, err)
		return err
	}
	runner := runnerResp.Runner
	info := runnerResp.Info
	workingDir, err := core.RepoPath(h.cfg.RepoRoot, userID, tab.Repo.Name)
	if err != nil {
		log.Warn("command git repo path failed", "err", err)
		h.appendError(ctx, userID, tabID, err)
		return err
	}
	if info.RepoRoot != "" && h.cfg.RepoRoot != "" {
		mapped, err := core.MapRepoPath(h.cfg.RepoRoot, info.RepoRoot, workingDir)
		if err != nil {
			log.Warn("command git repo map failed", "err", err)
			h.appendError(ctx, userID, tabID, err)
			return err
		}
		workingDir = mapped
	}

	h.appendStatus(ctx, userID, tabID, "running git add")
	if _, err := h.runCommandAndCapture(ctx, runner, core.RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     "git add -A",
		UseShell:    false,
		SSHAuthSock: info.SSHAuthSock,
	}); err != nil {
		log.Warn("command git add failed", "err", err)
		h.appendError(ctx, userID, tabID, err)
		return err
	}

	h.appendStatus(ctx, userID, tabID, "committing changes")
	output, err := h.runCommandAndCapture(ctx, runner, core.RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     fmt.Sprintf("git commit -m %s", shellQuote(message)),
		UseShell:    true,
		SSHAuthSock: info.SSHAuthSock,
	})
	if err != nil {
		log.Warn("command git commit failed", "err", err)
		h.appendError(ctx, userID, tabID, err)
		return err
	}

	lines := []string{"git commit completed"}
	if strings.TrimSpace(output) != "" {
		lines = append(lines, strings.Split(strings.TrimRight(output, "\n"), "\n")...)
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{
		UserID: userID,
		TabID:  tabID,
		Lines:  lines,
	})
	log.Info("command git commit completed")
	return nil
}

func (h *Handler) lookupTab(ctx context.Context, userID schema.UserID, tabID schema.TabID) (schema.TabSnapshot, error) {
	resp, err := h.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		return schema.TabSnapshot{}, err
	}
	for _, tab := range resp.Tabs {
		if tab.ID == tabID {
			return tab, nil
		}
	}
	return schema.TabSnapshot{}, schema.ErrTabNotFound
}

func (h *Handler) generateCommitMessage(ctx context.Context, userID schema.UserID, tab schema.TabSnapshot, modelID schema.ModelID) (string, error) {
	log := logx.WithUserTab(ctx, userID, tab.ID).With("model", modelID)
	ctx = logx.ContextWithUserTabLogger(ctx, log, userID, tab.ID)
	runnerResp, err := h.runners.RunnerFor(ctx, core.RunnerRequest{UserID: userID, TabID: tab.ID})
	if err != nil {
		log.Warn("command git message runner failed", "err", err)
		return "", err
	}
	runner := runnerResp.Runner
	info := runnerResp.Info
	prompt := "Give me a commit message according to conventionalcommits for the uncommitted changes in this repo, answer only with a single line."
	workingDir, err := core.RepoPath(h.cfg.RepoRoot, userID, tab.Repo.Name)
	if err != nil {
		log.Warn("command git message repo path failed", "err", err)
		return "", err
	}
	if info.RepoRoot != "" && h.cfg.RepoRoot != "" {
		mapped, err := core.MapRepoPath(h.cfg.RepoRoot, info.RepoRoot, workingDir)
		if err != nil {
			log.Warn("command git message repo map failed", "err", err)
			return "", err
		}
		workingDir = mapped
	}
	log = logx.WithRepo(log, core.RepoRefForUser(h.cfg.RepoRoot, userID, tab.Repo.Name))
	log = logx.WithSession(log, tab.SessionID)
	ctx = logx.ContextWithUserTabLogger(ctx, log, userID, tab.ID)
	if !h.cfg.DisableAuditLogging {
		command := "codex exec --json"
		if tab.SessionID != "" {
			command = fmt.Sprintf("codex exec resume %s --json", tab.SessionID)
		}
		log.Debug("audit command", "command_type", "codex", "command", command, "workdir", workingDir)
	}
	runReq := core.RunRequest{
		WorkingDir:           workingDir,
		Prompt:               prompt,
		Model:                modelID,
		ModelReasoningEffort: tab.ModelReasoningEffort,
		ResumeSessionID:      tab.SessionID,
		JSON:                 true,
		SSHAuthSock:          info.SSHAuthSock,
	}
	handle, err := runner.Run(ctx, runReq)
	if err != nil {
		log.Warn("command git message start failed", "err", err)
		return "", err
	}
	stream := handle.Events()
	var message string
	for {
		event, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Warn("command git message canceled", "err", err)
				return "", err
			}
			break
		}
		if event.Item != nil && event.Item.Type == schema.ItemAgentMessage && event.Item.Text != "" {
			message = event.Item.Text
		}
		if event.Type == schema.EventTurnFailed {
			if event.Error != nil && event.Error.Message != "" {
				log.Warn("command git message turn failed", "message", event.Error.Message)
				return "", fmt.Errorf("codex turn failed: %s", event.Error.Message)
			}
			log.Warn("command git message turn failed")
			return "", fmt.Errorf("codex turn failed")
		}
		if event.Type == schema.EventError {
			if event.Message != "" {
				log.Warn("command git message error", "message", event.Message)
				return "", fmt.Errorf("codex error: %s", event.Message)
			}
			log.Warn("command git message error")
			return "", fmt.Errorf("codex error")
		}
	}
	_, _ = handle.Wait(ctx)
	_ = handle.Close()

	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("no commit message produced")
	}
	if idx := strings.IndexByte(message, '\n'); idx > -1 {
		message = strings.TrimSpace(message[:idx])
	}
	log.Info("command git message generated")
	return message, nil
}

func helpLines(models []schema.ModelID) []string {
	modelList := strings.Join(formatModels(models), ", ")
	return []string{
		schema.WorkedForMarker + "Commands",
		schema.HelpMarker + "**/new** `<repo|git-url>` - create or open a repo (git URLs clone over SSH)",
		schema.HelpMarker + "**/listrepos** - list repos",
		schema.HelpMarker + "**/rm** `<number_or_name>` - close a tab",
		schema.HelpMarker + "**/close** - close current tab",
		schema.HelpMarker + "**/quit**, **/exit**, **/logout** - exit session / log out",
		schema.HelpMarker + "**/status** - show current session status",
		schema.HelpMarker + "**/model** `<model> [reasoning]` - set model for current tab (available: " + modelList + "; reasoning: " + modelReasoningEffortUsage + ")",
		schema.HelpMarker + "**/stop** or **/z** - stop running codex exec",
		schema.HelpMarker + "**/renew** - start a fresh codex session for the current tab",
		schema.HelpMarker + "**/chpasswd** - change your password",
		schema.HelpMarker + "**/codexauth** - upload codex auth.json",
		schema.HelpMarker + "**/git** `commit [message]` - commit changes",
		schema.HelpMarker + "**/addloginpubkey** `<pubkey>` - add an SSH login public key",
		schema.HelpMarker + "**/listloginpubkeys** - list SSH login public keys",
		schema.HelpMarker + "**/rmloginpubkey** `<id>` - remove SSH login public key by id",
		schema.HelpMarker + "**/pubkey** - show your git SSH public key",
		schema.HelpMarker + "**/rotatesshkey** `[affirm]` - rotate your git SSH key (affirm skips prompt)",
		schema.HelpMarker + "**/togglefullcommandoutput** - toggle full command output",
		schema.HelpMarker + "**/theme** `<name>` - set UI theme (available: " + strings.Join(formatThemes(schema.AvailableThemes()), ", ") + ")",
		schema.HelpMarker + "**/version** - show version information",
		schema.HelpMarker + "**!** `<cmd>` - run a shell command in the repo",
	}
}

func firstToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (h *Handler) runCommandAndCapture(ctx context.Context, runner core.Runner, req core.RunCommandRequest) (string, error) {
	log := pslog.Ctx(ctx).With("command_len", len(req.Command), "shell", req.UseShell)
	if name := firstToken(req.Command); name != "" {
		log = log.With("command", name)
	}
	if !h.cfg.DisableAuditLogging {
		log.Debug("audit command", "command_type", "runner", "command", req.Command, "workdir", req.WorkingDir)
	}
	log.Debug("command capture start")
	handle, err := runner.RunCommand(ctx, req)
	if err != nil {
		log.Warn("command capture failed", "err", err)
		return "", err
	}
	defer func() { _ = handle.Close() }()
	stream := handle.Outputs()
	lines := make([]string, 0, 16)
	for {
		output, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Warn("command capture stream failed", "err", err)
			return "", err
		}
		line := output.Text
		if output.Stream == core.CommandStreamStderr {
			line = schema.StderrMarker + line
		}
		lines = append(lines, line)
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		log.Warn("command capture wait failed", "err", err)
		return strings.Join(lines, "\n"), err
	}
	if result.ExitCode != 0 {
		log.Warn("command capture non-zero exit", "exit_code", result.ExitCode)
		return strings.Join(lines, "\n"), fmt.Errorf("command exited with code %d", result.ExitCode)
	}
	log.Debug("command capture finished", "exit_code", result.ExitCode, "lines", len(lines))
	return strings.Join(lines, "\n"), nil
}

func (h *Handler) lookupUsage(ctx context.Context, userID schema.UserID, tabID schema.TabID) (core.UsageInfo, bool, error) {
	if info, ok, err := h.cachedUsage(userID); ok {
		logx.WithUserTab(ctx, userID, tabID).Debug("usage cache hit", "err", err != nil, "chatgpt", info.ChatGPT)
		return info, true, err
	}
	if h.runners == nil {
		logx.WithUserTab(ctx, userID, tabID).Debug("usage lookup skipped", "reason", "runner unavailable")
		return core.UsageInfo{}, false, nil
	}
	runnerResp, err := h.runners.RunnerFor(ctx, core.RunnerRequest{UserID: userID, TabID: tabID})
	if err != nil {
		logx.WithUserTab(ctx, userID, tabID).Warn("usage runner lookup failed", "err", err)
		return core.UsageInfo{}, false, err
	}
	reader, ok := runnerResp.Runner.(core.UsageReader)
	if !ok {
		logx.WithUserTab(ctx, userID, tabID).Debug("usage reader missing")
		return core.UsageInfo{}, false, nil
	}
	info, err := reader.Usage(ctx)
	if h.usageTTL > 0 && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		h.storeUsage(userID, info, err)
	}
	logx.WithUserTab(ctx, userID, tabID).Info("usage lookup completed", "err", err != nil, "chatgpt", info.ChatGPT)
	return info, true, err
}

func (h *Handler) cachedUsage(userID schema.UserID) (core.UsageInfo, bool, error) {
	if h.usageTTL <= 0 {
		return core.UsageInfo{}, false, nil
	}
	h.usageMu.Lock()
	defer h.usageMu.Unlock()
	entry, ok := h.usageCache[userID]
	if !ok {
		return core.UsageInfo{}, false, nil
	}
	if h.now().Sub(entry.fetchedAt) > h.usageTTL {
		delete(h.usageCache, userID)
		return core.UsageInfo{}, false, nil
	}
	return entry.info, true, entry.err
}

func (h *Handler) storeUsage(userID schema.UserID, info core.UsageInfo, err error) {
	h.usageMu.Lock()
	defer h.usageMu.Unlock()
	h.usageCache[userID] = usageCacheEntry{
		fetchedAt: h.now(),
		info:      info,
		err:       err,
	}
}

func (h *Handler) resolveStatusDir(ctx context.Context, userID schema.UserID, tabID schema.TabID, tab schema.TabSnapshot) string {
	if h.runners != nil {
		if resp, err := h.runners.RunnerFor(ctx, core.RunnerRequest{UserID: userID, TabID: tabID}); err == nil {
			if strings.TrimSpace(h.cfg.RepoRoot) != "" && strings.TrimSpace(resp.Info.RepoRoot) != "" {
				if repoPath, err := core.RepoPath(h.cfg.RepoRoot, userID, tab.Repo.Name); err == nil {
					if mapped, err := core.MapRepoPath(h.cfg.RepoRoot, resp.Info.RepoRoot, repoPath); err == nil {
						return mapped
					}
				}
			}
		}
	}
	if repoPath, err := core.RepoPath(h.cfg.RepoRoot, userID, tab.Repo.Name); err == nil {
		return repoPath
	}
	if strings.TrimSpace(string(tab.Repo.Name)) != "" {
		return string(tab.Repo.Name)
	}
	return "unknown"
}

func (h *Handler) appendStatus(ctx context.Context, userID schema.UserID, tabID schema.TabID, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	h.appendLine(ctx, userID, tabID, "status: "+message)
}

func (h *Handler) appendError(ctx context.Context, userID schema.UserID, tabID schema.TabID, err error) {
	if err == nil {
		return
	}
	h.appendLine(ctx, userID, tabID, fmt.Sprintf("error: %v", err))
}

func (h *Handler) appendLine(ctx context.Context, userID schema.UserID, tabID schema.TabID, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if ctx == nil {
		return
	}
	if tabID == "" {
		_, _ = h.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{UserID: userID, Lines: []string{line}})
		return
	}
	_, _ = h.service.AppendOutput(ctx, schema.AppendOutputRequest{UserID: userID, TabID: tabID, Lines: []string{line}})
}

func formatCommandFinishedLine(duration time.Duration, exitCode int) string {
	return fmt.Sprintf("--- command finished in %s (exit %d) ---", formatDuration(duration), exitCode)
}

func formatDuration(duration time.Duration) string {
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration.Milliseconds())
	}
	seconds := duration.Seconds()
	if seconds < 10 {
		return fmt.Sprintf("%.2fs", seconds)
	}
	return fmt.Sprintf("%.1fs", seconds)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\'\''`) + "'"
}

func maxLabelWidth(labels []string) int {
	max := 0
	for _, label := range labels {
		if label == "" {
			continue
		}
		width := len(label) + 1
		if width > max {
			max = width
		}
	}
	return max
}

func formatStatusLine(label, value string, labelWidth int) string {
	if labelWidth <= 0 {
		labelWidth = len(label) + 1
	}
	if strings.TrimSpace(value) == "" {
		value = "unknown"
	}
	return fmt.Sprintf("%-*s %s", labelWidth, label+":", value)
}

func formatTokensUsed(tokens int) string {
	if tokens < 0 {
		tokens = 0
	}
	return fmt.Sprintf("%dK", tokens/1000)
}

func formatUsageWindow(window *core.UsageWindow, err error, now time.Time) string {
	if err != nil || window == nil {
		return "unavailable"
	}
	percent := percentRemaining(window.UsedPercent)
	bar := formatUsageBar(percent, usageBarWidth)
	reset := formatUsageReset(window.ResetAt, now)
	return fmt.Sprintf("%s %d%% / %s", bar, percent, reset)
}

func formatUsageBar(percent int, width int) string {
	if width <= 0 {
		return ""
	}
	filled := int(math.Round(float64(width) * float64(percent) / 100))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatUsageReset(resetAt int64, now time.Time) string {
	if resetAt <= 0 {
		return "reset unknown"
	}
	resetTime := time.Unix(resetAt, 0).Local()
	duration := formatStatusDuration(resetTime.Sub(now))
	return fmt.Sprintf("reset in %s @%s", duration, resetTime.Format("15:04 2 Jan"))
}

func formatStatusDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d%time.Minute != 0 {
		d = d.Truncate(time.Minute) + time.Minute
	} else {
		d = d.Truncate(time.Minute)
	}
	totalMinutes := int64(d / time.Minute)
	if totalMinutes == 0 {
		return "0m"
	}
	const minutesPerHour = 60
	const minutesPerDay = 24 * minutesPerHour
	days := totalMinutes / minutesPerDay
	hours := (totalMinutes / minutesPerHour) % 24
	minutes := totalMinutes % minutesPerHour
	if days > 0 {
		parts := make([]string, 0, 3)
		parts = append(parts, fmt.Sprintf("%dd", days))
		if hours > 0 {
			parts = append(parts, fmt.Sprintf("%dh", hours))
		}
		if minutes > 0 {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
		return strings.Join(parts, " ")
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}

func percentRemaining(used float64) int {
	return clampPercent(100 - used)
}

func clampPercent(value float64) int {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return int(math.Round(value))
}

func resolveTabRef(ref string, tabs []schema.TabSnapshot) (schema.TabID, string, error) {
	if idx, err := strconv.Atoi(ref); err == nil {
		if idx <= 0 || idx > len(tabs) {
			return "", "", fmt.Errorf("tab index out of range")
		}
		tab := tabs[idx-1]
		return tab.ID, string(tab.Name), nil
	}
	for _, tab := range tabs {
		if strings.EqualFold(string(tab.Name), ref) {
			return tab.ID, string(tab.Name), nil
		}
	}
	return "", "", fmt.Errorf("tab not found: %s", ref)
}

func nameForTab(tabID schema.TabID, tabs []schema.TabSnapshot) string {
	for _, tab := range tabs {
		if tab.ID == tabID {
			return string(tab.Name)
		}
	}
	return ""
}

func formatModels(models []schema.ModelID) []string {
	if len(models) == 0 {
		return nil
	}
	formatted := make([]string, 0, len(models))
	for _, modelID := range models {
		formatted = append(formatted, string(modelID))
	}
	return formatted
}

func formatThemes(themes []schema.ThemeName) []string {
	if len(themes) == 0 {
		return nil
	}
	formatted := make([]string, 0, len(themes))
	for _, name := range themes {
		formatted = append(formatted, string(name))
	}
	return formatted
}

func detachCommandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		if logger := pslog.Ctx(ctx); logger != nil {
			base = logx.CopyContextFields(pslog.ContextWithLogger(base, logger), ctx)
		}
		if prefs := sessionprefs.FromContext(ctx); prefs != nil {
			copyPrefs := *prefs
			base = sessionprefs.WithContext(base, &copyPrefs)
		}
	}
	return context.WithCancel(base)
}
