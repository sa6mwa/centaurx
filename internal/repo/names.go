package repo

import "pkt.systems/centaurx/schema"

// NormalizeRepoName validates and normalizes a repo name.
func NormalizeRepoName(name schema.RepoName) (schema.RepoName, error) {
	return normalizeRepoName(name)
}
