package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	githubScaleSet "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
	"mgarnier11.fr/docker-autoscaler/config"
)

type Scaler struct {
	logger         *slog.Logger
	scalesetClient *githubScaleSet.Client
	config         *config.AutoscalerConfig

	runnerScaleSet       *githubScaleSet.RunnerScaleSet
	messageSessionClient *githubScaleSet.MessageSessionClient
	listener             *listener.Listener

	runners runnerState

	nextDockerClientIndex int
	dockerClientMutex     sync.Mutex
	dockerClients         []*DockerClientWithMetadata
}

type ImageParams struct {
	RegistryURL       string
	RegistryUsername  string
	RegistryPassword  string
	RegistryMirrorURL string
	RunnerImage       string
	ArtifactoryToken  string
}

func (this *Scaler) Run(ctx context.Context) error {
	this.logger.Info("Starting listener for runner scale set", slog.Int("scaleSetID", this.runnerScaleSet.ID))

	return this.listener.Run(ctx, this)
}

func (this *Scaler) Shutdown(ctx context.Context) {
	// Shutdown all the runners
	this.logger.Info("Shutting down runners")
	this.runners.mu.Lock()
	for name, info := range this.runners.idle {
		this.logger.Info("Removing idle runner", slog.String("name", name), slog.String("containerID", info.containerID))
		if err := info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true}); err != nil {
			this.logger.Error("Failed to remove idle runner container", slog.String("name", name), slog.String("containerID", info.containerID), slog.String("error", err.Error()))
		}
	}
	clear(this.runners.idle)

	for name, info := range this.runners.busy {
		this.logger.Info("Removing busy runner", slog.String("name", name), slog.String("containerID", info.containerID))
		if err := info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true}); err != nil {
			this.logger.Error("Failed to remove busy runner container", slog.String("name", name), slog.String("containerID", info.containerID), slog.String("error", err.Error()))
		}
	}
	clear(this.runners.busy)
	this.runners.mu.Unlock()

	// Close the docker clients
	for _, client := range this.dockerClients {
		if err := client.Close(); err != nil {
			this.logger.Error(
				"Failed to close docker client",
				slog.String("dockerHost", client.DaemonHost()),
				slog.String("error", err.Error()),
			)
		}
	}

	// Close the message session client
	this.messageSessionClient.Close(ctx)

	// Delete the runner scale set
	this.logger.Info(
		"Deleting runner scale set",
		slog.Int("scaleSetID", this.runnerScaleSet.ID),
	)
	if err := this.scalesetClient.DeleteRunnerScaleSet(context.WithoutCancel(ctx), this.runnerScaleSet.ID); err != nil {
		this.logger.Error(
			"Failed to delete runner scale set",
			slog.Int("scaleSetID", this.runnerScaleSet.ID),
			slog.String("error", err.Error()),
		)
	}
}

func (this *Scaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	currentCount := this.runners.count()
	targetRunnerCount := min(this.config.MaxRunners, this.config.MinRunners+count)

	switch {
	case targetRunnerCount == currentCount:
		// No scaling needed
		return currentCount, nil
	case targetRunnerCount > currentCount:
		// Scale up
		scaleUp := targetRunnerCount - currentCount
		this.logger.Info(
			"Scaling up runners",
			slog.Int("currentCount", currentCount),
			slog.Int("desiredCount", targetRunnerCount),
			slog.Int("scaleUp", scaleUp),
		)

		for range scaleUp {
			if _, err := this.startRunner(ctx); err != nil {
				this.logger.Error("Failed to start runner", slog.String("error", err.Error()))
				return 0, nil
			}
		}

		return this.runners.count(), nil
	default:
		// No need to handle scale down events, since:
		// 1. JobCompleted events will first remove runners
		// 2. If the count is still below the current runner count, the JobCompleted event will be delivered in the next batch.
		// 3. Removal after JobCompleted events is handled synchronously.
		// 4. If the job is cancelled, the JobCompleted event will still be delivered.
	}
	return this.runners.count(), nil
}

func (this *Scaler) HandleJobStarted(ctx context.Context, jobInfo *githubScaleSet.JobStarted) error {
	this.logger.Info(
		"Job started",
		slog.Int64("runnerRequestId", jobInfo.RunnerRequestID),
		slog.String("jobId", jobInfo.JobID),
	)

	err := this.runners.markBusy(jobInfo.RunnerName)
	if err != nil {
		this.logger.Error(
			"Failed to mark runner busy",
			slog.String("name", jobInfo.RunnerName),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (this *Scaler) HandleJobCompleted(ctx context.Context, jobInfo *githubScaleSet.JobCompleted) error {
	this.logger.Info("Job completed", slog.Int64("runnerRequestId", jobInfo.RunnerRequestID), slog.String("jobId", jobInfo.JobID))

	info, err := this.runners.markDone(jobInfo.RunnerName)
	if err != nil {
		this.logger.Error(
			"Failed to mark runner done",
			slog.String("name", jobInfo.RunnerName),
			slog.String("error", err.Error()),
		)
	}

	err = info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true})
	if err != nil {
		this.logger.Error(
			"Failed to remove runner container",
			slog.String("name", jobInfo.RunnerName),
			slog.String("containerID", info.containerID),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (this *Scaler) startRunner(ctx context.Context) (string, error) {
	containerName := fmt.Sprintf("runner-%s", uuid.NewString()[:8])

	jit, err := this.GenerateJitRunnerConfig(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to generate JIT config: %w", err)
	}

	// Select the next Docker client in a round-robin fashion
	this.dockerClientMutex.Lock()
	client := this.dockerClients[this.nextDockerClientIndex]
	this.logger.Info(
		"Selected docker client",
		slog.String("dockerHost", client.DaemonHost()),
		slog.Int("clientIndex", this.nextDockerClientIndex),
	)
	this.nextDockerClientIndex = (this.nextDockerClientIndex + 1) % len(this.dockerClients)
	this.dockerClientMutex.Unlock()

	containerID, err := startRunnerContainer(
		ctx,
		client,
		&startContainerParams{
			containerName:     containerName,
			jitConfig:         jit,
			registryURL:       this.config.RegistryURL,
			registryUsername:  this.config.RegistryUsername,
			registryPassword:  this.config.RegistryPassword,
			runnerImage:       this.config.RunnerImage,
			artifactoryToken:  this.config.ArtifactoryToken,
			registryMirrorURL: this.config.RegistryMirrorURL,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	this.runners.addIdle(containerName, runnerInfo{
		containerID:  containerID,
		dockerClient: client,
	})

	return containerName, nil
}

func (this *Scaler) GenerateJitRunnerConfig(ctx context.Context, containerName string) (*githubScaleSet.RunnerScaleSetJitRunnerConfig, error) {
	// Generate JIT config for the runner
	jit, err := this.scalesetClient.GenerateJitRunnerConfig(
		ctx,
		&githubScaleSet.RunnerScaleSetJitRunnerSetting{
			Name: containerName,
		},
		this.runnerScaleSet.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JIT config: %w", err)
	}

	return jit, nil
}

var _ listener.Scaler = (*Scaler)(nil)
