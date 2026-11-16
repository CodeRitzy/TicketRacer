// Command api runs the Ticketracer HTTP API and its background loops.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"ticketracer/internal/api"
	"ticketracer/internal/config"
	"ticketracer/internal/pay"
	"ticketracer/internal/queue"
	"ticketracer/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// local runs read ../.env; in Docker the real environment wins
	config.LoadDotEnv("../.env")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	ropt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(ropt)
	defer rdb.Close()
	q := queue.New(rdb, cfg.AdmittedTTL)

	var provider pay.Provider = pay.Demo{}
	if cfg.PaymentMode == "stripe" {
		provider = pay.NewStripe(cfg.StripeSecretKey)
	}
	if cfg.AdminToken == "" {
		log.Warn("ADMIN_TOKEN is empty: dashboard and simulator are disabled")
	}

	srv := api.New(cfg, st, q, provider, log)
	srv.RunBackground(ctx)

	// timeouts stop slow clients from tying up connections; SSE lifts its own write deadline
	httpSrv := &http.Server{
		Addr: cfg.Addr, Handler: srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// graceful shutdown: finish in-flight requests, up to 10 seconds
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutCtx)
	}()
	log.Info("listening", "addr", cfg.Addr, "payment_mode", cfg.PaymentMode)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
