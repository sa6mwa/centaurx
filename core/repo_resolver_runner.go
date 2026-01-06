package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/internal/repo"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// NewRunnerRepoResolver returns a resolver that performs git operations via the runner.
func NewRunnerRepoResolver(root string, runners RunnerProvider) (RepoResolver, error) {
	if strings.TrimSpace(root) == "" {
		return nil, schema.ErrInvalidRepo
	}
	if runners == nil {
		return nil, errors.New("runner provider is required")
	}
	return &runnerRepoResolver{root: root, runners: runners}, nil
}

type runnerRepoResolver struct {
	root    string
	runners RunnerProvider
}

func (r *runnerRepoResolver) CreateRepo(ctx context.Context, req CreateRepoRequest) (CreateRepoResponse, error) {
	log := logx.WithUserTab(ctx, req.UserID, req.TabID).With("repo", req.Name)
	manager, err := r.manager(ctx, req.UserID)
	if err != nil {
		log.Warn("repo create failed", "err", err)
		return CreateRepoResponse{}, err
	}
	repoName, err := repo.NormalizeRepoName(req.Name)
	if err != nil {
		log.Warn("repo create failed", "err", err)
		return CreateRepoResponse{}, err
	}
	path := filepath.Join(manager.Root(), string(repoName))
	if _, err := os.Stat(path); err == nil {
		log.Warn("repo create failed", "err", schema.ErrRepoExists)
		return CreateRepoResponse{}, schema.ErrRepoExists
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Warn("repo create failed", "err", err)
		return CreateRepoResponse{}, err
	}

	runnerResp, err := r.runners.RunnerFor(ctx, RunnerRequest{UserID: req.UserID, TabID: req.TabID})
	if err != nil {
		log.Warn("repo create runner failed", "err", err)
		return CreateRepoResponse{}, err
	}
	runner := runnerResp.Runner
	info := runnerResp.Info
	workingDir, repoPath, err := r.mapRepoPath(manager.Root(), path, info.RepoRoot)
	if err != nil {
		log.Warn("repo create map failed", "err", err)
		return CreateRepoResponse{}, err
	}

	if err := r.runCommand(ctx, log, runner, coreRunCommand(workingDir, info.SSHAuthSock, fmt.Sprintf("git init %s", repoPath), false)); err != nil {
		return CreateRepoResponse{}, err
	}
	if err := r.runCommand(ctx, log, runner, coreRunCommand(workingDir, info.SSHAuthSock, fmt.Sprintf("git -C %s switch -c centaurx", repoPath), false)); err != nil {
		if err := r.runCommand(ctx, log, runner, coreRunCommand(workingDir, info.SSHAuthSock, fmt.Sprintf("git -C %s checkout -b centaurx", repoPath), false)); err != nil {
			return CreateRepoResponse{}, err
		}
	}
	log.Info("repo created", "path", path)
	return CreateRepoResponse{Repo: schema.RepoRef{Name: repoName, Path: path}}, nil
}

func (r *runnerRepoResolver) ResolveRepo(ctx context.Context, req ResolveRepoRequest) (ResolveRepoResponse, error) {
	log := logx.WithUser(ctx, req.UserID).With("repo", req.Name)
	manager, err := r.manager(ctx, req.UserID)
	if err != nil {
		log.Warn("repo resolve failed", "err", err)
		return ResolveRepoResponse{}, err
	}
	repoRef, err := manager.ResolveRepo(req.Name)
	if err != nil {
		log.Warn("repo resolve failed", "err", err)
		return ResolveRepoResponse{}, err
	}
	log.Debug("repo resolved", "path", repoRef.Path)
	return ResolveRepoResponse{Repo: repoRef}, nil
}

func (r *runnerRepoResolver) ListRepos(ctx context.Context, req ListReposRequest) (ListReposResponse, error) {
	log := logx.WithUser(ctx, req.UserID)
	manager, err := r.manager(ctx, req.UserID)
	if err != nil {
		log.Warn("repo list failed", "err", err)
		return ListReposResponse{}, err
	}
	repos, err := manager.ListRepos()
	if err != nil {
		log.Warn("repo list failed", "err", err)
		return ListReposResponse{}, err
	}
	log.Debug("repo list ok", "count", len(repos))
	return ListReposResponse{Repos: repos}, nil
}

