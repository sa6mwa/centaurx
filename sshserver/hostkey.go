package sshserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// EnsureHostKey ensures the SSH host key exists at path and returns the signer.
func EnsureHostKey(path string) (ssh.Signer, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("ssh host key path is required")
	}
	if _, err := os.Stat(path); err == nil {
		return loadHostKey(path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat host key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create host key dir: %w", err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, "centaurx")
	if err != nil {
		return nil, fmt.Errorf("marshal host key: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("write host key: %w", err)
	}
	if err := pem.Encode(file, block); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("encode host key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close host key: %w", err)
	}

	return ssh.NewSignerFromKey(priv)
}

func loadHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read host key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse host key: %w", err)
	}
	return signer, nil
}
