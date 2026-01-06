package sshserver

import (
	"context"
	"errors"
	"io"
	"net"

	gliderssh "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/eventbus"
	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// CommandHandler routes slash commands.
type CommandHandler interface {
	Handle(ctx context.Context, userID schema.UserID, tabID schema.TabID, input string) (bool, error)
}

// Server exposes centaurx over SSH.
type Server struct {
	Addr        string
	HostKeyPath string
	Listener    net.Listener
	Service     core.Service
	Handler     CommandHandler
	IdlePrompt  string
	AuthStore   LoginAuthStore
	EventBus    *eventbus.Bus
	logger      pslog.Logger
}

// LoginAuthStore validates SSH login credentials and supports password changes.
type LoginAuthStore interface {
	HasLoginPubKey(userID schema.UserID, key ssh.PublicKey) (bool, error)
	ValidateTOTP(username string, totpCode string) error
	ChangePassword(username, currentPassword, totpCode, newPassword string) error
}

type authContextKey string

const loginPubKeyOK authContextKey = "login-pubkey-ok"

// ListenAndServe starts the SSH server and shuts down on context cancellation.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.IdlePrompt == "" {
		s.IdlePrompt = "> "
	}
	if s.logger == nil {
		s.logger = pslog.Ctx(ctx)
	}

	signer, err := EnsureHostKey(s.HostKeyPath)
	if err != nil {
		return err
	}

	if s.AuthStore == nil {
		return errors.New("auth store is required for SSH")
	}

	server := &gliderssh.Server{
		Addr:                       s.Addr,
		Handler:                    s.handleSession,
		PublicKeyHandler:           s.handlePublicKey,
		KeyboardInteractiveHandler: s.handleKeyboardInteractive,
	}
	server.AddHostKey(signer)

	errCh := make(chan error, 1)
	go func() {
		if s.Listener != nil {
			errCh <- server.Serve(s.Listener)
			return
		}
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = server.Close()
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) handlePublicKey(ctx gliderssh.Context, key gliderssh.PublicKey) bool {
	log := s.logger
	if log == nil {
		log = pslog.Ctx(ctx)
	}
	fingerprint := ssh.FingerprintSHA256(key)
	remote := remoteAddr(ctx)
	userID := schema.UserID(ctx.User())
	sshSession := ctx.SessionID()
	if userID == "" {
		log.Warn("ssh pubkey rejected", "reason", "missing user", "remote", remote, "ssh_session", sshSession, "fingerprint", fingerprint)
		return false
	}
	log = log.With("user", userID, "remote", remote, "fingerprint", fingerprint)
	if sshSession != "" {
		log = log.With("ssh_session", sshSession)
	}
	ok, err := s.AuthStore.HasLoginPubKey(userID, key)
	if err != nil {
		log.Warn("ssh pubkey rejected", "err", err)
		return false
	}
	if !ok {
		log.Warn("ssh pubkey rejected", "reason", "no matching key")
		return false
	}
	ctx.SetValue(loginPubKeyOK, true)
	log.Info("ssh pubkey accepted")
	return false
}

func (s *Server) handleKeyboardInteractive(ctx gliderssh.Context, challenger ssh.KeyboardInteractiveChallenge) bool {
	if ctx.Value(loginPubKeyOK) != true {
		return false
	}
	log := s.logger
	if log == nil {
		log = pslog.Ctx(ctx)
	}
	remote := remoteAddr(ctx)
	userID := schema.UserID(ctx.User())
	sshSession := ctx.SessionID()
	if userID != "" {
		log = log.With("user", userID, "remote", remote)
		if sshSession != "" {
			log = log.With("ssh_session", sshSession)
		}
	}
	answers, err := challenger(ctx.User(), "", []string{"Verification code: "}, []bool{false})
	if err != nil {
		log.Warn("ssh totp rejected", "reason", "challenge failed", "err", err)
		return false
	}
	if len(answers) != 1 {
		log.Warn("ssh totp rejected", "reason", "invalid answer count", "count", len(answers))
		return false
	}
	if err := s.AuthStore.ValidateTOTP(ctx.User(), answers[0]); err != nil {
		log.Warn("ssh totp rejected", "reason", "invalid code", "err", err)
		return false
	}
	log.Info("ssh totp accepted")
	return true
}

func remoteAddr(ctx gliderssh.Context) string {
	if ctx == nil || ctx.RemoteAddr() == nil {
		return ""
	}
	return ctx.RemoteAddr().String()
}

func (s *Server) handleSession(sess gliderssh.Session) {
	log := s.logger
	if log == nil {
		log = pslog.Ctx(sess.Context())
	}
	userID := schema.UserID(sess.User())
	if userID == "" {
		log.Info("ssh session rejected", "reason", "missing user", "remote", sess.RemoteAddr().String())
		_, _ = io.WriteString(sess, "missing user\n")
		return
	}
	remote := sess.RemoteAddr().String()
	sshSession := sess.Context().SessionID()
	log = log.With("user", userID, "remote", remote)
	if sshSession != "" {
		log = log.With("ssh_session", sshSession)
	}
	ctx := logx.ContextWithUserLogger(sess.Context(), log, userID)

	pty, winCh, ok := sess.Pty()
	if !ok {
		log.Info("ssh session rejected", "reason", "pty required", "user", userID, "remote", sess.RemoteAddr().String())
		_, _ = io.WriteString(sess, "pty required\n")
		return
	}

	log.Info("ssh session opened", "term", pty.Term)
	var events <-chan eventbus.Event
	var unsubscribe func()
	if s.EventBus != nil {
		events, unsubscribe = s.EventBus.Subscribe(userID)
	}
	if unsubscribe != nil {
		defer unsubscribe()
	}
	ui := newTerminalSession(sess, s.Service, s.Handler, s.AuthStore, userID, s.IdlePrompt, events)
	ui.SetSize(pty.Window.Width, pty.Window.Height)
	_ = ui.Run(ctx, winCh)
	log.Info("ssh session closed", "term", pty.Term)
}
