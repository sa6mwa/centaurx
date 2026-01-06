package httpapi

import (
	"net/http"
	"strings"
	"time"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

type responseRecorder struct {
	status int
	bytes  int64
	writer http.ResponseWriter
}

func (r *responseRecorder) Header() http.Header {
	return r.writer.Header()
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.writer.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.writer.Write(p)
	r.bytes += int64(n)
	return n, err
}

func (r *responseRecorder) Flush() {
	if f, ok := r.writer.(http.Flusher); ok {
		f.Flush()
	}
}

type sessionLookupFunc func(*http.Request) (userID schema.UserID, sessionID string)

func withRequestLogging(next http.Handler, lookup sessionLookupFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var userID schema.UserID
		var sessionID string
		if lookup != nil {
			userID, sessionID = lookup(r)
		}
		rec := &responseRecorder{writer: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path = path + "?" + r.URL.RawQuery
		}
		logger := pslog.Ctx(r.Context()).With("remote", clientIP(r))
		if userID != "" {
			logger = logger.With("user", userID)
		}
		if sessionID != "" {
			logger = logger.With("http_session", sessionID)
		}
		logger.Info("http request", "method", r.Method, "path", path, "status", status, "bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds())
		logger.Debug("http request details", "ua", r.UserAgent())
	})
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	return r.RemoteAddr
}
