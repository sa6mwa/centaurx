package repo

import "testing"

func TestNormalizeGitURL(t *testing.T) {
	cases := []struct {
		input    string
		wantURL  string
		wantName string
		wantErr  bool
	}{
		{
			input:    "github.com/org/repo",
			wantURL:  "git@github.com:org/repo.git",
			wantName: "repo",
		},
		{
			input:    "git@github.com:org/repo",
			wantURL:  "git@github.com:org/repo.git",
			wantName: "repo",
		},
		{
			input:    "ssh://git@github.com/org/repo.git",
			wantURL:  "ssh://git@github.com/org/repo.git",
			wantName: "repo",
		},
		{
			input:   "https://github.com/org/repo",
			wantErr: true,
		},
		{
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		gotURL, gotName, err := NormalizeGitURL(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("NormalizeGitURL(%q): %v", tc.input, err)
		}
		if gotURL != tc.wantURL {
			t.Fatalf("NormalizeGitURL(%q) url: got %q want %q", tc.input, gotURL, tc.wantURL)
		}
		if string(gotName) != tc.wantName {
			t.Fatalf("NormalizeGitURL(%q) name: got %q want %q", tc.input, gotName, tc.wantName)
		}
	}
}
