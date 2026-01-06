package core

import (
	"context"
	"path/filepath"

	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/internal/repo"
	"pkt.systems/centaurx/schema"
)

// RepoResolver manages repository discovery and creation per user.
type RepoResolver interface {
	CreateRepo(ctx context.Context, req CreateRepoRequest) (CreateRepoResponse, error)
	ResolveRepo(ctx context.Context, req ResolveRepoRequest) (ResolveRepoResponse, error)
	ListRepos(ctx context.Context, req ListReposRequest) (ListReposResponse, error)
	OpenOrCloneURL(ctx context.Context, req OpenOrCloneRequest) (OpenOrCloneResponse, error)
}

// CreateRepoRequest requests repo creation.
type CreateRepoRequest struct {
	UserID schema.UserID
	TabID  schema.TabID
	Name   schema.RepoName
}

// CreateRepoResponse describes a created repo.
type CreateRepoResponse struct {
	Repo schema.RepoRef
}

// ResolveRepoRequest requests repo resolution.
type ResolveRepoRequest struct {
	UserID schema.UserID
	Name   schema.RepoName
}

// ResolveRepoResponse describes a resolved repo.
type ResolveRepoResponse struct {
	Repo schema.RepoRef
}

// ListReposRequest lists repos for a user.
type ListReposRequest struct {
	UserID schema.UserID
}

// ListReposResponse lists repos for a user.
type ListReposResponse struct {
	Repos []schema.RepoRef
}

// OpenOrCloneRequest requests a clone-or-open operation.
type OpenOrCloneRequest struct {
	UserID schema.UserID
	TabID  schema.TabID
	URL    string
}

// OpenOrCloneResponse describes a clone-or-open result.
type OpenOrCloneResponse struct {
	Repo    schema.RepoRef
	Created bool
}

// NewRepoResolver returns the default local resolver implementation.
func NewRepoResolver(root string) (RepoResolver, error) {
	if root == "" {
		return nil, schema.ErrInvalidRepo
	}
	return &localRepoResolver{root: root}, nil
}

type localRepoResolver struct {
	root string
}

func (r *localRepoResolver) CreateRepo(ctx context.Context, req CreateRepoRequest) (CreateRepoResponse, error) {
	log := logx.WithUserTab(ctx, req.UserID, req.TabID).With("repo", req.Name)
	manager, err := r.manager(ctx, req.UserID)
	if err != nil {
		log.Warn("repo create failed", "err", err)
		return CreateRepoResponse{}, err
	}
	repoRef, err := manager.CreateRepo(req.Name)
	if err != nil {
		log.Warn("repo create failed", "err", err)
		return CreateRepoResponse{}, err
	}
	log.Info("repo created", "path", repoRef.Path)
	return CreateRepoResponse{Repo: repoRef}, nil
}

func (r *localRepoResolver) ResolveRepo(ctx context.Context, req ResolveRepoRequest) (ResolveRepoResponse, error) {
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

func (r *localRepoResolver) ListRepos(ctx context.Context, req ListReposRequest) (ListReposResponse, error) {
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

func (r *localRepoResolver) OpenOrCloneURL(ctx context.Context, req OpenOrCloneRequest) (OpenOrCloneResponse, error) {
	log := logx.WithUserTab(ctx, req.UserID, req.TabID).With("url", req.URL)
	manager, err := r.manager(ctx, req.UserID)
	if err != nil {
		log.Warn("repo clone failed", "err", err)
		return OpenOrCloneResponse{}, err
	}
	repoRef, created, err := manager.OpenOrCloneURL(req.URL)
	if err != nil {
		log.Warn("repo clone failed", "err", err)
		return OpenOrCloneResponse{}, err
	}
	log.Info("repo clone ok", "repo", repoRef.Name, "created", created)
	return OpenOrCloneResponse{Repo: repoRef, Created: created}, nil
}

func (r *localRepoResolver) manager(ctx context.Context, userID schema.UserID) (*repo.Manager, error) {
	path := filepath.Join(r.root, string(userID))
	return repo.NewManagerWithLogger(path, logx.WithUser(ctx, userID))
}
