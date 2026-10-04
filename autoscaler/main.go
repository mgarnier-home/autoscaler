package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mgarnier11.fr/docker-autoscaler/config"
	"mgarnier11.fr/docker-autoscaler/github"
	"mgarnier11.fr/docker-autoscaler/scaler"

	githubScaleSet "github.com/actions/scaleset"
	utilsConfig "github.com/mgarnier-home/utils/config"
)

// systemInfo serves as a base system info
func systemInfo() githubScaleSet.SystemInfo {
	return githubScaleSet.SystemInfo{
		System:    "dockerscaleset",
		Subsystem: "dockerscaleset",
		CommitSHA: "NA",    // TODO: passer ce parametre au build
		Version:   "0.1.0", // TODO: passer ce parametre au build
	}
}

func main() {

	autoscalerConfig := &config.AutoscalerConfig{}

	errors := utilsConfig.GetConfig(autoscalerConfig)

	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		}
		os.Exit(1)
	}

	logger := autoscalerConfig.Logger()
	logger.Info("Starting Autoscaler...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := autoscaler(runCtx, autoscalerConfig); err != nil {
		logger.Error("Autoscaler encountered an error", "error", err)
		os.Exit(1)
	}

}

func autoscaler(ctx context.Context, config *config.AutoscalerConfig) error {
	logger := config.Logger()

	logger.Info("Creating github client with personal access token")
	githubClient, err := github.New(logger, config)
	if err != nil {
		logger.Error("Failed to create github client", "error", err)
		os.Exit(1)
	}

	sc, err := scaler.New(ctx, logger, githubClient, config)
	if err != nil {
		logger.Error("Failed to create scaler service", "error", err)
		os.Exit(1)
	}
	// L'arrêt doit pouvoir parler à github et docker alors même que le contexte
	// vient d'être annulé par le signal : sans WithoutCancel, aucun runner libre
	// ne serait désenregistré ni supprimé.
	defer sc.Shutdown(context.WithoutCancel(ctx))

	if err := sc.Run(ctx); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("listener run failed: %w", err)
	}
	logger.Info("Scaler service stopped")

	return nil
}
