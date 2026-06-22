package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetAutoscalerConfig_LoadsValues(t *testing.T) {
	configFilePath := writeTempScaleSetsConfig(t, `
- scaleSetName: my-scale-set
  maxRunners: 12
  minRunners: 2
  labels:
    - self-hosted
    - linux
    - x64
  dockerHosts:
    - name: athena
      runtime: runc
      url: tcp://1.1.1.1:2375
    - name: atlas
      runtime: sysbox-runc
      url: tcp://2.2.2.2:2375
`)

	t.Setenv("CONFIG_FILE_PATH", configFilePath)
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("GITHUB_TOKEN", "token-value")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "runner-user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "runner-pass")
	t.Setenv("ARTIFACTORY_TOKEN", "artifact-token")

	cfg, errs := GetAutoscalerConfig()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	if cfg.RegistrationURL != "https://github.com/org/repo" {
		t.Fatalf("unexpected RegistrationURL: %q", cfg.RegistrationURL)
	}
	if cfg.RunnerImage != "ghcr.io/actions/actions-runner:latest" {
		t.Fatalf("unexpected RunnerImage: %q", cfg.RunnerImage)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected default LogLevel info, got %q", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("expected default LogFormat text, got %q", cfg.LogFormat)
	}
	if cfg.RegistryURL != "ghcr.io" || cfg.RegistryUsername != "runner-user" || cfg.RegistryPassword != "runner-pass" {
		t.Fatalf("unexpected registry values: %#v", cfg)
	}
	if cfg.ArtifactoryToken != "artifact-token" {
		t.Fatalf("unexpected ArtifactoryToken: %q", cfg.ArtifactoryToken)
	}

	if len(cfg.ScaleSetsConfigs) != 1 {
		t.Fatalf("expected 1 scale set config, got %d", len(cfg.ScaleSetsConfigs))
	}

	scaleSet := cfg.ScaleSetsConfigs[0]
	if scaleSet.ScaleSetName != "my-scale-set" {
		t.Fatalf("unexpected ScaleSetName: %q", scaleSet.ScaleSetName)
	}
	if scaleSet.MaxRunners != 12 {
		t.Fatalf("unexpected MaxRunners: %d", scaleSet.MaxRunners)
	}
	if scaleSet.MinRunners != 2 {
		t.Fatalf("unexpected MinRunners: %d", scaleSet.MinRunners)
	}
	if len(scaleSet.Labels) != 3 || scaleSet.Labels[0] != "self-hosted" || scaleSet.Labels[1] != "linux" || scaleSet.Labels[2] != "x64" {
		t.Fatalf("unexpected Labels: %#v", scaleSet.Labels)
	}
	if len(scaleSet.DockerHosts) != 2 || scaleSet.DockerHosts[0].Url != "tcp://1.1.1.1:2375" || scaleSet.DockerHosts[1].Url != "tcp://2.2.2.2:2375" {
		t.Fatalf("unexpected DockerHosts: %#v", scaleSet.DockerHosts)
	}
	if scaleSet.DockerHosts[0].Name != "athena" || scaleSet.DockerHosts[0].Runtime != "runc" {
		t.Fatalf("unexpected first DockerHost metadata: %#v", scaleSet.DockerHosts[0])
	}
	if scaleSet.DockerHosts[1].Name != "atlas" || scaleSet.DockerHosts[1].Runtime != "sysbox-runc" {
		t.Fatalf("unexpected second DockerHost metadata: %#v", scaleSet.DockerHosts[1])
	}
}

func TestGetAutoscalerConfig_MissingRequiredFields(t *testing.T) {
	configFilePath := writeTempScaleSetsConfig(t, `
- scaleSetName: my-scale-set
  maxRunners: 2
  minRunners: 0
  labels: [self-hosted]
  dockerHosts:
    - name: athena
      runtime: runc
      url: tcp://1.1.1.1:2375
`)

	t.Setenv("CONFIG_FILE_PATH", configFilePath)
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")

	t.Setenv("REGISTRATION_URL", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("DOCKER_REGISTRY_URL", "")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "")
	t.Setenv("ARTIFACTORY_TOKEN", "")

	cfg, errs := GetAutoscalerConfig()
	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	if len(errs) == 0 {
		t.Fatalf("expected validation errors for missing required env vars")
	}

	missingKeys := map[string]bool{
		"REGISTRATION_URL":         false,
		"GITHUB_TOKEN":             false,
		"DOCKER_REGISTRY_URL":      false,
		"DOCKER_REGISTRY_USERNAME": false,
		"DOCKER_REGISTRY_PASSWORD": false,
		"ARTIFACTORY_TOKEN":        false,
	}

	for _, err := range errs {
		for key := range missingKeys {
			if err != nil && strings.Contains(err.Error(), key) {
				missingKeys[key] = true
			}
		}
	}

	for key, seen := range missingKeys {
		if !seen {
			t.Fatalf("expected error mentioning missing key %s, got errors %v", key, errs)
		}
	}
}

