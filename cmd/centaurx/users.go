package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mdp/qrterminal/v3"
	"github.com/pquerna/otp/totp"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/auth"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/internal/userhome"
	"pkt.systems/centaurx/schema"
	"pkt.systems/kryptograf/keymgmt"
	"pkt.systems/pslog"
)

const (
	defaultPasswordLength = 20
	totpIssuer            = "centaurx"
)

func newUsersCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage centaurx users",
	}
	cmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "path to config file")

	cmd.AddCommand(newUsersListCmd(&cfgPath))
	cmd.AddCommand(newUsersAddCmd(&cfgPath))
	cmd.AddCommand(newUsersDeleteCmd(&cfgPath))
	cmd.AddCommand(newUsersRotateTOTP(&cfgPath))
	cmd.AddCommand(newUsersRotateSSHKey(&cfgPath))
	cmd.AddCommand(newUsersChpasswd(&cfgPath))
	cmd.AddCommand(newUsersAddLoginPubKey(&cfgPath))
	cmd.AddCommand(newUsersListLoginPubKeys(&cfgPath))
	cmd.AddCommand(newUsersRemoveLoginPubKey(&cfgPath))

	return cmd
}

func newUsersListCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List users",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			users := store.LoadUsers()
			out := cmd.OutOrStdout()
			for _, user := range users {
				_, _ = fmt.Fprintln(out, user.Username)
			}
			return nil
		},
	}
}

func newUsersAddCmd(cfgPath *string) *cobra.Command {
	var passwordFromStdin bool
	var autoPassword bool
	var sshKeyType string
	var sshKeyBits int
	cmd := &cobra.Command{
		Use:   "add <username>",
		Short: "Add a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			password, generated, err := resolvePassword(cmd, passwordFromStdin, autoPassword)
			if err != nil {
				return err
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			secret, url, err := generateTOTP(username)
			if err != nil {
				return err
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			if err := store.AddUser(auth.User{
				Username:     username,
				PasswordHash: string(hash),
				TOTPSecret:   secret,
			}); err != nil {
				return err
			}
			pubKey, err := keyStore.GenerateKey(username, sshKeyType, sshKeyBits)
			if err != nil {
				_ = store.DeleteUser(username)
				return err
			}
			skelDir := userhome.SkelDir(cfg.StateDir)
			data := userhome.DefaultTemplateData(cfg)
			if _, err := userhome.EnsureHome(cfg.StateDir, username, skelDir, data); err != nil {
				_ = store.DeleteUser(username)
				_ = keyStore.RemoveKey(username)
				return err
			}
			printUserEnrollment(cmd.OutOrStdout(), username, password, generated, secret, url, pubKey)
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordFromStdin, "password-from-stdin", false, "read password from stdin")
	cmd.Flags().BoolVar(&autoPassword, "auto-password", false, "generate a random password")
	cmd.Flags().StringVar(&sshKeyType, "ssh-key-type", sshkeys.KeyTypeEd25519, "ssh key type (ed25519 or rsa)")
	cmd.Flags().IntVar(&sshKeyBits, "ssh-key-bits", sshkeys.DefaultRSABits, "ssh key size when using rsa")
	return cmd
}

func newUsersDeleteCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			if err := store.DeleteUser(args[0]); err != nil {
				return err
			}
			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			if err := keyStore.RemoveKey(args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "deleted user: %s\n", args[0])
			return nil
		},
	}
}

func newUsersRotateTOTP(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-totp <username>",
		Short: "Rotate TOTP secret for a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			secret, url, err := generateTOTP(username)
			if err != nil {
				return err
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			if err := store.UpdateTOTP(username, secret); err != nil {
				return err
			}
			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			pubKey, err := keyStore.LoadPublicKey(username)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			printUserEnrollment(cmd.OutOrStdout(), username, "", false, secret, url, pubKey)
			return nil
		},
	}
}

func newUsersChpasswd(cfgPath *string) *cobra.Command {
	var passwordFromStdin bool
	var autoPassword bool
	cmd := &cobra.Command{
		Use:   "chpasswd <username>",
		Short: "Change a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			password, generated, err := resolvePassword(cmd, passwordFromStdin, autoPassword)
			if err != nil {
				return err
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			if err := store.UpdatePassword(username, string(hash)); err != nil {
				return err
			}
			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			pubKey, err := keyStore.LoadPublicKey(username)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			printUserEnrollment(cmd.OutOrStdout(), username, password, generated, "", "", pubKey)
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordFromStdin, "password-from-stdin", false, "read password from stdin")
	cmd.Flags().BoolVar(&autoPassword, "auto-password", false, "generate a random password")
	return cmd
}

func newUsersRotateSSHKey(cfgPath *string) *cobra.Command {
	var sshKeyType string
	var sshKeyBits int
	cmd := &cobra.Command{
		Use:   "rotate-ssh-key <username>",
		Short: "Rotate SSH key for a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			pubKey, err := keyStore.RotateKey(username, sshKeyType, sshKeyBits)
			if err != nil {
				return err
			}
			printUserEnrollment(cmd.OutOrStdout(), username, "", false, "", "", pubKey)
			return nil
		},
	}
	cmd.Flags().StringVar(&sshKeyType, "ssh-key-type", sshkeys.KeyTypeEd25519, "ssh key type (ed25519 or rsa)")
	cmd.Flags().IntVar(&sshKeyBits, "ssh-key-bits", sshkeys.DefaultRSABits, "ssh key size when using rsa")
	return cmd
}

