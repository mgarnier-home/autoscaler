package scaler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/actions/scaleset/listener"
	"mgarnier11.fr/docker-autoscaler/config"
	"mgarnier11.fr/docker-autoscaler/docker"
	"mgarnier11.fr/docker-autoscaler/github"
)

func New(ctx context.Context, logger *slog.Logger, githubClient *github.GithubClient, config *config.AutoscalerConfig) (*Scaler, error) {
	logger.Info("Starting scaler", "scaleSetName", config.ScaleSetName)

	if err := githubClient.EnsureScaleSet(ctx); err != nil {
		return nil, fmt.Errorf("failed to create runner scale set: %w", err)
	}

	if err := githubClient.OpenMessageSession(ctx); err != nil {
		return nil, fmt.Errorf("failed to create message session client: %w", err)
	}

	dockerPool, err := docker.NewPool(logger, config.DockerHosts, config.Runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker clients: %w", err)
	}

	if err := dockerPool.PullRunnerImage(ctx, &docker.PullImageParams{
		RegistryURL:      config.RegistryURL,
		RegistryUsername: config.RegistryUsername,
		RegistryPassword: config.RegistryPassword,
		RunnerImage:      config.RunnerImage,
	}); err != nil {
		return nil, fmt.Errorf("failed to pull runner image: %w", err)
	}

	if err := dockerPool.CreateCacheVolumes(ctx); err != nil {
		return nil, fmt.Errorf("failed to create cache volumes: %w", err)
	}

	// Runners repris d'une session précédente : l'autoscaler peut avoir été
	// arrêté alors que des conteneurs tournaient encore.
	runnerContainers, err := dockerPool.ListRunners(ctx, config.ScaleSetName)
	if err != nil {
		return nil, fmt.Errorf("failed to recover runner containers: %w", err)
	}
	recovered := recoverRunners(logger, runnerContainers)

	listener, err := listener.New(githubClient.MessageSession(), listener.Config{
		ScaleSetID: githubClient.ScaleSetID(),
		MaxRunners: config.MaxRunners,
		Logger:     logger.WithGroup("listener"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	scaler := &Scaler{
		logger:       logger,
		githubClient: githubClient,
		config:       config,
		dockerPool:   dockerPool,
		listener:     listener,
		runners: runnerState{
			// Les runners repris sont tous placés en idle : ni docker ni github
			// ne disent lequel exécute un job — les statistiques du scale set
			// sont agrégées, et le per-runner de l'API ne porte pas cette
			// information. C'est sans danger : la sonde de l'arrêt interroge
			// github runner par runner, et un « idle » qui travaille répondra
			// ErrJobStillRunning et survivra.
			idle: recovered,
			busy: make(map[string]runnerInfo),
		},
	}

	return scaler, nil
}

// recoverRunners traduit en état interne les conteneurs runner retrouvés sur les
// hôtes docker.
//
// Docker est la source de vérité, et les labels du conteneur portent tout ce que
// runnerInfo contient. Un fichier d'état serait une seconde source, capable de
// diverger dans les deux sens : conteneur supprimé par AutoRemove pendant l'arrêt
// (entrée fantôme), ou créé juste avant un crash (entrée manquante). Un label ne
// peut pas décrire un conteneur qui n'existe plus.
func recoverRunners(logger *slog.Logger, containers []docker.RunnerContainer) map[string]runnerInfo {
	runners := make(map[string]runnerInfo, len(containers))

	for _, runnerContainer := range containers {
		logger.Info(
			"Recovered runner container",
			slog.String("name", runnerContainer.Name),
			slog.String("containerID", docker.ShortID(runnerContainer.ID)),
			slog.String("dockerHost", runnerContainer.Client.DaemonHost()),
		)
		runners[runnerContainer.Name] = runnerInfo{
			containerID:  runnerContainer.ID,
			dockerClient: runnerContainer.Client,
			runnerID:     runnerContainer.RunnerID,
		}
	}

	return runners
}
