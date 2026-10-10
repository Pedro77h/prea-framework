package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"prea/logger"
)

// Server configures the HTTP server. Zero fields use the defaults below.
type Server struct {
	ReadHeaderTimeout time.Duration // default 5s; protects against slowloris
	ReadTimeout       time.Duration // default 30s; whole request, body included
	WriteTimeout      time.Duration // default 30s; raise it for streaming or slow downloads
	IdleTimeout       time.Duration // default 120s; keep-alive connections
	ShutdownTimeout   time.Duration // default 15s; time in-flight requests get to finish
	MaxHeaderBytes    int           // default 1 MB
}

func (s Server) http(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: cmp.Or(s.ReadHeaderTimeout, 5*time.Second),
		ReadTimeout:       cmp.Or(s.ReadTimeout, 30*time.Second),
		WriteTimeout:      cmp.Or(s.WriteTimeout, 30*time.Second),
		IdleTimeout:       cmp.Or(s.IdleTimeout, 120*time.Second),
		MaxHeaderBytes:    cmp.Or(s.MaxHeaderBytes, 1<<20),
	}
}

// Run serves on addr until SIGINT or SIGTERM, then shuts down gracefully.
// A second signal stops the process immediately.
func (a App) Run(addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		stop()
	}()
	return a.Serve(ctx, ln)
}

// Serve serves on ln until ctx is done. It stops accepting connections, waits
// up to Server.ShutdownTimeout for in-flight requests, closes the components
// and only then returns, so resources closed after it (like the database
// pool) are no longer in use.
func (a App) Serve(ctx context.Context, ln net.Listener) error {
	engine, closeComponents, err := a.Build(ctx)
	if err != nil {
		ln.Close()
		return err
	}
	timeout := cmp.Or(a.Server.ShutdownTimeout, 15*time.Second)

	err = a.serve(ctx, a.Server.http(engine), ln, timeout)

	closing, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := errors.Join(err, closeComponents(closing)); err != nil {
		return err
	}
	logger.Success("prea: stopped")
	return nil
}

func (a App) serve(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.Info("prea: listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	logger.Info("prea: shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		// Closing the connections cancels the request contexts, so handlers
		// that use c.Request.Context() stop their queries.
		srv.Close()
		return fmt.Errorf("prea: shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