func newUsersAddLoginPubKey(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "add-login-pubkey <username> <pubkey>",
		Short: "Add an SSH login public key to a user",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			pubKey := strings.TrimSpace(strings.Join(args[1:], " "))
			if pubKey == "" {
				return errors.New("pubkey is required")
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			id, err := store.AddLoginPubKey(schema.UserID(username), pubKey)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "login pubkey added (id %d)\n", id)
			return nil
		},
	}
}

func newUsersListLoginPubKeys(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list-login-pubkeys <username>",
		Short: "List SSH login public keys for a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			keys, err := store.ListLoginPubKeys(schema.UserID(username))
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(keys) == 0 {
				_, _ = fmt.Fprintln(out, "no login pubkeys")
				return nil
			}
			for idx, key := range keys {
				_, _ = fmt.Fprintf(out, "%d) %s\n", idx+1, strings.TrimSpace(key))
			}
			return nil
		},
	}
}

func newUsersRemoveLoginPubKey(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rm-login-pubkey <username> <id>",
		Short: "Remove an SSH login public key from a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if err := validateUsername(username); err != nil {
				return err
			}
			id, err := strconv.Atoi(args[1])
			if err != nil || id <= 0 {
				return errors.New("invalid pubkey id")
			}
			cfg, err := appconfig.Load(*cfgPath)
			if err != nil {
				return err
			}
			logger := pslog.Ctx(cmd.Context())
			store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
			if err != nil {
				return err
			}
			if err := store.RemoveLoginPubKey(schema.UserID(username), id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "login pubkey removed (id %d)\n", id)
			return nil
		},
	}
}

func resolvePassword(cmd *cobra.Command, fromStdin, auto bool) (string, bool, error) {
	if fromStdin && auto {
		return "", false, errors.New("choose one of --password-from-stdin or --auto-password")
	}
	if fromStdin {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", false, err
		}
		pass := strings.TrimSpace(string(data))
		if pass == "" {
			return "", false, errors.New("password from stdin is empty")
		}
		return pass, false, nil
	}
	if auto {
		pass, err := generatePassword(defaultPasswordLength)
		if err != nil {
			return "", false, err
		}
		return pass, true, nil
	}
	passphrase, err := keymgmt.PromptPassphrase(cmd.InOrStdin(), "Password: ", cmd.ErrOrStderr())
	if err != nil {
		return "", false, err
	}
	confirm, err := keymgmt.PromptPassphrase(cmd.InOrStdin(), "Confirm password: ", cmd.ErrOrStderr())
	if err != nil {
		return "", false, err
	}
	if string(passphrase) != string(confirm) {
		return "", false, errors.New("passwords do not match")
	}
	pass := string(passphrase)
	if pass == "" {
		return "", false, errors.New("password is empty")
	}
	return pass, false, nil
}

func generatePassword(length int) (string, error) {
	if length <= 0 {
		length = defaultPasswordLength
	}
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i, b := range bytes {
		bytes[i] = charset[int(b)%len(charset)]
	}
	return string(bytes), nil
}

func generateTOTP(username string) (string, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func printUserEnrollment(w io.Writer, username, password string, showPassword bool, secret, url string, sshPublicKey string) {
	_, _ = fmt.Fprintf(w, "username: %s\n", username)
	if showPassword && password != "" {
		_, _ = fmt.Fprintf(w, "password: %s\n", password)
	}
	if secret != "" {
		_, _ = fmt.Fprintf(w, "totp_secret: %s\n", secret)
	}
	if url != "" {
		_, _ = fmt.Fprintf(w, "otpauth_url: %s\n", url)
		_, _ = fmt.Fprintln(w, "totp_qr:")
		qrterminal.GenerateHalfBlock(url, qrterminal.L, w)
	}
	if sshPublicKey != "" {
		_, _ = fmt.Fprintf(w, "ssh_public_key: %s\n", strings.TrimSpace(sshPublicKey))
	}
}

func validateUsername(username string) error {
	if err := schema.ValidateUserID(schema.UserID(username)); err != nil {
		return errors.New("invalid username: must match [a-z0-9._-]")
	}
	return nil
}
