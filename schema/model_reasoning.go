package schema

import (
	"fmt"
	"strings"
)

// ModelReasoningLow and related constants define allowed reasoning effort values.
const (
	ModelReasoningLow    ModelReasoningEffort = "low"
	ModelReasoningMedium ModelReasoningEffort = "medium"
	ModelReasoningHigh   ModelReasoningEffort = "high"
	ModelReasoningXHigh  ModelReasoningEffort = "xhigh"
)

// DefaultModelReasoningEffort is the default reasoning effort when none is specified.
const DefaultModelReasoningEffort ModelReasoningEffort = ModelReasoningMedium

// FormatModelWithReasoning formats a model label that includes reasoning effort.
func FormatModelWithReasoning(model ModelID, effort ModelReasoningEffort) string {
	name := strings.TrimSpace(string(model))
	if name == "" {
		name = "unknown"
	}
	reasoning := strings.TrimSpace(string(effort))
	if reasoning == "" {
		reasoning = string(DefaultModelReasoningEffort)
	}
	return fmt.Sprintf("%s (reasoning %s)", name, reasoning)
}
