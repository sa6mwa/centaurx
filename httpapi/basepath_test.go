package httpapi

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", ""},
		{"centaurx", "/centaurx"},
		{"/centaurx", "/centaurx"},
		{"/centaurx/", "/centaurx"},
	}
	for _, tc := range cases {
		if got := normalizeBasePath(tc.in); got != tc.want {
			t.Fatalf("normalizeBasePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildBaseHref(t *testing.T) {
	cases := []struct {
		baseURL  string
		basePath string
		want     string
	}{
		{"", "", ""},
		{"", "/centaurx", "/centaurx/"},
		{"", "centaurx", "/centaurx/"},
		{"https://example.com", "", "https://example.com/"},
		{"https://example.com/", "centaurx", "https://example.com/centaurx/"},
		{"https://example.com/base", "/x", "https://example.com/base/x/"},
	}
	for _, tc := range cases {
		if got := buildBaseHref(tc.baseURL, tc.basePath); got != tc.want {
			t.Fatalf("buildBaseHref(%q, %q) = %q, want %q", tc.baseURL, tc.basePath, got, tc.want)
		}
	}
}
