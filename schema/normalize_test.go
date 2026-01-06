package schema

import "testing"

func TestValidateUserID(t *testing.T) {
	cases := []struct {
		name  string
		user  UserID
		valid bool
	}{
		{"simple", "alice", true},
		{"with-dots", "alice.dev", true},
		{"with-underscore", "alice_dev", true},
		{"with-dash", "alice-dev", true},
		{"with-digits", "alice123", true},
		{"empty", "", false},
		{"uppercase", "Alice", false},
		{"space", "alice dev", false},
		{"leading-space", " alice", false},
		{"trailing-space", "alice ", false},
		{"unicode", "ålice", false},
		{"symbol", "alice@", false},
	}

	for _, tc := range cases {
		err := ValidateUserID(tc.user)
		if tc.valid && err != nil {
			t.Fatalf("case %q expected valid, got error: %v", tc.name, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("case %q expected error, got nil", tc.name)
		}
	}
}
