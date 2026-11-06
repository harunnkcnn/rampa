package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/harunnkcnn/rampa/store"
)

// Options lets a distribution (e.g. the commercial build) customise Main.
type Options struct {
	Edition    string
	Extensions func(st *store.Store, log *slog.Logger) ([]Extension, error)
}

// Main parses flags/env, opens the database and serves until interrupted.
// Every flag has an environment variable so the binary also suits Docker.
func Main(ctx context.Context, args []string, opts Options) error {
	fs := flag.NewFlagSet("rampa", flag.ContinueOnError)
	addr := fs.String("addr", envOr("RAMPA_ADDR", "127.0.0.1:8080"), "listen address (RAMPA_ADDR)")
	dbPath := fs.String("db", envOr("RAMPA_DB", "rampa.db"), "SQLite database file (RAMPA_DB)")
	secure := fs.Bool("secure-cookie", envBool("RAMPA_SECURE_COOKIE"), "mark cookies Secure; set when behind HTTPS (RAMPA_SECURE_COOKIE)")
	version := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version {
		fmt.Println(Version)
		return nil
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	var exts []Extension
	if opts.Extensions != nil {
		if exts, err = opts.Extensions(st, log); err != nil {
			return err
		}
	}
	srv, err := New(ctx, Config{
		Addr: *addr, DBPath: *dbPath, SecureCookie: *secure, Edition: opts.Edition,
	}, st, log, exts...)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()
	log.Info("Rampa hazır", "adres", "http://"+ln.Addr().String(), "veritabanı", *dbPath, "sürüm", Version)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("kapatılıyor")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
	return nil
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string) bool {
	b, _ := strconv.ParseBool(os.Getenv(key))
	return b
}
