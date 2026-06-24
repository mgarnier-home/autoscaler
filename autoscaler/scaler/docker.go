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
)

const (
	runnerNpmCacheVolumeName    = "runner-npm-cache"
	runnerMavenCacheVolumeName  = "runner-maven-cache"
	runnerBuildxCacheVolumeName = "runner-buildx-cache"
)

type DockerClientWithMetadata struct {
	*dockerclient.Client
	Runtime string
}

type pullImageParams struct {
	RegistryURL      string
	RegistryUsername string
	RegistryPassword string
	RunnerImage      string
}

func pullRunnerImage(ctx context.Context, client *DockerClientWithMetadata, params *pullImageParams) error {
	// Check if image already exists locally
	_, localErr := client.ImageInspect(ctx, params.RunnerImage)
	imageExistsLocally := localErr == nil

	authConfig := registry.AuthConfig{
		Username:      params.RegistryUsername,
		Password:      params.RegistryPassword,
		ServerAddress: params.RegistryURL, // e.g., "ghcr.io" or your custom registry URL
	}
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal auth config: %w", err)
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)

	// Pull the runner image
	pull, err := client.ImagePull(ctx, params.RunnerImage, image.PullOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		// Pull failed.
		// If we already have the image locally, continue.
		if imageExistsLocally {
			slog.Warn(
				"failed to pull image, using local copy",
				"dockerHost", client.DaemonHost(),
				"image", params.RunnerImage,
				"error", err,
			)
			return nil
		}

		// No local image either -> hard failure
		return fmt.Errorf(
			"image %q not available locally and pull failed: %w",
			params.RunnerImage,
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

func createDockerClients(dockerHosts []string, runtime string) ([]*DockerClientWithMetadata, error) {
	var clients []*DockerClientWithMetadata

	for _, dockerHost := range dockerHosts {
		host := strings.TrimSpace(dockerHost)
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
		clients = append(clients, &DockerClientWithMetadata{
			Client:  client,
			Runtime: runtime,
		})
	}

	return clients, nil
}

func createCacheVolumes(ctx context.Context, client *DockerClientWithMetadata) error {
	for _, volName := range []string{runnerNpmCacheVolumeName, runnerMavenCacheVolumeName, runnerBuildxCacheVolumeName} {
		_, err := client.VolumeCreate(ctx, volume.CreateOptions{
			Name: volName,
		})
		if err != nil {
			return fmt.Errorf("failed to create volume %s: %w", volName, err)
		}
	}
	return nil
}

type startContainerParams struct {
	containerName     string
	jitConfig         *scaleset.RunnerScaleSetJitRunnerConfig
	registryURL       string
	registryUsername  string
	registryPassword  string
	runnerImage       string
	artifactoryToken  string
	registryMirrorURL string
}

func startRunnerContainer(
	ctx context.Context,
	dockerClient *DockerClientWithMetadata,
	startContainerParams *startContainerParams,
) (containerId string, err error) {
	runnerContainer, err := dockerClient.ContainerCreate(
		ctx,
		&container.Config{
			Image: startContainerParams.runnerImage,
			User:  "runner",
			Cmd:   []string{"/home/runner/run.sh"},
			Env: []string{
				fmt.Sprintf("ACTIONS_RUNNER_INPUT_JITCONFIG=%s", startContainerParams.jitConfig.EncodedJITConfig),
				fmt.Sprintf("DOCKER_REGISTRY_URL=%s", startContainerParams.registryURL),
				fmt.Sprintf("DOCKER_REGISTRY_USERNAME=%s", startContainerParams.registryUsername),
				fmt.Sprintf("DOCKER_REGISTRY_PASSWORD=%s", startContainerParams.registryPassword),
				fmt.Sprintf("ARTIFACTORY_TOKEN=%s", startContainerParams.artifactoryToken),
				fmt.Sprintf("DOCKER_MIRROR_URL=%s", startContainerParams.registryMirrorURL),
				"START_DOCKER_SERVICE=true",
			},
		},
		&container.HostConfig{
			Runtime: dockerClient.Runtime,
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
					Source: runnerBuildxCacheVolumeName,
					Target: "/buildx-cache",
				},
			},
		},
		nil, nil,
		startContainerParams.containerName,
	)

	if err != nil {
		return "", fmt.Errorf("failed to create runner container: %w", err)
	}

	if err := dockerClient.ContainerStart(ctx, runnerContainer.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	return runnerContainer.ID, nil
}
