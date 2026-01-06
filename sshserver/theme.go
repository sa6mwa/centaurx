package sshserver

import (
	"strconv"

	"pkt.systems/centaurx/schema"
)

type rgb struct {
	r int
	g int
	b int
}

type tuiTheme struct {
	Name             schema.ThemeName
	TabBarBG         rgb
	TabActiveBG      rgb
	TabActiveFG      rgb
	TabInactiveBG    rgb
	TabInactiveFG    rgb
	ErrorFG          rgb
	StderrFG         rgb
	MetaFG           rgb
	PromptFG         rgb
	SpinnerFG        rgb
	ReasoningFG      rgb
	ReasoningBold    rgb
	CodeFG           rgb
	AboutLinkFG      rgb
	AboutCopyrightFG rgb
	HelpArgFG        rgb
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiItalic = "\x1b[3m"
)

var tuiThemes = map[schema.ThemeName]tuiTheme{
	"outrun": {
		Name:             "outrun",
		TabBarBG:         rgb{r: 32, g: 8, b: 56},
		TabActiveBG:      rgb{r: 0, g: 229, b: 255},
		TabActiveFG:      rgb{r: 10, g: 13, b: 23},
		TabInactiveBG:    rgb{r: 32, g: 8, b: 56},
		TabInactiveFG:    rgb{r: 240, g: 241, b: 255},
		ErrorFG:          rgb{r: 255, g: 107, b: 107},
		StderrFG:         rgb{r: 255, g: 91, b: 189},
		MetaFG:           rgb{r: 154, g: 163, b: 178},
		PromptFG:         rgb{r: 255, g: 255, b: 255},
		SpinnerFG:        rgb{r: 110, g: 136, b: 255},
		ReasoningFG:      rgb{r: 110, g: 136, b: 255},
		ReasoningBold:    rgb{r: 255, g: 91, b: 189},
		CodeFG:           rgb{r: 112, g: 214, b: 255},
		AboutLinkFG:      rgb{r: 112, g: 214, b: 255},
		AboutCopyrightFG: rgb{r: 60, g: 79, b: 184},
		HelpArgFG:        rgb{r: 154, g: 182, b: 255},
	},
	"gruvbox": {
		Name:             "gruvbox",
		TabBarBG:         rgb{r: 60, g: 56, b: 54},
		TabActiveBG:      rgb{r: 250, g: 189, b: 47},
		TabActiveFG:      rgb{r: 40, g: 40, b: 40},
		TabInactiveBG:    rgb{r: 60, g: 56, b: 54},
		TabInactiveFG:    rgb{r: 235, g: 219, b: 178},
		ErrorFG:          rgb{r: 251, g: 73, b: 52},
		StderrFG:         rgb{r: 211, g: 134, b: 155},
		MetaFG:           rgb{r: 146, g: 131, b: 116},
		PromptFG:         rgb{r: 255, g: 255, b: 255},
		SpinnerFG:        rgb{r: 131, g: 165, b: 152},
		ReasoningFG:      rgb{r: 131, g: 165, b: 152},
		ReasoningBold:    rgb{r: 214, g: 93, b: 14},
		CodeFG:           rgb{r: 250, g: 189, b: 47},
		AboutLinkFG:      rgb{r: 250, g: 189, b: 47},
		AboutCopyrightFG: rgb{r: 75, g: 110, b: 166},
		HelpArgFG:        rgb{r: 131, g: 165, b: 152},
	},
	"tokyo-midnight": {
		Name:             "tokyo-midnight",
		TabBarBG:         rgb{r: 26, g: 27, b: 38},
		TabActiveBG:      rgb{r: 122, g: 162, b: 247},
		TabActiveFG:      rgb{r: 26, g: 27, b: 38},
		TabInactiveBG:    rgb{r: 26, g: 27, b: 38},
		TabInactiveFG:    rgb{r: 192, g: 202, b: 245},
		ErrorFG:          rgb{r: 247, g: 118, b: 142},
		StderrFG:         rgb{r: 187, g: 154, b: 247},
		MetaFG:           rgb{r: 127, g: 133, b: 163},
		PromptFG:         rgb{r: 255, g: 255, b: 255},
		SpinnerFG:        rgb{r: 122, g: 162, b: 247},
		ReasoningFG:      rgb{r: 122, g: 162, b: 247},
		ReasoningBold:    rgb{r: 187, g: 154, b: 247},
		CodeFG:           rgb{r: 158, g: 206, b: 106},
		AboutLinkFG:      rgb{r: 122, g: 162, b: 247},
		AboutCopyrightFG: rgb{r: 59, g: 79, b: 159},
		HelpArgFG:        rgb{r: 125, g: 207, b: 255},
	},
}

func themeForName(name schema.ThemeName) tuiTheme {
	if name == "" {
		name = schema.DefaultTheme
	}
	if theme, ok := tuiThemes[name]; ok {
		return theme
	}
	return tuiThemes[schema.DefaultTheme]
}

func ansiFgRGB(c rgb) string {
	return "\x1b[38;2;" + strconv.Itoa(c.r) + ";" + strconv.Itoa(c.g) + ";" + strconv.Itoa(c.b) + "m"
}

func ansiBgRGB(c rgb) string {
	return "\x1b[48;2;" + strconv.Itoa(c.r) + ";" + strconv.Itoa(c.g) + ";" + strconv.Itoa(c.b) + "m"
}
