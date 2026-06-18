package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"operis.fr/docker-autoscaler/config"
	"operis.fr/docker-autoscaler/scaler"
	"operis.fr/docker-autoscaler/scaleset"
)

func main() {

	// Get config from env variable or env file
	// Configuration errors will be collected and printed at once, instead of failing fast on the first error.
	autoscalerConfig, configErrors := config.GetAutoscalerConfig()
	if len(configErrors) > 0 {
		errorString := "Configuration errors:\n"
		for _, err := range configErrors {
			errorString += fmt.Sprintf("- %s\n", err)
		}

		panic(errorString)
	}

	logger := autoscalerConfig.Logger()
	logger.Info("Starting Autoscaler...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := createScaleset(ctx, autoscalerConfig)
	if err != nil {
		logger.Error("Autoscaler failed", "error", err)
		os.Exit(1)
	}
}

func createScaleset(ctx context.Context, config *config.AutoscalerConfig) error {
	logger := config.Logger()

	// Create scaleset service
	scalesetService, err := scaleset.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create scaleset service: %w", err)
	}
	// Ensure scalesetService is closed when the function exits
	defer scalesetService.Close()

	// Create scaler service
	scalerService, err := scaler.New(ctx, scalesetService, config)
	if err != nil {
		return fmt.Errorf("failed to create scaler service: %w", err)
	}
	// Ensure scalerService is closed when the function exits
	defer scalerService.Close(context.WithoutCancel(ctx))

	listener, err := scalesetService.CreateListenner(ctx)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	logger.Info("Starting listener")
	// Run the listener and handle any errors that occur, except for context.Canceled which indicates a graceful shutdown
	if err := listener.Run(ctx, scalerService); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("listener run failed: %w", err)
	}
	return nil
}
