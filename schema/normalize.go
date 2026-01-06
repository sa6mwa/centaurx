package schema

import (
	"strings"
	"unicode"
)

// NormalizeModelID validates and normalizes a model identifier.
// Allowed characters: A-Z, a-z, 0-9, '.', '_', '-'.
func NormalizeModelID(model string) (ModelID, error) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", ErrInvalidModel
	}
	for _, r := range trimmed {
		if r == '.' || r == '_' || r == '-' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return "", ErrInvalidModel
	}
	return ModelID(trimmed), nil
}

// NormalizeModelReasoningEffort validates and normalizes a reasoning effort value.
// Allowed values: low, medium, high, xhigh.
func NormalizeModelReasoningEffort(value string) (ModelReasoningEffort, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "", ErrInvalidModelReasoningEffort
	}
	switch trimmed {
	case "low", "medium", "high", "xhigh":
		return ModelReasoningEffort(trimmed), nil
	default:
		return "", ErrInvalidModelReasoningEffort
	}
}

// ValidateUserID ensures a user id matches [a-z0-9._-] with no normalization.
func ValidateUserID(userID UserID) error {
	raw := string(userID)
	if raw == "" {
		return ErrInvalidUser
	}
	if strings.TrimSpace(raw) != raw {
		return ErrInvalidUser
	}
	for _, r := range raw {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' || r == '_' || r == '-' {
			continue
		}
		return ErrInvalidUser
	}
	return nil
}
