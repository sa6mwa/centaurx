package httpapi

import "strings"

func normalizeBasePath(value string) string {
	path := strings.TrimSpace(value)
	if path == "" || path == "/" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimRight(path, "/")
	if path == "/" {
		return ""
	}
	return path
}

func buildBaseHref(baseURL, basePath string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	path := normalizeBasePath(basePath)
	if base == "" && path == "" {
		return ""
	}
	if base == "" {
		return ensureTrailingSlash(path)
	}
	return ensureTrailingSlash(base + path)
}

func ensureTrailingSlash(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "/") {
		return value
	}
	return value + "/"
}
