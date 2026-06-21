package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	githubScaleSet "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/google/uuid"
	"mgarnier11.fr/docker-autoscaler/config"
)

func New(ctx context.Context, logger *slog.Logger, scalesetClient *githubScaleSet.Client, config *config.ScaleSetConfig, imageParams *ImageParams) (*Scaler, error) {
	logger = logger.WithGroup("scaler").With("scaleSetName", config.ScaleSetName)

	runnerScaleSet, err := createRunnerScaleSet(context.Background(), config, scalesetClient, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner scale set: %w", err)
	}

	messageSessionClient, err := createMessageSessionClient(context.Background(), runnerScaleSet, scalesetClient, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create message session client: %w", err)
	}

	clients, err := createDockerClients(config.DockerHosts)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker clients: %w", err)
	}

	for _, client := range clients {
		if err := pullRunnerImage(
			ctx,
			client,
			imageParams,
		); err != nil {
			return nil, fmt.Errorf("failed to pull runner image: %w", err)
		}

		if err := createCacheVolumes(ctx, client); err != nil {
			return nil, fmt.Errorf("failed to create cache volumes: %w", err)
		}
	}

	listener, err := listener.New(messageSessionClient, listener.Config{
		ScaleSetID: runnerScaleSet.ID,
		MaxRunners: config.MaxRunners,
		Logger:     logger.WithGroup("listener"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	scaler := &Scaler{
		logger:               logger,
		scalesetClient:       scalesetClient,
		config:               config,
		imageParams:          imageParams,
		runnerScaleSet:       runnerScaleSet,
		messageSessionClient: messageSessionClient,
		dockerClients:        clients,
		listener:             listener,
		runners: runnerState{
			idle: make(map[string]runnerInfo),
			busy: make(map[string]runnerInfo),
		},
	}

	return scaler, nil
}

func createRunnerScaleSet(ctx context.Context, config *config.ScaleSetConfig, scalesetClient *githubScaleSet.Client, logger *slog.Logger) (*githubScaleSet.RunnerScaleSet, error) {
	// Get the runner group ID of the chosen runner group
	var runnerGroupID int
	if config.RunnerGroup == "default" {
		runnerGroupID = 1
	} else {
		runnerGroup, err := scalesetClient.GetRunnerGroupByName(ctx, config.RunnerGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to get runner group ID: %w", err)
		}
		runnerGroupID = runnerGroup.ID
	}
	logger.Info("Using runner group", slog.String("runnerGroup", config.RunnerGroup), slog.Int("runnerGroupID", runnerGroupID))

	// Get the runner scale set, create it if it doesn't exist
	scaleSet, err := scalesetClient.GetRunnerScaleSet(ctx, runnerGroupID, config.ScaleSetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get runner scale set: %w", err)
	}
	if scaleSet == nil {
		logger.Info("Runner scale set not found, creating a new one", slog.String("scaleSetName", config.ScaleSetName))

		labels := make([]githubScaleSet.Label, len(config.Labels))
		for j, name := range config.Labels {
			labels[j] = githubScaleSet.Label{Name: strings.TrimSpace(name)}
		}

		scaleSet, err = scalesetClient.CreateRunnerScaleSet(ctx, &githubScaleSet.RunnerScaleSet{
			Name:          config.ScaleSetName,
			RunnerGroupID: runnerGroupID,
			Labels:        labels,
			RunnerSetting: githubScaleSet.RunnerSetting{
				DisableUpdate: true,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create runner scale set: %w", err)
		}
		logger.Info("Created runner scale set", slog.String("scaleSetName", config.ScaleSetName), slog.Int("scaleSetID", scaleSet.ID))
	} else {
		logger.Info("Found existing runner scale set", slog.String("scaleSetName", config.ScaleSetName), slog.Int("scaleSetID", scaleSet.ID))
	}

	return scaleSet, nil
}

func createMessageSessionClient(ctx context.Context, runnerScaleSet *githubScaleSet.RunnerScaleSet, scalesetClient *githubScaleSet.Client, logger *slog.Logger) (*githubScaleSet.MessageSessionClient, error) {
	// Get the name of the client which will be used as the owner
	hostname, err := os.Hostname()
	if err != nil {
		hostname = uuid.NewString()
		logger.Info("Failed to get hostname, fallback to uuid", "uuid", hostname, "error", err)
	}

	if runnerScaleSet == nil {
		return nil, fmt.Errorf("runner scale set is not initialized")
	}

	// Create a message session client for the runner scale set
	sessionClient, err := scalesetClient.MessageSessionClient(ctx, runnerScaleSet.ID, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to create message session client: %w", err)
	}
	return sessionClient, nil
}
