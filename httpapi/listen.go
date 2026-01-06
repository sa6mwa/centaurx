package httpapi

import (
	"context"
	"net"
	"net/http"

	"pkt.systems/pslog"
)

// ListenAndServe starts an HTTP server and shuts it down on context cancellation.
func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	logger := pslog.Ctx(ctx)
	server := &http.Server{
		Addr:     addr,
		Handler:  handler,
		ErrorLog: pslog.LogLoggerWithLevel(logger, pslog.ErrorLevel),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}
