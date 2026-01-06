package sshkeys

import (
	"fmt"
	"os"
	"path/filepath"

	"pkt.systems/kryptograf/keymgmt"
	"pkt.systems/pslog"
)

// EnsureKeyStore creates or loads the key store at path and ensures a root key exists.
func EnsureKeyStore(path string) error {
	return EnsureKeyStoreWithLogger(path, nil)
}

// EnsureKeyStoreWithLogger creates or loads the key store with logging.
func EnsureKeyStoreWithLogger(path string, logger pslog.Logger) error {
	if path == "" {
		return fmt.Errorf("ssh key store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		if logger != nil {
			logger.Warn("ssh key store ensure failed", "err", err)
		}
		return err
	}
	store, err := keymgmt.LoadProto(path)
	if err != nil {
		if logger != nil {
			logger.Warn("ssh key store ensure failed", "err", err)
		}
		return err
	}
	if _, err := store.EnsureRootKey(); err != nil {
		if logger != nil {
			logger.Warn("ssh key store ensure failed", "err", err)
		}
		return err
	}
	if err := store.Commit(); err != nil {
		if logger != nil {
			logger.Warn("ssh key store ensure failed", "err", err)
		}
		return err
	}
	if logger != nil {
		logger.Info("ssh key store ensure ok", "path", path)
	}
	return nil
}
