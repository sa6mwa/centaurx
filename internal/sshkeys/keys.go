package sshkeys

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"pkt.systems/kryptograf"
	"pkt.systems/kryptograf/keymgmt"
	"pkt.systems/pslog"
)

const (
	// KeyTypeEd25519 requests Ed25519 key generation.
	KeyTypeEd25519 = "ed25519"
	// KeyTypeRSA requests RSA key generation.
	KeyTypeRSA = "rsa"
	// DefaultRSABits is the default RSA key size in bits.
	DefaultRSABits   = 3072
	defaultKeyFile   = "key.enc"
	defaultPubFile   = "key.pub"
	descriptorPrefix = "centaurx:sshkey:"
)

// Store manages encrypted SSH key material for users.
type Store struct {
	storePath string
	keyDir    string
	log       pslog.Logger
}

// NewStore initializes the key store and ensures the root key exists.
func NewStore(storePath, keyDir string) (*Store, error) {
	return NewStoreWithLogger(storePath, keyDir, nil)
}

// NewStoreWithLogger initializes the key store with logging.
func NewStoreWithLogger(storePath, keyDir string, logger pslog.Logger) (*Store, error) {
	if strings.TrimSpace(storePath) == "" {
		return nil, fmt.Errorf("ssh key store path is required")
	}
	if strings.TrimSpace(keyDir) == "" {
		return nil, fmt.Errorf("ssh key directory is required")
	}
	if err := EnsureKeyStoreWithLogger(storePath, logger); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return nil, err
	}
	if logger != nil {
		logger = logger.With("ssh_key_store", storePath, "ssh_key_dir", keyDir)
	}
	return &Store{storePath: storePath, keyDir: keyDir, log: logger}, nil
}

// GenerateKey creates a new SSH key for the user.
func (s *Store) GenerateKey(username, keyType string, bits int) (string, error) {
	if s.log != nil {
		s.log.Info("ssh key generate start", "user", username, "type", keyType, "bits", bits)
	}
	return s.writeKey(username, keyType, bits, false)
}

// RotateKey replaces the SSH key for the user.
func (s *Store) RotateKey(username, keyType string, bits int) (string, error) {
	if s.log != nil {
		s.log.Info("ssh key rotate start", "user", username, "type", keyType, "bits", bits)
	}
	return s.writeKey(username, keyType, bits, true)
}

// EnsureKey ensures the user has an SSH key and returns the public key.
func (s *Store) EnsureKey(username, keyType string, bits int) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("username is required")
	}
	exists, err := s.keyExists(username)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key stat failed", "user", username, "err", err)
		}
		return "", err
	}
	if !exists {
		return s.GenerateKey(username, keyType, bits)
	}
	return s.LoadPublicKey(username)
}

// RemoveKey deletes stored SSH key material for the user.
func (s *Store) RemoveKey(username string) error {
	dir := s.userDir(username)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if s.log != nil {
			s.log.Warn("ssh key remove failed", "user", username, "err", err)
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		if s.log != nil {
			s.log.Warn("ssh key remove failed", "user", username, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("ssh key removed", "user", username)
	}
	return nil
}

// LoadSigner loads the user's private key as an ssh.Signer.
func (s *Store) LoadSigner(username string) (ssh.Signer, error) {
	priv, err := s.LoadPrivateKey(username)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key load signer failed", "user", username, "err", err)
		}
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// LoadPrivateKey decrypts and parses the user's private key.
func (s *Store) LoadPrivateKey(username string) (crypto.PrivateKey, error) {
	path := s.privateKeyPath(username)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if s.log != nil {
				s.log.Warn("ssh key load failed", "user", username, "err", err)
			}
			return nil, os.ErrNotExist
		}
		if s.log != nil {
			s.log.Warn("ssh key load failed", "user", username, "err", err)
		}
		return nil, err
	}
	material, root, err := s.materialForUser(username, false)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key load failed", "user", username, "err", err)
		}
		return nil, err
	}
	kg := kryptograf.New(root)
	file, err := os.Open(path)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key load failed", "user", username, "err", err)
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	reader, err := kg.DecryptReader(file, material)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key load failed", "user", username, "err", err)
		}
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	plain, err := io.ReadAll(reader)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key load failed", "user", username, "err", err)
		}
		return nil, err
	}
	priv, err := ssh.ParseRawPrivateKey(plain)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key load failed", "user", username, "err", err)
		}
		return nil, err
	}
	if s.log != nil {
		s.log.Debug("ssh key load ok", "user", username)
	}
	return priv, nil
}

