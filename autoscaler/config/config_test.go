package config

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestGetAutoscalerConfig_LoadsValues(t *testing.T) {
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("GITHUB_TOKEN", "token-value")

	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")

	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "runner-user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "runner-pass")

	t.Setenv("MAX_RUNNERS", "12")
	t.Setenv("MIN_RUNNERS", "2")

	t.Setenv("SCALE_SET_NAME", "my-scale-set")
	t.Setenv("LABELS", "self-hosted,linux,x64")

	t.Setenv("DOCKER_HOSTS", "tcp://1.1.1.1:2375,tcp://2.2.2.2:2375")
	t.Setenv("RUNNER_GROUP", "my-group")
	t.Setenv("RUNTIME", "sysbox-runc")

	cfg, errs := GetAutoscalerConfig()

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.RegistrationURL != "https://github.com/org/repo" {
		t.Fatalf("unexpected RegistrationURL: %q", cfg.RegistrationURL)
	}

	if cfg.Token != "token-value" {
		t.Fatalf("unexpected Token: %q", cfg.Token)
	}

	if cfg.RunnerImage != "ghcr.io/actions/actions-runner:latest" {
		t.Fatalf("unexpected RunnerImage: %q", cfg.RunnerImage)
	}

	if cfg.RegistryURL != "ghcr.io" {
		t.Fatalf("unexpected RegistryURL: %q", cfg.RegistryURL)
	}

	if cfg.RegistryUsername != "runner-user" {
		t.Fatalf("unexpected RegistryUsername: %q", cfg.RegistryUsername)
	}

	if cfg.RegistryPassword != "runner-pass" {
		t.Fatalf("unexpected RegistryPassword: %q", cfg.RegistryPassword)
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

	if len(cfg.Labels) != 3 {
		t.Fatalf("unexpected Labels: %#v", cfg.Labels)
	}

	expectedLabels := []string{"self-hosted", "linux", "x64"}
	for i, label := range expectedLabels {
		if cfg.Labels[i] != label {
			t.Fatalf("expected label %q at index %d, got %q", label, i, cfg.Labels[i])
		}
	}

	if len(cfg.DockerHosts) != 2 {
		t.Fatalf("unexpected DockerHosts: %#v", cfg.DockerHosts)
	}

	expectedHosts := []string{
		"tcp://1.1.1.1:2375",
		"tcp://2.2.2.2:2375",
	}

	for i, host := range expectedHosts {
		if cfg.DockerHosts[i] != host {
			t.Fatalf("expected host %q at index %d, got %q", host, i, cfg.DockerHosts[i])
		}
	}

	if cfg.RunnerGroup != "my-group" {
		t.Fatalf("unexpected RunnerGroup: %q", cfg.RunnerGroup)
	}

	if cfg.Runtime != "sysbox-runc" {
		t.Fatalf("unexpected Runtime: %q", cfg.Runtime)
	}
}

func TestGetAutoscalerConfig_DefaultValues(t *testing.T) {
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("GITHUB_TOKEN", "token-value")

	t.Setenv("RUNNER_IMAGE", "runner-image")

	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "pass")

	t.Setenv("SCALE_SET_NAME", "my-scale-set")
	t.Setenv("LABELS", "self-hosted")
	t.Setenv("DOCKER_HOSTS", "tcp://1.1.1.1:2375")

	cfg, errs := GetAutoscalerConfig()

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if cfg.LogLevel != "info" {
		t.Fatalf("expected default LogLevel=info, got %q", cfg.LogLevel)
	}

	if cfg.LogFormat != "text" {
		t.Fatalf("expected default LogFormat=text, got %q", cfg.LogFormat)
	}

	if cfg.MaxRunners != 10 {
		t.Fatalf("expected default MaxRunners=10, got %d", cfg.MaxRunners)
	}

	if cfg.MinRunners != 0 {
		t.Fatalf("expected default MinRunners=0, got %d", cfg.MinRunners)
	}

	if cfg.RunnerGroup != "default" {
		t.Fatalf("expected default RunnerGroup=default, got %q", cfg.RunnerGroup)
	}

	if cfg.Runtime != "runc" {
		t.Fatalf("expected default Runtime=runc, got %q", cfg.Runtime)
	}

	if cfg.BuildkitHostURL != "" {
		t.Fatalf("expected empty BuildkitHostURL, got %q", cfg.BuildkitHostURL)
	}

	if cfg.DeleteScaleSetOnShutdown {
		t.Fatal("expected default DeleteScaleSetOnShutdown=false, got true")
	}
}

