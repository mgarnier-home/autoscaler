package config

import (
	"testing"
)

func TestGetAutoscalerConfig_LoadsValues(t *testing.T) {
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("MAX_RUNNERS", "12")
	t.Setenv("MIN_RUNNERS", "2")
	t.Setenv("SCALE_SET_NAME", "my-scale-set")
	t.Setenv("LABELS", "self-hosted,linux,x64")
	t.Setenv("GITHUB_TOKEN", "token-value")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "runner-user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "runner-pass")
	t.Setenv("ARTIFACTORY_TOKEN", "artifact-token")
	t.Setenv("DOCKER_HOSTS", "tcp://1.1.1.1:2375,tcp://2.2.2.2:2375")
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
	if cfg.MaxRunners != 12 {
		t.Fatalf("unexpected MaxRunners: %d", cfg.MaxRunners)
	}
	if cfg.MinRunners != 2 {
		t.Fatalf("unexpected MinRunners: %d", cfg.MinRunners)
	}
	if cfg.ScaleSetName != "my-scale-set" {
		t.Fatalf("unexpected ScaleSetName: %q", cfg.ScaleSetName)
	}
	if len(cfg.Labels) != 3 || cfg.Labels[0] != "self-hosted" || cfg.Labels[1] != "linux" || cfg.Labels[2] != "x64" {
		t.Fatalf("unexpected Labels: %#v", cfg.Labels)
	}
	if cfg.RunnerGroup != "default" {
		t.Fatalf("expected default RunnerGroup, got %q", cfg.RunnerGroup)
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
	if len(cfg.DockerHosts) != 2 || cfg.DockerHosts[0] != "tcp://1.1.1.1:2375" || cfg.DockerHosts[1] != "tcp://2.2.2.2:2375" {
		t.Fatalf("unexpected DockerHosts: %#v", cfg.DockerHosts)
	}
}

func TestGetAutoscalerConfig_MissingRequiredFields(t *testing.T) {
	t.Setenv("MAX_RUNNERS", "10")
	t.Setenv("MIN_RUNNERS", "0")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")

	cfg, errs := GetAutoscalerConfig()
	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	if len(errs) == 0 {
		t.Fatalf("expected validation errors for missing required env vars")
	}

	missingKeys := map[string]bool{
		"REGISTRATION_URL":         false,
		"SCALE_SET_NAME":           false,
		"LABELS":                   false,
		"GITHUB_TOKEN":             false,
		"DOCKER_REGISTRY_URL":      false,
		"DOCKER_REGISTRY_USERNAME": false,
		"DOCKER_REGISTRY_PASSWORD": false,
		"ARTIFACTORY_TOKEN":        false,
	}

	for _, err := range errs {
		for key := range missingKeys {
			if err != nil && contains(err.Error(), key) {
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
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("MAX_RUNNERS", "not-an-int")
	t.Setenv("MIN_RUNNERS", "0")
	t.Setenv("SCALE_SET_NAME", "my-scale-set")
	t.Setenv("LABELS", "self-hosted")
	t.Setenv("GITHUB_TOKEN", "token-value")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "runner-user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "runner-pass")
	t.Setenv("ARTIFACTORY_TOKEN", "artifact-token")

	_, errs := GetAutoscalerConfig()
	if len(errs) == 0 {
		t.Fatalf("expected error for invalid MAX_RUNNERS")
	}
}

func contains(value, substr string) bool {
	return len(value) >= len(substr) && (value == substr || len(substr) == 0 || indexOf(value, substr) >= 0)
}

func indexOf(value, substr string) int {
	for i := 0; i+len(substr) <= len(value); i++ {
		if value[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
