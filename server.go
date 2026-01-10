package centaurx

import (
	"context"
	"errors"
	"sync"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/httpapi"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/auth"
	"pkt.systems/centaurx/internal/command"
	"pkt.systems/centaurx/internal/eventbus"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/schema"
	"pkt.systems/centaurx/sshserver"
	"pkt.systems/pslog"
)

// Server composes the HTTP, SSH, and runner services.
type Server interface {
	Start(ctx context.Context) error
	Wait() error
	Stop(ctx context.Context) error
}

// ServerConfig configures the compositor.
type ServerConfig struct {
	Service             schema.ServiceConfig
	HTTP                httpapi.Config
	SSH                 sshserver.Config
	Auth                AuthConfig
	HubHistory          int
	CommitModel         schema.ModelID
	DisableAuditLogging bool
}

// AuthConfig defines authentication storage settings.
type AuthConfig struct {
	UserFile  string
	SeedUsers []SeedUser
}

// SeedUser seeds an initial user record.
type SeedUser struct {
	Username     string
	PasswordHash string
	TOTPSecret   string
}

// ServerDeps captures dependencies required to build the server.
type ServerDeps struct {
	ServiceDeps core.ServiceDeps
	Runner      core.RunnerServer
}

// ServerOption toggles compositor components.
type ServerOption func(*serverOptions)

type serverOptions struct {
	enableHTTP   bool
	enableSSH    bool
	enableRunner bool
}

// WithHTTP enables the HTTP API/UI server.
func WithHTTP() ServerOption {
	return func(o *serverOptions) { o.enableHTTP = true }
}

// WithSSH enables the SSH server.
func WithSSH() ServerOption {
	return func(o *serverOptions) { o.enableSSH = true }
}

// WithRunner enables the runner server (if provided in deps).
func WithRunner() ServerOption {
	return func(o *serverOptions) { o.enableRunner = true }
}

// New constructs a composable centaurx server.
func New(cfg ServerConfig, deps ServerDeps, opts ...ServerOption) (Server, error) {
	options := serverOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	if !options.enableHTTP && !options.enableSSH && !options.enableRunner {
		return nil, errors.New("no services enabled")
	}

	var hub *httpapi.Hub
	var bus *eventbus.Bus
	var authStore *auth.Store
	var gitKeyStore *sshkeys.Store
	var httpSrv *httpapi.Server
	var sshSrv *sshserver.Server
	if options.enableHTTP || options.enableSSH {
		if deps.ServiceDeps.RunnerProvider == nil {
			return nil, errors.New("runner dependency is required")
		}
		normalized, err := schema.NormalizeServiceConfig(cfg.Service)
		if err != nil {
			return nil, err
		}
		cfg.Service = normalized

		serviceDeps := deps.ServiceDeps
		if options.enableSSH {
			bus = eventbus.New(serviceDeps.Logger)
		}
		if options.enableHTTP {
			hub = httpapi.NewHub(cfg.HubHistory)
		}

		if serviceDeps.EventSink == nil && hub == nil && bus == nil {
			serviceDeps.EventSink = nil
		} else {
			sinks := make([]core.EventSink, 0, 3)
			if serviceDeps.EventSink != nil {
				sinks = append(sinks, serviceDeps.EventSink)
			}
			if hub != nil && serviceDeps.EventSink != hub {
				sinks = append(sinks, hub)
			}
			if bus != nil && serviceDeps.EventSink != bus {
				sinks = append(sinks, bus)
			}
			if len(sinks) == 1 {
				serviceDeps.EventSink = sinks[0]
			} else {
				serviceDeps.EventSink = eventFanout{sinks: sinks}
			}
		}

		service, err := core.NewService(cfg.Service, serviceDeps)
		if err != nil {
			return nil, err
		}

		logger := deps.ServiceDeps.Logger
		seeds := toSeedUsers(cfg.Auth.SeedUsers)
		store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, seeds, logger)
		if err != nil {
			return nil, err
		}
		authStore = store

		gitStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
		if err != nil {
			return nil, err
		}
		gitKeyStore = gitStore

		cmdHandler := command.NewHandler(service, serviceDeps.RunnerProvider, command.HandlerConfig{
			AllowedModels:       cfg.Service.AllowedModels,
			CommitModel:         cfg.CommitModel,
			RepoRoot:            cfg.Service.RepoRoot,
			LoginPubKeyStore:    authStore,
			GitKeyStore:         gitKeyStore,
			GitKeyRotator:       gitKeyStore,
			DisableAuditLogging: cfg.DisableAuditLogging,
		})

		if options.enableHTTP {
			httpSrv = httpapi.NewServer(cfg.HTTP, service, cmdHandler, authStore, hub)
		}

		if options.enableSSH {
			sshSrv = &sshserver.Server{
				Addr:        cfg.SSH.Addr,
				HostKeyPath: cfg.SSH.HostKeyPath,
				Service:     service,
				Handler:     cmdHandler,
				IdlePrompt:  cfg.SSH.IdlePrompt,
				AuthStore:   authStore,
				EventBus:    bus,
			}
		}
	}

	if options.enableRunner && deps.Runner == nil {
		return nil, errors.New("runner server dependency is required")
	}

	return &compositeServer{
		cfg:     cfg,
		options: options,
		httpSrv: httpSrv,
		sshSrv:  sshSrv,
		runner:  deps.Runner,
		runners: deps.ServiceDeps.RunnerProvider,
	}, nil
}

