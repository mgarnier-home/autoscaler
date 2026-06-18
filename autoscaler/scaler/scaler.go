package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	githubScaleset "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/google/uuid"
	"operis.fr/docker-autoscaler/config"
	"operis.fr/docker-autoscaler/scaleset"
)

type runnerInfo struct {
	containerID  string
	dockerClient *dockerclient.Client
}

type Service struct {
	logger                *slog.Logger
	config                *config.AutoscalerConfig
	scalesetService       *scaleset.Service
	runners               runnerState
	dockerClients         []*dockerclient.Client
	nextDockerClientIndex int
	dockerClientMutex     sync.Mutex
}

func New(ctx context.Context, scalesetService *scaleset.Service, config *config.AutoscalerConfig) (*Service, error) {
	clients, err := createDockerClients(config.DockerHosts)
	if err != nil {
		return nil, err
	}

	for _, client := range clients {
		if err := pullRunnerImage(ctx, client, config); err != nil {
			return nil, fmt.Errorf("failed to pull runner image: %w", err)
		}

		if err := createCacheVolumes(ctx, client); err != nil {
			return nil, fmt.Errorf("failed to create cache volumes: %w", err)
		}
	}

	return &Service{
		logger:                config.Logger().WithGroup("scaler"),
		dockerClients:         clients,
		nextDockerClientIndex: 0,
		config:                config,
		scalesetService:       scalesetService,
		runners: runnerState{
			idle: make(map[string]runnerInfo),
			busy: make(map[string]runnerInfo),
		},
	}, nil
}

func (service *Service) Close(ctx context.Context) {
	service.logger.Info("Closing scaler service")
	service.Shutdown(ctx)

	for _, client := range service.dockerClients {
		if err := client.Close(); err != nil {
			service.logger.Error("Failed to close docker client", slog.String("dockerHost", client.DaemonHost()), slog.String("error", err.Error()))
		}
	}
}

func (service *Service) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	currentCount := service.runners.count()
	targetRunnerCount := min(service.config.MaxRunners, service.config.MinRunners+count)

	switch {
	case targetRunnerCount == currentCount:
		// No scaling needed
		return currentCount, nil
	case targetRunnerCount > currentCount:
		// Scale up
		scaleUp := targetRunnerCount - currentCount
		service.logger.Info(
			"Scaling up runners",
			slog.Int("currentCount", currentCount),
			slog.Int("desiredCount", targetRunnerCount),
			slog.Int("scaleUp", scaleUp),
		)

		for range scaleUp {
			if _, err := service.startRunner(ctx); err != nil {
				service.logger.Error("Failed to start runner", slog.String("error", err.Error()))
				return 0, nil
			}
		}

		return service.runners.count(), nil
	default:
		// No need to handle scale down events, since:
		// 1. JobCompleted events will first remove runners
		// 2. If the count is still below the current runner count, the JobCompleted event will be delivered in the next batch.
		// 3. Removal after JobCompleted events is handled synchronously.
		// 4. If the job is cancelled, the JobCompleted event will still be delivered.
	}
	return service.runners.count(), nil
}

func (service *Service) HandleJobStarted(ctx context.Context, jobInfo *githubScaleset.JobStarted) error {
	service.logger.Info(
		"Job started",
		slog.Int64("runnerRequestId", jobInfo.RunnerRequestID),
		slog.String("jobId", jobInfo.JobID),
	)
	service.runners.markBusy(jobInfo.RunnerName)
	return nil
}

func (service *Service) HandleJobCompleted(ctx context.Context, jobInfo *githubScaleset.JobCompleted) error {
	service.logger.Info("Job completed", slog.Int64("runnerRequestId", jobInfo.RunnerRequestID), slog.String("jobId", jobInfo.JobID))

	info := service.runners.markDone(jobInfo.RunnerName)
	if err := info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true}); err != nil {
		service.logger.Error(
			"Failed to remove runner container",
			slog.String("name", jobInfo.RunnerName),
			slog.String("containerID", info.containerID),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (service *Service) startRunner(ctx context.Context) (string, error) {
	containerName := fmt.Sprintf("runner-%s", uuid.NewString()[:8])

	jit, err := service.scalesetService.GenerateJitRunnerConfig(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to generate JIT config: %w", err)
	}

	// Select the next Docker client in a round-robin fashion
	service.dockerClientMutex.Lock()
	client := service.dockerClients[service.nextDockerClientIndex]
	service.logger.Info("Selected docker client", slog.String("dockerHost", client.DaemonHost()), slog.Int("clientIndex", service.nextDockerClientIndex))
	service.nextDockerClientIndex = (service.nextDockerClientIndex + 1) % len(service.dockerClients)
	service.dockerClientMutex.Unlock()

	containerID, err := service.startRunnerContainer(
		ctx,
		client,
		containerName,
		jit,
	)
	if err != nil {
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	service.runners.addIdle(containerName, runnerInfo{
		containerID:  containerID,
		dockerClient: client,
	})

	return containerName, nil
}

func (service *Service) Shutdown(ctx context.Context) {
	service.logger.Info("Shutting down runners")
	service.runners.mu.Lock()
	defer service.runners.mu.Unlock()

	for name, info := range service.runners.idle {
		service.logger.Info("Removing idle runner", slog.String("name", name), slog.String("containerID", info.containerID))
		if err := info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true}); err != nil {
			service.logger.Error("Failed to remove idle runner container", slog.String("name", name), slog.String("containerID", info.containerID), slog.String("error", err.Error()))
		}
	}
	clear(service.runners.idle)

	for name, info := range service.runners.busy {
		service.logger.Info("Removing busy runner", slog.String("name", name), slog.String("containerID", info.containerID))
		if err := info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true}); err != nil {
			service.logger.Error("Failed to remove busy runner container", slog.String("name", name), slog.String("containerID", info.containerID), slog.String("error", err.Error()))
		}
	}
	clear(service.runners.busy)
}

var _ listener.Scaler = (*Service)(nil)
