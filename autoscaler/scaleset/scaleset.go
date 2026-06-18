package scaleset

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	githubScaleset "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/google/uuid"

	"operis.fr/docker-autoscaler/config"
)

type Service struct {
	logger               *slog.Logger
	scalesetClient       *githubScaleset.Client
	config               *config.AutoscalerConfig
	runnerScaleset       *githubScaleset.RunnerScaleSet
	messageSessionClient *githubScaleset.MessageSessionClient
}

func New(ctx context.Context, config *config.AutoscalerConfig) (*Service, error) {
	logger := config.Logger().WithGroup("scaleset")

	// Create a scaleset client using the token auth
	logger.Info("Creating scaleset client with personal access token")
	scalesetClient, err := githubScaleset.NewClientWithPersonalAccessToken(
		githubScaleset.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     config.RegistrationURL,
			PersonalAccessToken: config.Token,
			SystemInfo:          systemInfo(0),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scaleset client: %w", err)
	}

	service := &Service{
		logger:         logger,
		scalesetClient: scalesetClient,
		config:         config,
	}

	err = service.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize scaleset service: %w", err)
	}

	return service, nil
}

// systemInfo serves as a base system info
func systemInfo(scaleSetID int) githubScaleset.SystemInfo {
	return githubScaleset.SystemInfo{
		System:     "dockerscaleset",
		Subsystem:  "dockerscaleset",
		CommitSHA:  "NA",    // TODO: passer ce parametre au build
		Version:    "0.1.0", // TODO: passer ce parametre au build
		ScaleSetID: scaleSetID,
	}
}

func (service *Service) Init() error {
	runnerScaleset, err := service.createRunnerScaleSet(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create runner scale set: %w", err)
	}
	service.runnerScaleset = runnerScaleset

	messageSessionClient, err := service.createMessageSessionClient(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create message session client: %w", err)
	}
	service.messageSessionClient = messageSessionClient

	return nil
}

func (service *Service) Close() {
	if service.messageSessionClient != nil {
		service.messageSessionClient.Close(context.Background())
	}
	if service.runnerScaleset != nil {
		service.deleteRunnerScaleSet(context.Background(), service.runnerScaleset.ID)
	}
}

func (service *Service) GenerateJitRunnerConfig(ctx context.Context, containerName string) (*githubScaleset.RunnerScaleSetJitRunnerConfig, error) {
	// Generate JIT config for the runner
	jit, err := service.scalesetClient.GenerateJitRunnerConfig(
		ctx,
		&githubScaleset.RunnerScaleSetJitRunnerSetting{
			Name: containerName,
		},
		service.runnerScaleset.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JIT config: %w", err)
	}

	return jit, nil
}

func (service *Service) CreateListenner(ctx context.Context) (*listener.Listener, error) {
	service.logger.Info("Initializing listener")
	listener, err := listener.New(service.messageSessionClient, listener.Config{
		ScaleSetID: service.runnerScaleset.ID,
		MaxRunners: service.config.MaxRunners,
		Logger:     service.logger.WithGroup("listener"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}
	return listener, nil
}

func (service *Service) createRunnerScaleSet(ctx context.Context) (*githubScaleset.RunnerScaleSet, error) {
	// Get the runner group ID of the chosen runner group
	var runnerGroupID int
	if service.config.RunnerGroup == "default" {
		runnerGroupID = 1
	} else {
		runnerGroup, err := service.scalesetClient.GetRunnerGroupByName(ctx, service.config.RunnerGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to get runner group ID: %w", err)
		}
		runnerGroupID = runnerGroup.ID
	}
	service.logger.Info("Using runner group", slog.String("runnerGroup", service.config.RunnerGroup), slog.Int("runnerGroupID", runnerGroupID))

	// Get the runner scale set, create it if it doesn't exist
	scaleSet, err := service.scalesetClient.GetRunnerScaleSet(ctx, runnerGroupID, service.config.ScaleSetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get runner scale set: %w", err)
	}
	if scaleSet == nil {
		service.logger.Info("Runner scale set not found, creating a new one", slog.String("scaleSetName", service.config.ScaleSetName))
		scaleSet, err = service.scalesetClient.CreateRunnerScaleSet(ctx, &githubScaleset.RunnerScaleSet{
			Name:          service.config.ScaleSetName,
			RunnerGroupID: runnerGroupID,
			Labels:        service.config.BuildLabels(),
			RunnerSetting: githubScaleset.RunnerSetting{
				DisableUpdate: true,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create runner scale set: %w", err)
		}
		service.logger.Info("Created runner scale set", slog.String("scaleSetName", service.config.ScaleSetName), slog.Int("scaleSetID", scaleSet.ID))
	} else {
		service.logger.Info("Found existing runner scale set", slog.String("scaleSetName", service.config.ScaleSetName), slog.Int("scaleSetID", scaleSet.ID))
	}
	// Set the user agent for the scaleset client now that we have the scale set ID
	service.scalesetClient.SetSystemInfo(systemInfo(scaleSet.ID))

	return scaleSet, nil
}

func (service *Service) deleteRunnerScaleSet(ctx context.Context, scalesetID int) error {
	service.logger.Info(
		"Deleting runner scale set",
		slog.Int("scaleSetID", scalesetID),
	)
	if err := service.scalesetClient.DeleteRunnerScaleSet(context.WithoutCancel(ctx), scalesetID); err != nil {
		service.logger.Error(
			"Failed to delete runner scale set",
			slog.Int("scaleSetID", scalesetID),
			slog.String("error", err.Error()),
		)
	}
	return nil
}

func (service *Service) createMessageSessionClient(ctx context.Context) (*githubScaleset.MessageSessionClient, error) {
	// Get the name of the client which will be used as the owner
	hostname, err := os.Hostname()
	if err != nil {
		hostname = uuid.NewString()
		service.logger.Info("Failed to get hostname, fallback to uuid", "uuid", hostname, "error", err)
	}

	if service.runnerScaleset == nil {
		return nil, fmt.Errorf("runner scale set is not initialized")
	}

	// Create a message session client for the runner scale set
	sessionClient, err := service.scalesetClient.MessageSessionClient(ctx, service.runnerScaleset.ID, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to create message session client: %w", err)
	}
	return sessionClient, nil
}
