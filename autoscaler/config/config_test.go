package config

import (
	"strings"
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
	t.Setenv("DOCKER_HOSTS_JSON", `[{"name":"athena","labels":["self-hosted","linux"],"runtime":"runc","url":"tcp://1.1.1.1:2375"},{"name":"atlas","labels":["self-hosted","docker"],"runtime":"sysbox-runc","url":"tcp://2.2.2.2:2375"}]`)
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
	if len(cfg.DockerHosts) != 2 || cfg.DockerHosts[0].Url != "tcp://1.1.1.1:2375" || cfg.DockerHosts[1].Url != "tcp://2.2.2.2:2375" {
		t.Fatalf("unexpected DockerHosts: %#v", cfg.DockerHosts)
	}
	if cfg.DockerHosts[0].Name != "athena" || cfg.DockerHosts[0].Runtime != "runc" {
		t.Fatalf("unexpected first DockerHost metadata: %#v", cfg.DockerHosts[0])
	}
	if cfg.DockerHosts[1].Name != "atlas" || cfg.DockerHosts[1].Runtime != "sysbox-runc" {
		t.Fatalf("unexpected DockerHosts: %#v", cfg.DockerHosts)
	}
}

func TestGetAutoscalerConfig_MissingRequiredFields(t *testing.T) {
	t.Setenv("MAX_RUNNERS", "10")
	t.Setenv("MIN_RUNNERS", "0")
	t.Setenv("RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")

	t.Setenv("REGISTRATION_URL", "")
	t.Setenv("SCALE_SET_NAME", "")
	t.Setenv("LABELS", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("DOCKER_REGISTRY_URL", "")
	t.Setenv("DOCKER_REGISTRY_USERNAME", "")
	t.Setenv("DOCKER_REGISTRY_PASSWORD", "")
	t.Setenv("ARTIFACTORY_TOKEN", "")
	t.Setenv("DOCKER_HOSTS_JSON", "")

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
		"DOCKER_HOSTS_JSON":        false,
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
	t.Setenv("DOCKER_HOSTS_JSON", `[{"name":"athena","labels":["self-hosted"],"runtime":"runc","url":"tcp://1.1.1.1:2375"}]`)

	_, errs := GetAutoscalerConfig()
	if len(errs) == 0 {
		t.Fatalf("expected error for invalid MAX_RUNNERS")
	}
}

func TestDecodeDockerHostsJSON(t *testing.T) {
	t.Run("valid JSON decodes hosts", func(t *testing.T) {
		raw := `[{"name":"athena","labels":["self-hosted","linux"],"runtime":"runc","url":"tcp://1.1.1.1:2375"}]`

		hosts, err := DecodeDockerHostsJSON(raw)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(hosts) != 1 {
			t.Fatalf("expected 1 host, got %d", len(hosts))
		}

		host := hosts[0]
		if host.Name != "athena" {
			t.Fatalf("unexpected host name: %q", host.Name)
		}
		if host.Runtime != "runc" {
			t.Fatalf("unexpected runtime: %q", host.Runtime)
		}
		if host.Url != "tcp://1.1.1.1:2375" {
			t.Fatalf("unexpected url: %q", host.Url)
		}
		if len(host.Labels) != 2 || host.Labels[0] != "self-hosted" || host.Labels[1] != "linux" {
			t.Fatalf("unexpected labels: %#v", host.Labels)
		}
	})

	testCases := []struct {
		name      string
		raw       string
		errSubstr string
	}{
		{
			name:      "empty host name is rejected",
			raw:       `[{"name":"","labels":["self-hosted"],"runtime":"runc","url":"tcp://1.1.1.1:2375"}]`,
			errSubstr: "name cannot be empty",
		},
		{
			name:      "invalid url is rejected",
			raw:       `[{"name":"athena","labels":["self-hosted"],"runtime":"runc","url":"not-a-url"}]`,
			errSubstr: "Invalid Docker host URL",
		},
		{
			name:      "unsupported runtime is rejected",
			raw:       `[{"name":"athena","labels":["self-hosted"],"runtime":"kata","url":"tcp://1.1.1.1:2375"}]`,
			errSubstr: "Unsupported Docker host runtime",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeDockerHostsJSON(tc.raw)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("expected error containing %q, got %q", tc.errSubstr, err.Error())
			}
		})
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