// LoadPublicKey returns the stored public key data.
func (s *Store) LoadPublicKey(username string) (string, error) {
	path := s.publicKeyPath(username)
	data, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		if s.log != nil {
			s.log.Warn("ssh key public load failed", "user", username, "err", err)
		}
		return "", err
	}
	signer, err := s.LoadSigner(username)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

func (s *Store) writeKey(username, keyType string, bits int, rotate bool) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("username is required")
	}
	keyType = strings.ToLower(strings.TrimSpace(keyType))
	if keyType == "" {
		keyType = KeyTypeEd25519
	}
	var priv crypto.PrivateKey
	switch keyType {
	case KeyTypeEd25519:
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			if s.log != nil {
				s.log.Warn("ssh key write failed", "user", username, "err", err)
			}
			return "", err
		}
		priv = key
	case KeyTypeRSA:
		if bits == 0 {
			bits = DefaultRSABits
		}
		if bits < 2048 {
			return "", fmt.Errorf("rsa bits must be at least 2048")
		}
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			if s.log != nil {
				s.log.Warn("ssh key write failed", "user", username, "err", err)
			}
			return "", err
		}
		priv = key
	default:
		return "", fmt.Errorf("unsupported ssh key type %q", keyType)
	}

	block, err := ssh.MarshalPrivateKey(priv, username)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	plain := pem.EncodeToMemory(block)
	material, root, err := s.materialForUser(username, rotate)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	kg := kryptograf.New(root)

	dir := s.userDir(username)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "key-*.enc")
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	writer, err := kg.EncryptWriter(tmp, material)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	if _, err := io.Copy(writer, bytes.NewReader(plain)); err != nil {
		_ = writer.Close()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	if err := writer.Close(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	_ = tmp.Close()
	if err := os.Rename(tmpPath, s.privateKeyPath(username)); err != nil {
		_ = os.Remove(tmpPath)
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	pub := ssh.MarshalAuthorizedKey(signer.PublicKey())
	if err := os.WriteFile(s.publicKeyPath(username), pub, 0o644); err != nil {
		if s.log != nil {
			s.log.Warn("ssh key write failed", "user", username, "err", err)
		}
		return "", err
	}
	if s.log != nil {
		action := "generated"
		if rotate {
			action = "rotated"
		}
		s.log.Info("ssh key write ok", "user", username, "action", action)
	}
	return strings.TrimSpace(string(pub)), nil
}

func (s *Store) materialForUser(username string, rotate bool) (keymgmt.Material, keymgmt.RootKey, error) {
	store, err := keymgmt.LoadProto(s.storePath)
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key material load failed", "user", username, "err", err)
		}
		return keymgmt.Material{}, keymgmt.RootKey{}, err
	}
	root, err := store.EnsureRootKey()
	if err != nil {
		if s.log != nil {
			s.log.Warn("ssh key material load failed", "user", username, "err", err)
		}
		return keymgmt.Material{}, keymgmt.RootKey{}, err
	}
	descName := descriptorName(username)
	contextBytes := []byte(descName)
	var material keymgmt.Material
	if rotate {
		material, err = keymgmt.MintDEK(root, contextBytes)
		if err != nil {
			if s.log != nil {
				s.log.Warn("ssh key material mint failed", "user", username, "err", err)
			}
			return keymgmt.Material{}, keymgmt.RootKey{}, err
		}
		if err := store.SetDescriptor(descName, material.Descriptor); err != nil {
			if s.log != nil {
				s.log.Warn("ssh key material update failed", "user", username, "err", err)
			}
			return keymgmt.Material{}, keymgmt.RootKey{}, err
		}
	} else {
		material, err = store.EnsureDescriptor(descName, root, contextBytes)
		if err != nil {
			if s.log != nil {
				s.log.Warn("ssh key material ensure failed", "user", username, "err", err)
			}
			return keymgmt.Material{}, keymgmt.RootKey{}, err
		}
	}
	if err := store.Commit(); err != nil {
		if s.log != nil {
			s.log.Warn("ssh key material commit failed", "user", username, "err", err)
		}
		return keymgmt.Material{}, keymgmt.RootKey{}, err
	}
	return material, root, nil
}

func descriptorName(username string) string {
	return descriptorPrefix + username
}

func (s *Store) userDir(username string) string {
	return filepath.Join(s.keyDir, username)
}

func (s *Store) privateKeyPath(username string) string {
	return filepath.Join(s.userDir(username), defaultKeyFile)
}

func (s *Store) publicKeyPath(username string) string {
	return filepath.Join(s.userDir(username), defaultPubFile)
}

func (s *Store) keyExists(username string) (bool, error) {
	path := s.privateKeyPath(username)
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