func TestGetAutoscalerConfig_InvalidIntAddsError(t *testing.T) {
	configFilePath := writeTempScaleSetsConfig(t, `
- scaleSetName: my-scale-set
  maxRunners: 2
  minRunners: 0
  labels: [self-hosted]
  dockerHosts:
    - name: athena
      runtime: runc
      url: tcp://1.1.1.1:2375
`)

	t.Setenv("CONFIG_FILE_PATH", configFilePath)
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("GITHUB_TOKEN", "token-value")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "runner-pass")
	t.Setenv("ARTIFACTORY_TOKEN", "artifact-token")

	_, errs := GetAutoscalerConfig()
	if len(errs) == 0 {
		t.Fatalf("expected validation error for missing DOCKER_REGISTRY_USERNAME")
	}
}

func TestDecodeScaleSetsConfigs(t *testing.T) {
	t.Run("valid YAML decodes scale sets", func(t *testing.T) {
		configFilePath := writeTempScaleSetsConfig(t, `
- scaleSetName: my-scale-set
  maxRunners: 10
  minRunners: 2
  labels:
    - self-hosted
    - linux
  dockerHosts:
    - name: athena
      runtime: runc
      url: tcp://1.1.1.1:2375
`)

		scaleSets, errs := DecodeScaleSetsConfigs(configFilePath)
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}

		if len(scaleSets) != 1 {
			t.Fatalf("expected 1 scale set, got %d", len(scaleSets))
		}

		if scaleSets[0].ScaleSetName != "my-scale-set" {
			t.Fatalf("unexpected scaleSetName: %q", scaleSets[0].ScaleSetName)
		}
		if len(scaleSets[0].DockerHosts) != 1 || scaleSets[0].DockerHosts[0].Name != "athena" {
			t.Fatalf("unexpected dockerHosts: %#v", scaleSets[0].DockerHosts)
		}
	})

	testCases := []struct {
		name      string
		yaml      string
		errSubstr string
	}{
		{
			name: "empty host name is rejected",
			yaml: `
- scaleSetName: my-scale-set
  maxRunners: 1
  minRunners: 0
  labels: [self-hosted]
  dockerHosts:
    - name: ""
      runtime: runc
      url: tcp://1.1.1.1:2375
`,
			errSubstr: "'name' cannot be empty",
		},
		{
			name: "invalid url is rejected",
			yaml: `
- scaleSetName: my-scale-set
  maxRunners: 1
  minRunners: 0
  labels: [self-hosted]
  dockerHosts:
    - name: athena
      runtime: runc
      url: not-a-url
`,
			errSubstr: "invalid 'url'",
		},
		{
			name: "unsupported runtime is rejected",
			yaml: `
- scaleSetName: my-scale-set
  maxRunners: 1
  minRunners: 0
  labels: [self-hosted]
  dockerHosts:
    - name: athena
      runtime: kata
      url: tcp://1.1.1.1:2375
`,
			errSubstr: "unsupported 'runtime'",
		},
		{
			name: "max lower than min is rejected",
			yaml: `
- scaleSetName: my-scale-set
  maxRunners: 0
  minRunners: 1
  labels: [self-hosted]
  dockerHosts:
    - name: athena
      runtime: runc
      url: tcp://1.1.1.1:2375
`,
			errSubstr: "cannot be less than 'minRunners'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configFilePath := writeTempScaleSetsConfig(t, tc.yaml)
			_, errs := DecodeScaleSetsConfigs(configFilePath)
			if len(errs) == 0 {
				t.Fatalf("expected errors, got none")
			}

			found := false
			for _, err := range errs {
				if strings.Contains(err.Error(), tc.errSubstr) {
					found = true
					break
				}
			}

			if !found {
				t.Fatalf("expected one error containing %q, got %v", tc.errSubstr, errs)
			}
		})
	}
}

func writeTempScaleSetsConfig(t *testing.T, content string) string {
	t.Helper()

	filePath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(filePath, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	return filePath
}
