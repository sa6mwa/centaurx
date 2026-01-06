package schema

// StderrMarker prefixes lines that originated from stderr.
const StderrMarker = "\x1f"

// AgentMarker prefixes agent message lines (markdown-enabled).
const AgentMarker = "\x1c"

// ReasoningMarker prefixes reasoning lines (markdown-enabled).
const ReasoningMarker = "\x1d"

// CommandMarker prefixes command execution lines (no markdown).
const CommandMarker = "\x1a"

// WorkedForMarker prefixes "worked for" separator lines.
const WorkedForMarker = "\x1e"

// HelpMarker prefixes markdown-enabled help lines.
const HelpMarker = "\x16"

// AboutVersionMarker prefixes the version line in /version output.
const AboutVersionMarker = "\x17"

// AboutCopyrightMarker prefixes the copyright line in /version output.
const AboutCopyrightMarker = "\x18"

// AboutLinkMarker prefixes the link line in /version output.
const AboutLinkMarker = "\x19"
