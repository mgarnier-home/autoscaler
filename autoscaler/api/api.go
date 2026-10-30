package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mgarnier-home/utils/version"
	"mgarnier11.fr/docker-autoscaler/scaler"
)

// NewHandler sert GET /stats, lu par le dashboard homepage, et GET /version.
func NewHandler(stats func() scaler.Stats) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats())
	})
	mux.Handle("GET /version", version.Handler())
	return mux
}

// Serve écoute sur port jusqu'à l'annulation de ctx. Les stats ne sont pas
// vitales : une erreur d'écoute est journalisée sans arrêter l'autoscaler.
func Serve(ctx context.Context, logger *slog.Logger, port int, handler http.Handler) {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("Starting stats API", slog.Int("port", port))
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("Stats API stopped", slog.String("error", err.Error()))
	}
}