func TestGetAutoscalerConfig_MissingRequiredFields(t *testing.T) {
	t.Setenv("REGISTRATION_URL", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("RUNNER_IMAGE", "")
	t.Setenv("DOCKER_REGISTRY_URL", "")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "")
	t.Setenv("SCALE_SET_NAME", "")
	t.Setenv("LABELS", "")
	t.Setenv("DOCKER_HOSTS", "")

	cfg, errs := GetAutoscalerConfig()

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}

	missingKeys := map[string]bool{
		"REGISTRATION_URL":         false,
		"GITHUB_TOKEN":             false,
		"RUNNER_IMAGE":             false,
		"DOCKER_REGISTRY_URL":      false,
		"DOCKER_REGISTRY_USERNAME": false,
		"DOCKER_REGISTRY_PASSWORD": false,
		"SCALE_SET_NAME":           false,
		"LABELS":                   false,
		"DOCKER_HOSTS":             false,
	}

	for _, err := range errs {
		for key := range missingKeys {
			if strings.Contains(err.Error(), key) {
				missingKeys[key] = true
			}
		}
	}

	for key, found := range missingKeys {
		if !found {
			t.Fatalf("expected error mentioning %s, got %v", key, errs)
		}
	}
}

func TestGetAutoscalerConfig_InvalidInteger(t *testing.T) {
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("GITHUB_TOKEN", "token")

	t.Setenv("RUNNER_IMAGE", "runner-image")

	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "pass")

	t.Setenv("SCALE_SET_NAME", "my-scale-set")
	t.Setenv("LABELS", "self-hosted")
	t.Setenv("DOCKER_HOSTS", "tcp://1.1.1.1:2375")

	t.Setenv("MAX_RUNNERS", "not-an-int")

	_, errs := GetAutoscalerConfig()

	if len(errs) == 0 {
		t.Fatal("expected validation error")
	}
}

func TestLoggerCreation(t *testing.T) {
	cfg := &AutoscalerConfig{
		LogLevel:  "debug",
		LogFormat: "json",
	}

	if cfg.Logger() == nil {
		t.Fatal("expected logger")
	}

	cfg = &AutoscalerConfig{
		LogLevel:  "info",
		LogFormat: "text",
	}

	if cfg.Logger() == nil {
		t.Fatal("expected logger")
	}
}

// Premier champ booléen de la configuration : on vérifie que la branche
// reflect.Bool du décodage lit bien la variable d'environnement.
func TestGetAutoscalerConfig_DeleteScaleSetOnShutdown(t *testing.T) {
	t.Setenv("REGISTRATION_URL", "https://github.com/org/repo")
	t.Setenv("GITHUB_TOKEN", "token-value")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("DOCKER_REGISTRY_URL", "ghcr.io")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "runner-user")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "runner-pass")
	t.Setenv("ARTIFACTORY_TOKEN", "artifact-token")
	t.Setenv("SCALE_SET_NAME", "my-scale-set")
	t.Setenv("LABELS", "self-hosted")
	t.Setenv("DOCKER_HOSTS", "tcp://1.1.1.1:2375")

	t.Setenv("DELETE_SCALE_SET_ON_SHUTDOWN", "true")

	cfg, errs := GetAutoscalerConfig()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	if !cfg.DeleteScaleSetOnShutdown {
		t.Fatal("expected DeleteScaleSetOnShutdown=true")
	}
}

// Un LOG_FORMAT inconnu (faute de frappe, valeur vide) doit retomber sur le
// format texte, et surtout pas rendre l'autoscaler muet.
func TestLoggerFallsBackToTextOnUnknownFormat(t *testing.T) {
	for _, format := range []string{"", "jsonn", "TEXT", "console"} {
		cfg := &AutoscalerConfig{
			LogLevel:  "info",
			LogFormat: format,
		}

		handler := cfg.Logger().Handler()
		if !handler.Enabled(context.Background(), slog.LevelInfo) {
			t.Fatalf("LOG_FORMAT=%q: le logger est muet, il devrait écrire en texte", format)
		}
	}

	// Le format json reste évidemment pris en compte, quelle que soit la casse.
	cfg := &AutoscalerConfig{LogLevel: "info", LogFormat: "JSON"}
	if _, ok := cfg.Logger().Handler().(*slog.JSONHandler); !ok {
		t.Fatal("LOG_FORMAT=JSON devrait produire un JSONHandler")
	}
}
