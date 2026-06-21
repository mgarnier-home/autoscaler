package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
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

	logger.Info("Creating scaleset client with personal access token")
	scaleSetClient, err := githubScaleSet.NewClientWithPersonalAccessToken(
		githubScaleSet.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     autoscalerConfig.RegistrationURL,
			PersonalAccessToken: autoscalerConfig.Token,
			SystemInfo:          systemInfo(),
		},
	)
	if err != nil {
		logger.Error("Failed to create scaleset client", "error", err)
		os.Exit(1)
	}

	var (
		// On crée un WaitGroup pour attendre que toutes les goroutines se terminent avant de quitter le programme.
		wg      sync.WaitGroup
		scalers []*scaler.Scaler
		// On crée un channel pour collecter les erreurs des goroutines.
		errCh = make(chan error, len(autoscalerConfig.ScaleSetsConfigs))
	)

	for _, scaleSetConfig := range autoscalerConfig.ScaleSetsConfigs {
		sc, err := scaler.New(runCtx, logger, scaleSetClient, &scaleSetConfig, &scaler.ImageParams{
			RegistryURL:      autoscalerConfig.RegistryURL,
			RegistryUsername: autoscalerConfig.RegistryUsername,
			RegistryPassword: autoscalerConfig.RegistryPassword,
			RunnerImage:      autoscalerConfig.RunnerImage,
			ArtifactoryToken: autoscalerConfig.ArtifactoryToken,
		})
		if err != nil {
			logger.Error("Failed to create scaler service", "error", err)
			os.Exit(1)
		}

		scalers = append(scalers, sc)

		// Démarre une goroutine pour chaque scaler. Chaque goroutine exécute le scaler et envoie toute erreur sur le channel d'erreurs.
		wg.Add(1)
		go func(sc *scaler.Scaler) {
			defer wg.Done()

			// Démarre le scaler, et si une erreur survient, on l'envoie sur le channel d'erreurs et on annule le contexte pour arrêter les autres scalers.
			if err := sc.Run(runCtx); err != nil && runCtx.Err() == nil {
				errCh <- err
				cancel()
			}
		}(sc)
	}

	// Crée un channel pour signaler que toutes les goroutines ont terminé.
	allDone := make(chan struct{})
	go func() {
		// Attend que toutes les goroutines se terminent.
		wg.Wait()
		// Ferme le channel allDone pour signaler que toutes les goroutines ont terminé.
		close(allDone)
	}()

	// Attend soit un signal d'arrêt, soit une erreur d'une des goroutines, soit que toutes les goroutines se terminent.
	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("Stop signal received, waiting for scaler goroutines to finish")
		// Annule le contexte pour signaler aux goroutines de s'arrêter.
		cancel()
	case runErr = <-errCh:
		logger.Error("A scaler service failed", "error", runErr)
		// Annule le contexte pour signaler aux goroutines de s'arrêter.
		cancel()
	case <-allDone:
	}
	// Attend que toutes les goroutines se terminent avant de continuer.
	<-allDone

	// On appelle Shutdown sur tous les scalers pour s'assurer que toutes les ressources sont correctement libérées.
	shutdownCtx := context.WithoutCancel(ctx)
	for _, sc := range scalers {
		sc.Shutdown(shutdownCtx)
	}

	// On vérifie si une erreur est survenue dans l'une des goroutines. Si c'est le cas, on quitte avec un code d'erreur.
	if runErr != nil {
		os.Exit(1)
	}
}
