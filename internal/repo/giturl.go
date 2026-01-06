package repo

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"pkt.systems/centaurx/schema"
)

// NormalizeGitURL converts shorthand git references into SSH clone URLs and extracts the repo name.
func NormalizeGitURL(raw string) (string, schema.RepoName, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", "", schema.ErrInvalidRepo
	}
	lower := strings.ToLower(input)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", "", errors.New("only ssh git URLs are supported")
	}

	if strings.HasPrefix(lower, "ssh://") {
		parsed, err := url.Parse(input)
		if err != nil {
			return "", "", err
		}
		trimmed := strings.TrimPrefix(parsed.Path, "/")
		if trimmed == "" {
			return "", "", schema.ErrInvalidRepo
		}
		repoName, err := repoNameFromPath(trimmed)
		if err != nil {
			return "", "", err
		}
		parsed.Path = "/" + ensureGitSuffix(trimmed)
		return parsed.String(), repoName, nil
	}

	if strings.Contains(input, "@") && strings.Contains(input, ":") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", schema.ErrInvalidRepo
		}
		pathPart := strings.TrimPrefix(parts[1], "/")
		repoName, err := repoNameFromPath(pathPart)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%s:%s", parts[0], ensureGitSuffix(pathPart)), repoName, nil
	}

	if strings.Contains(input, "/") {
		host, rest, ok := strings.Cut(input, "/")
		if !ok || host == "" || rest == "" {
			return "", "", schema.ErrInvalidRepo
		}
		repoName, err := repoNameFromPath(rest)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("git@%s:%s", host, ensureGitSuffix(rest)), repoName, nil
	}

	return "", "", schema.ErrInvalidRepo
}

func ensureGitSuffix(value string) string {
	if strings.HasSuffix(value, ".git") {
		return value
	}
	return value + ".git"
}

func repoNameFromPath(value string) (schema.RepoName, error) {
	if strings.TrimSpace(value) == "" {
		return "", schema.ErrInvalidRepo
	}
	base := path.Base(strings.TrimSuffix(value, ".git"))
	return normalizeRepoName(schema.RepoName(base))
}
