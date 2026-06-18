package scaler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/actions/scaleset"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"operis.fr/docker-autoscaler/config"
)

const (
	runnerNpmCacheVolumeName      = "runner-npm-cache"
	runnerMavenCacheVolumeName    = "runner-maven-cache"
	runnerAsdfDownloadsVolumeName = "runner-asdf-downloads"
	runnerBuildxCacheVolumeName   = "runner-buildx-cache"
)

func pullRunnerImage(ctx context.Context, client *dockerclient.Client, config *config.AutoscalerConfig) error {
	// Check if image already exists locally
	_, localErr := client.ImageInspect(ctx, config.RunnerImage)
	imageExistsLocally := localErr == nil

	authConfig := registry.AuthConfig{
		Username:      config.RegistryUsername,
		Password:      config.RegistryPassword,
		ServerAddress: config.RegistryURL, // e.g., "ghcr.io" or your custom registry URL
	}
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal auth config: %w", err)
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)

	// Pull the runner image
	pull, err := client.ImagePull(ctx, config.RunnerImage, image.PullOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		// Pull failed.
		// If we already have the image locally, continue.
		if imageExistsLocally {
			slog.Warn(
				"failed to pull image, using local copy",
				"dockerHost", client.DaemonHost(),
				"image", config.RunnerImage,
				"error", err,
			)
			return nil
		}

		// No local image either -> hard failure
		return fmt.Errorf(
			"image %q not available locally and pull failed: %w",
			config.RunnerImage,
			err,
		)
	}

	if _, err := io.ReadAll(pull); err != nil {
		return fmt.Errorf("failed to read image pull response: %w", err)
	}

	if err := pull.Close(); err != nil {
		return fmt.Errorf("failed to close image pull: %w", err)
	}

	return nil
}

func createDockerClients(dockerHosts []string) ([]*dockerclient.Client, error) {
	var clients []*dockerclient.Client

	for _, host := range dockerHosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		client, err := dockerclient.NewClientWithOpts(
			dockerclient.WithHost(host),
			dockerclient.WithAPIVersionNegotiation(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker client for host %s: %w", host, err)
		}
		clients = append(clients, client)
	}

	return clients, nil
}

func createCacheVolumes(ctx context.Context, client *dockerclient.Client) error {
	for _, volName := range []string{runnerNpmCacheVolumeName, runnerMavenCacheVolumeName, runnerAsdfDownloadsVolumeName, runnerBuildxCacheVolumeName} {
		_, err := client.VolumeCreate(ctx, volume.CreateOptions{
			Name: volName,
		})
		if err != nil {
			return fmt.Errorf("failed to create volume %s: %w", volName, err)
		}
	}
	return nil
}

func (service *Service) startRunnerContainer(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	containerName string,
	jitConfig *scaleset.RunnerScaleSetJitRunnerConfig,
) (containerId string, err error) {

	service.logger.Info("Starting runner container", slog.String("name", containerName), slog.String("dockerHost", dockerClient.DaemonHost()))

	runnerContainer, err := dockerClient.ContainerCreate(
		ctx,
		&container.Config{
			Image: service.config.RunnerImage,
			User:  "runner",
			Cmd:   []string{"/home/runner/run.sh"},
			Env: []string{
				fmt.Sprintf("ACTIONS_RUNNER_INPUT_JITCONFIG=%s", jitConfig.EncodedJITConfig),
				fmt.Sprintf("DOCKER_REGISTRY_URL=%s", service.config.RegistryURL),
				fmt.Sprintf("DOCKER_REGISTRY_USERNAME=%s", service.config.RegistryUsername),
				fmt.Sprintf("DOCKER_REGISTRY_PASSWORD=%s", service.config.RegistryPassword),
				fmt.Sprintf("ARTIFACTORY_TOKEN=%s", service.config.ArtifactoryToken),
				"DOCKER_MIRROR_URL=http://registry-mirror:5000",
				"START_DOCKER_SERVICE=true",
			},
		},
		&container.HostConfig{
			Runtime: service.config.DockerRuntime,
			ExtraHosts: []string{
				"registry-mirror:host-gateway",
			},
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: runnerNpmCacheVolumeName,
					Target: "/home/runner/.npm",
				},
				{
					Type:   mount.TypeVolume,
					Source: runnerMavenCacheVolumeName,
					Target: "/home/runner/.m2/repository",
				},
				{
					Type:   mount.TypeVolume,
					Source: runnerAsdfDownloadsVolumeName,
					Target: "/asdf/downloads",
				},
				{
					Type:   mount.TypeVolume,
					Source: runnerBuildxCacheVolumeName,
					Target: "/buildx-cache",
				},
			},
		},
		nil, nil,
		containerName,
	)

	if err != nil {
		return "", fmt.Errorf("failed to create runner container: %w", err)
	}

	if err := dockerClient.ContainerStart(ctx, runnerContainer.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	return runnerContainer.ID, nil
}
