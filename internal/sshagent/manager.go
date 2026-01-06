package sshagent

import (
	"crypto"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh/agent"

	"pkt.systems/pslog"
)

// KeyProvider supplies private keys for users.
type KeyProvider interface {
	LoadPrivateKey(username string) (crypto.PrivateKey, error)
}

// Manager hosts per-user SSH agents backed by stored keys.
type Manager struct {
	provider KeyProvider
	dir      string
	mu       sync.Mutex
	agents   map[string]*agentHandle
	log      pslog.Logger
}

type agentHandle struct {
	socket   string
	listener net.Listener
	keyring  agent.Agent
}

const sessionBindExtension = "session-bind@openssh.com"

type sessionBindAgent struct {
	agent.ExtendedAgent
}

func (a sessionBindAgent) Extension(extensionType string, contents []byte) ([]byte, error) {
	if extensionType == sessionBindExtension {
		return nil, nil
	}
	return a.ExtendedAgent.Extension(extensionType, contents)
}

// NewManager constructs a Manager rooted at the agent directory.
func NewManager(provider KeyProvider, dir string) (*Manager, error) {
	return NewManagerWithLogger(provider, dir, nil)
}

// NewManagerWithLogger constructs a Manager rooted at the agent directory with logging.
func NewManagerWithLogger(provider KeyProvider, dir string, logger pslog.Logger) (*Manager, error) {
	if provider == nil {
		return nil, errors.New("ssh agent key provider is required")
	}
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("ssh agent directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if logger != nil {
		logger = logger.With("agent_dir", dir)
	}
	return &Manager{
		provider: provider,
		dir:      dir,
		agents:   make(map[string]*agentHandle),
		log:      logger,
	}, nil
}

// EnsureAgent returns a socket path for the user's agent, starting it if needed.
func (m *Manager) EnsureAgent(username string) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("username is required")
	}
	if m.log != nil {
		m.log.Debug("ssh agent ensure start", "user", username)
	}
	m.mu.Lock()
	if handle, ok := m.agents[username]; ok {
		if ok := probeSocket(handle.socket); ok {
			if err := refreshAgent(handle, m.provider, username); err != nil && m.log != nil {
				m.log.Warn("ssh agent refresh failed", "user", username, "err", err)
			}
			m.mu.Unlock()
			if m.log != nil {
				m.log.Debug("ssh agent ensure ok", "user", username, "socket", handle.socket)
			}
			return handle.socket, nil
		}
		_ = handle.listener.Close()
		delete(m.agents, username)
	}
	m.mu.Unlock()

	key, err := m.provider.LoadPrivateKey(username)
	if err != nil {
		if m.log != nil {
			m.log.Warn("ssh agent ensure failed", "user", username, "err", err)
		}
		return "", err
	}

	dir := filepath.Join(m.dir, username)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	socket := filepath.Join(dir, "agent.sock")
	_ = os.Remove(socket)

	listener, err := net.Listen("unix", socket)
	if err != nil {
		if m.log != nil {
			m.log.Warn("ssh agent ensure failed", "user", username, "err", err)
		}
		return "", err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		if m.log != nil {
			m.log.Warn("ssh agent ensure failed", "user", username, "err", err)
		}
		return "", err
	}

	keyring := agent.NewKeyring()
	extended, ok := keyring.(agent.ExtendedAgent)
	if !ok {
		_ = listener.Close()
		return "", errors.New("ssh agent does not support extensions")
	}
	wrapped := sessionBindAgent{ExtendedAgent: extended}
	if err := wrapped.Add(agent.AddedKey{PrivateKey: key, Comment: username}); err != nil {
		_ = listener.Close()
		if m.log != nil {
			m.log.Warn("ssh agent ensure failed", "user", username, "err", err)
		}
		return "", err
	}

	handle := &agentHandle{socket: socket, listener: listener, keyring: wrapped}
	m.mu.Lock()
	m.agents[username] = handle
	m.mu.Unlock()

	go serve(handle)
	if m.log != nil {
		m.log.Info("ssh agent ensure ok", "user", username, "socket", socket)
	}
	return socket, nil
}

// Close stops all agent listeners.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var lastErr error
	count := len(m.agents)
	for user, handle := range m.agents {
		if err := handle.listener.Close(); err != nil && lastErr == nil {
			lastErr = err
		}
		_ = os.Remove(handle.socket)
		delete(m.agents, user)
	}
	if m.log != nil {
		m.log.Info("ssh agent closed", "count", count)
	}
	return lastErr
}

func serve(handle *agentHandle) {
	for {
		conn, err := handle.listener.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			_ = agent.ServeAgent(handle.keyring, c)
			_ = c.Close()
		}(conn)
	}
}

func probeSocket(path string) bool {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func refreshAgent(handle *agentHandle, provider KeyProvider, username string) error {
	key, err := provider.LoadPrivateKey(username)
	if err != nil {
		return err
	}
	if err := handle.keyring.RemoveAll(); err != nil {
		return fmt.Errorf("remove all keys: %w", err)
	}
	if err := handle.keyring.Add(agent.AddedKey{PrivateKey: key, Comment: username}); err != nil {
		return fmt.Errorf("add key: %w", err)
	}
	return nil
}
