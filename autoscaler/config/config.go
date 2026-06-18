package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/actions/scaleset"
	"operis.fr/docker-autoscaler/utils"
)

type AutoscalerConfig struct {
	RegistrationURL  string   `key:"REGISTRATION_URL" required:"true"`
	MaxRunners       int      `key:"MAX_RUNNERS" default-value:"10"`
	MinRunners       int      `key:"MIN_RUNNERS" default-value:"0"`
	ScaleSetName     string   `key:"SCALE_SET_NAME" required:"true"`
	Labels           []string `key:"LABELS" required:"true"`
	RunnerGroup      string   `key:"RUNNER_GROUP" default-value:"default"`
	Token            string   `key:"GITHUB_TOKEN" required:"true"`
	RunnerImage      string   `key:"RUNNER_IMAGE" required:"true"`
	LogLevel         string   `key:"LOG_LEVEL" default-value:"info"`
	LogFormat        string   `key:"LOG_FORMAT" default-value:"text"`
	RegistryURL      string   `key:"DOCKER_REGISTRY_URL" required:"true"`
	RegistryUsername string   `key:"DOCKER_REGISTRY_USERNAME" required:"true"`
	RegistryPassword string   `key:"DOCKER_REGISTRY_PASSWORD" required:"true"`
	ArtifactoryToken string   `key:"ARTIFACTORY_TOKEN" required:"true"`
	DockerHosts      []string `key:"DOCKER_HOSTS" required:"true"`
	DockerRuntime    string   `key:"DOCKER_RUNTIME" default-value:"runc"`
}

func (c *AutoscalerConfig) Logger() *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	switch c.LogFormat {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     lvl,
		}))
	case "text":
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     lvl,
		}))
	default:
		return slog.New(slog.DiscardHandler)
	}
}

// BuildLabels returns the labels to use for the runner scale set.
// If custom labels are provided, those are used; otherwise, the scale set name is used as the label.
func (c *AutoscalerConfig) BuildLabels() []scaleset.Label {
	if len(c.Labels) > 0 {
		labels := make([]scaleset.Label, len(c.Labels))
		for i, name := range c.Labels {
			labels[i] = scaleset.Label{Name: strings.TrimSpace(name)}
		}
		return labels
	}
	return []scaleset.Label{{Name: c.ScaleSetName}}
}

func (c *AutoscalerConfig) validate() []error {
	var errs []error
	if _, err := url.ParseRequestURI(c.RegistrationURL); err != nil {
		errs = append(errs, fmt.Errorf("Invalid REGISTRATION_URL: %v", err))
	}

	if c.MaxRunners < c.MinRunners {
		errs = append(errs, fmt.Errorf("MAX_RUNNERS (%d) cannot be less than MIN_RUNNERS (%d)", c.MaxRunners, c.MinRunners))
	}

	return errs
}

func GetAutoscalerConfig() (autoscalerCfg *AutoscalerConfig, configErrors []error) {
	utils.InitEnvFromFile()

	configErrors = []error{}

	autoscalerCfg = &AutoscalerConfig{}

	t := reflect.TypeOf(AutoscalerConfig{})

	for i := 0; i < t.NumField(); i++ {
		// On parse les fields de la struct autoscalerConfig pour récupérer les valeurs des variables d'environnement correspondantes
		field := t.Field(i)
		key := field.Tag.Get("key")
		defaultValue := field.Tag.Get("default-value")
		required := field.Tag.Get("required") == "true"

		// On déclare les variables value et err en dehors du switch pour pouvoir les utiliser après le switch
		var value any
		var err error

		switch field.Type.Kind() {
		case reflect.Int:
			// Quand le type est int, on convertit la valeur par défaut en int avant de l'utiliser
			defaultInt, err := strconv.Atoi(defaultValue)
			if err != nil {
				configErrors = append(configErrors, fmt.Errorf("Invalid default value for field %s: %v", field.Name, err))
				continue
			}

			err, value = utils.GetEnvValue(key, defaultInt, required)
			if err != nil {
				configErrors = append(configErrors, err)
				continue
			}

		case reflect.String:
			err, value = utils.GetEnvValue(key, defaultValue, required)
			if err != nil {
				configErrors = append(configErrors, err)
				continue
			}
		case reflect.Bool:
			err, value = utils.GetEnvValue(key, defaultValue == "true", required)
			if err != nil {
				configErrors = append(configErrors, err)
				continue
			}
		case reflect.Slice:
			err, value = utils.GetEnvValue(key, strings.Split(defaultValue, ","), required)
			if err != nil {
				configErrors = append(configErrors, err)
				continue
			}
		default:
			configErrors = append(configErrors, fmt.Errorf("Unsupported field type for field %s", field.Name))
			continue
		}

		fieldValue := reflect.ValueOf(autoscalerCfg).Elem().FieldByName(field.Name)

		if fieldValue.CanSet() {
			fieldValue.Set(reflect.ValueOf(value))
		} else {
			configErrors = append(configErrors, fmt.Errorf("Cannot set field %s", field.Name))
		}
	}

	configErrors = append(configErrors, autoscalerCfg.validate()...)

	return autoscalerCfg, configErrors
}
