package core

import "context"

// UsageWindow captures account usage for a single window.
type UsageWindow struct {
	UsedPercent        float64
	LimitWindowSeconds int64
	ResetAt            int64
}

// UsageInfo captures rate limit usage for a ChatGPT account.
type UsageInfo struct {
	ChatGPT   bool
	Primary   *UsageWindow
	Secondary *UsageWindow
}

// UsageReader fetches account usage information.
type UsageReader interface {
	Usage(ctx context.Context) (UsageInfo, error)
}
