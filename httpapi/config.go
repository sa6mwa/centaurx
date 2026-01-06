package httpapi

// Config defines HTTP API and UI settings.
type Config struct {
	Addr               string
	SessionCookie      string
	SessionTTLHours    int
	BaseURL            string
	BasePath           string
	InitialBufferLines int
	UIMaxBufferLines   int
}
