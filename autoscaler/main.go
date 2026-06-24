package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mgarnier11.fr/docker-autoscaler/config"
	"mgarnier11.fr/docker-autoscaler/scaler"

	githubScaleSet "github.com/actions/scaleset"
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
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := autoscaler(runCtx, autoscalerConfig); err != nil {
		logger.Error("Autoscaler encountered an error", "error", err)
		os.Exit(1)
	}

}

func autoscaler(ctx context.Context, config *config.AutoscalerConfig) error {
	logger := config.Logger()

	logger.Info("Creating scaleset client with personal access token")
	scaleSetClient, err := githubScaleSet.NewClientWithPersonalAccessToken(
		githubScaleSet.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     config.RegistrationURL,
			PersonalAccessToken: config.Token,
			SystemInfo:          systemInfo(),
		},
	)
	if err != nil {
		logger.Error("Failed to create scaleset client", "error", err)
		os.Exit(1)
	}

	sc, err := scaler.New(ctx, logger, scaleSetClient, config)
	if err != nil {
		logger.Error("Failed to create scaler service", "error", err)
		os.Exit(1)
	}
	defer sc.Shutdown(context.WithoutCancel(ctx))

	err = sc.Run(ctx)
	if err := sc.Run(ctx); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("listener run failed: %w", err)
	}
	logger.Info("Scaler service stopped")

	return nil
}