func (r *runnerRepoResolver) OpenOrCloneURL(ctx context.Context, req OpenOrCloneRequest) (OpenOrCloneResponse, error) {
	log := logx.WithUserTab(ctx, req.UserID, req.TabID).With("url", req.URL)
	manager, err := r.manager(ctx, req.UserID)
	if err != nil {
		log.Warn("repo clone failed", "err", err)
		return OpenOrCloneResponse{}, err
	}
	cloneURL, repoName, err := repo.NormalizeGitURL(req.URL)
	if err != nil {
		log.Warn("repo clone failed", "err", err)
		return OpenOrCloneResponse{}, err
	}
	if repoRef, err := manager.ResolveRepo(repoName); err == nil {
		log.Info("repo clone skipped", "repo", repoRef.Name)
		return OpenOrCloneResponse{Repo: repoRef, Created: false}, nil
	} else if !errors.Is(err, schema.ErrRepoNotFound) {
		log.Warn("repo clone failed", "err", err)
		return OpenOrCloneResponse{}, err
	}

	path := filepath.Join(manager.Root(), string(repoName))
	if _, err := os.Stat(path); err == nil {
		log.Warn("repo clone failed", "err", schema.ErrRepoExists)
		return OpenOrCloneResponse{}, schema.ErrRepoExists
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Warn("repo clone failed", "err", err)
		return OpenOrCloneResponse{}, err
	}

	runnerResp, err := r.runners.RunnerFor(ctx, RunnerRequest{UserID: req.UserID, TabID: req.TabID})
	if err != nil {
		log.Warn("repo clone runner failed", "err", err)
		return OpenOrCloneResponse{}, err
	}
	runner := runnerResp.Runner
	info := runnerResp.Info
	workingDir, repoPath, err := r.mapRepoPath(manager.Root(), path, info.RepoRoot)
	if err != nil {
		log.Warn("repo clone map failed", "err", err)
		return OpenOrCloneResponse{}, err
	}
	cmd := fmt.Sprintf("git clone %s %s", cloneURL, repoPath)
	if err := r.runCommand(ctx, log, runner, coreRunCommand(workingDir, info.SSHAuthSock, cmd, false)); err != nil {
		return OpenOrCloneResponse{}, err
	}
	log.Info("repo clone ok", "repo", repoName)
	return OpenOrCloneResponse{Repo: schema.RepoRef{Name: repoName, Path: path}, Created: true}, nil
}

func (r *runnerRepoResolver) manager(ctx context.Context, userID schema.UserID) (*repo.Manager, error) {
	path := filepath.Join(r.root, string(userID))
	return repo.NewManagerWithLogger(path, logx.WithUser(ctx, userID))
}

func (r *runnerRepoResolver) mapRepoPath(root, path, runnerRoot string) (string, string, error) {
	workingDir := root
	repoPath := path
	if runnerRoot != "" {
		mappedRoot, err := MapRepoPath(r.root, runnerRoot, root)
		if err != nil {
			return "", "", err
		}
		mappedPath, err := MapRepoPath(r.root, runnerRoot, path)
		if err != nil {
			return "", "", err
		}
		workingDir = mappedRoot
		repoPath = mappedPath
	}
	return workingDir, repoPath, nil
}

func (r *runnerRepoResolver) runCommand(ctx context.Context, log pslog.Logger, runner Runner, req RunCommandRequest) error {
	handle, err := runner.RunCommand(ctx, req)
	if err != nil {
		if log != nil {
			log.Warn("repo command start failed", "err", err)
		}
		return err
	}
	defer func() { _ = handle.Close() }()
	if log != nil {
		log.Trace("repo command start", "workdir", req.WorkingDir, "command", req.Command)
	}
	stream := handle.Outputs()
	output := make([]string, 0, 8)
	for {
		line, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if log != nil {
				log.Warn("repo command stream failed", "err", err)
			}
			return err
		}
		if line.Text != "" {
			output = append(output, line.Text)
		}
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		if log != nil {
			log.Warn("repo command failed", "err", err)
		}
		return fmt.Errorf("command failed: %w (%s)", err, strings.Join(output, "\n"))
	}
	if result.ExitCode != 0 {
		if log != nil {
			log.Warn("repo command failed", "exit_code", result.ExitCode)
		}
		return fmt.Errorf("command exited with code %d (%s)", result.ExitCode, strings.Join(output, "\n"))
	}
	if log != nil {
		log.Debug("repo command ok", "exit_code", result.ExitCode)
	}
	return nil
}

func coreRunCommand(workingDir, sshAuthSock, cmd string, useShell bool) RunCommandRequest {
	return RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     cmd,
		UseShell:    useShell,
		SSHAuthSock: sshAuthSock,
	}
}
