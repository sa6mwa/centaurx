package sshserver

// Config defines SSH server settings.
type Config struct {
	Addr         string
	HostKeyPath  string
	IdlePrompt   string
	KeyStorePath string
	KeyDir       string
}