type compositeServer struct {
	cfg     ServerConfig
	options serverOptions
	httpSrv *httpapi.Server
	sshSrv  *sshserver.Server
	runner  core.RunnerServer
	runners core.RunnerProvider
	logger  pslog.Logger

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	errCh   chan error
	started bool
}

func (s *compositeServer) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		pslog.Ctx(ctx).Warn("server start rejected", "reason", "already started")
		return errors.New("server already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.errCh = make(chan error, 3)
	s.started = true
	s.logger = pslog.Ctx(s.ctx)
	s.mu.Unlock()

	log := s.logger
	log.Info(
		"server start",
		"http", s.options.enableHTTP,
		"ssh", s.options.enableSSH,
		"runner", s.options.enableRunner,
		"http_addr", s.cfg.HTTP.Addr,
		"http_base_url", s.cfg.HTTP.BaseURL,
		"http_base_path", s.cfg.HTTP.BasePath,
		"ssh_addr", s.cfg.SSH.Addr,
	)
	if s.options.enableHTTP && s.httpSrv != nil {
		s.httpSrv.SetBaseContext(s.ctx)
		go func() {
			if err := httpapi.ListenAndServe(s.ctx, s.cfg.HTTP.Addr, s.httpSrv.Handler()); err != nil {
				log.Error("http server failed", "err", err)
				s.errCh <- err
			}
		}()
	}
	if s.options.enableSSH && s.sshSrv != nil {
		go func() {
			if err := s.sshSrv.ListenAndServe(s.ctx); err != nil {
				log.Error("ssh server failed", "err", err)
				s.errCh <- err
			}
		}()
	}
	if s.options.enableRunner && s.runner != nil {
		go func() {
			if err := s.runner.ListenAndServe(s.ctx); err != nil {
				log.Error("runner server failed", "err", err)
				s.errCh <- err
			}
		}()
	}
	return nil
}

func (s *compositeServer) Wait() error {
	s.mu.Lock()
	ctx := s.ctx
	errCh := s.errCh
	started := s.started
	s.mu.Unlock()
	if !started {
		return errors.New("server not started")
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if err != nil {
			pslog.Ctx(ctx).Error("server stopped", "err", err)
			_ = s.Stop(context.Background())
			return err
		}
		return nil
	}
}

func (s *compositeServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	started := s.started
	log := s.logger
	s.mu.Unlock()
	if !started {
		return nil
	}
	if log == nil {
		log = pslog.Ctx(context.Background())
	}
	log.Info("server stop requested")
	if s.runners != nil {
		if err := s.runners.CloseAll(context.Background()); err != nil {
			log.Warn("server runner close failed", "err", err)
		} else {
			log.Info("server runner close ok")
		}
	}
	if cancel != nil {
		cancel()
	}
	if ctx == nil {
		log.Info("server stop completed")
		return nil
	}
	select {
	case <-ctx.Done():
		log.Warn("server stop timed out", "err", ctx.Err())
		return ctx.Err()
	case <-s.ctx.Done():
		log.Info("server stopped")
		return nil
	}
}

func toSeedUsers(users []SeedUser) []appconfig.SeedUser {
	if len(users) == 0 {
		return nil
	}
	out := make([]appconfig.SeedUser, 0, len(users))
	for _, user := range users {
		out = append(out, appconfig.SeedUser{
			Username:     user.Username,
			PasswordHash: user.PasswordHash,
			TOTPSecret:   user.TOTPSecret,
		})
	}
	return out
}
