package logx

import (
	"context"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

type contextKey int

const (
	userKey contextKey = iota
	tabKey
)

// Ctx returns the logger bound to the provided context.
func Ctx(ctx context.Context) pslog.Logger {
	return pslog.Ctx(ctx)
}

// WithUser annotates the logger with the user id if present.
func WithUser(ctx context.Context, userID schema.UserID) pslog.Logger {
	log := pslog.Ctx(ctx)
	if userID != "" {
		if current, ok := ctx.Value(userKey).(schema.UserID); ok && current == userID {
			return log
		}
		log = log.With("user", userID)
	}
	return log
}

// WithUserTab annotates the logger with user and tab identifiers.
func WithUserTab(ctx context.Context, userID schema.UserID, tabID schema.TabID) pslog.Logger {
	log := WithUser(ctx, userID)
	if tabID != "" {
		if current, ok := ctx.Value(tabKey).(schema.TabID); ok && current == tabID {
			return log
		}
		log = log.With("tab", tabID)
	}
	return log
}

// WithRepo annotates the logger with repo metadata when available.
func WithRepo(log pslog.Logger, repo schema.RepoRef) pslog.Logger {
	if repo.Name != "" {
		log = log.With("repo", repo.Name)
	}
	if repo.Path != "" {
		log = log.With("repo_path", repo.Path)
	}
	return log
}

// WithSession annotates the logger with a session id when available.
func WithSession(log pslog.Logger, sessionID schema.SessionID) pslog.Logger {
	if sessionID != "" {
		log = log.With("session", sessionID)
	}
	return log
}

// ContextWithUser stores the user marker on the context for log de-duplication.
func ContextWithUser(ctx context.Context, userID schema.UserID) context.Context {
	if ctx == nil || userID == "" {
		return ctx
	}
	return context.WithValue(ctx, userKey, userID)
}

// ContextWithTab stores the tab marker on the context for log de-duplication.
func ContextWithTab(ctx context.Context, tabID schema.TabID) context.Context {
	if ctx == nil || tabID == "" {
		return ctx
	}
	return context.WithValue(ctx, tabKey, tabID)
}

// ContextWithUserTab stores user/tab markers on the context for log de-duplication.
func ContextWithUserTab(ctx context.Context, userID schema.UserID, tabID schema.TabID) context.Context {
	return ContextWithTab(ContextWithUser(ctx, userID), tabID)
}

// ContextWithUserLogger attaches the logger and user marker to the context.
func ContextWithUserLogger(ctx context.Context, log pslog.Logger, userID schema.UserID) context.Context {
	ctx = pslog.ContextWithLogger(ctx, log)
	return ContextWithUser(ctx, userID)
}

// ContextWithUserTabLogger attaches the logger and user/tab markers to the context.
func ContextWithUserTabLogger(ctx context.Context, log pslog.Logger, userID schema.UserID, tabID schema.TabID) context.Context {
	ctx = pslog.ContextWithLogger(ctx, log)
	return ContextWithUserTab(ctx, userID, tabID)
}

// CopyContextFields copies user/tab markers from src to dst.
func CopyContextFields(dst context.Context, src context.Context) context.Context {
	if src == nil {
		return dst
	}
	if user, ok := src.Value(userKey).(schema.UserID); ok && user != "" {
		dst = ContextWithUser(dst, user)
	}
	if tab, ok := src.Value(tabKey).(schema.TabID); ok && tab != "" {
		dst = ContextWithTab(dst, tab)
	}
	return dst
}
