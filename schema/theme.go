package schema

import "strings"

// DefaultTheme is the default UI theme name.
const DefaultTheme ThemeName = "outrun"

var themeNames = []ThemeName{
	"outrun",
	"gruvbox",
	"tokyo-midnight",
}

// AvailableThemes returns the supported theme names.
func AvailableThemes() []ThemeName {
	out := make([]ThemeName, len(themeNames))
	copy(out, themeNames)
	return out
}

// NormalizeThemeName returns a canonical theme name if supported.
func NormalizeThemeName(name string) (ThemeName, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "outrun", "outrun-electric":
		return "outrun", true
	case "gruvbox":
		return "gruvbox", true
	case "tokyo-midnight", "tokyo":
		return "tokyo-midnight", true
	default:
		return "", false
	}
}
