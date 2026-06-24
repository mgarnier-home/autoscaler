package config

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strconv"
	"strings"

	"mgarnier11.fr/docker-autoscaler/utils"
)

type AutoscalerConfig struct {
	RegistrationURL string `key:"REGISTRATION_URL" required:"true"`
	Token           string `key:"GITHUB_TOKEN" required:"true"`

	RunnerImage       string `key:"RUNNER_IMAGE" required:"true"`
	RegistryURL       string `key:"DOCKER_REGISTRY_URL" required:"true"`
	RegistryUsername  string `key:"DOCKER_REGISTRY_USERNAME" required:"true"`
	RegistryPassword  string `key:"DOCKER_REGISTRY_PASSWORD" required:"true"`
	RegistryMirrorURL string `key:"DOCKER_REGISTRY_MIRROR_URL" default-value:""`

	LogLevel  string `key:"LOG_LEVEL" default-value:"info"`
	LogFormat string `key:"LOG_FORMAT" default-value:"text"`

	MaxRunners   int      `key:"MAX_RUNNERS" default-value:"10"`
	MinRunners   int      `key:"MIN_RUNNERS" default-value:"0"`
	ScaleSetName string   `key:"SCALE_SET_NAME" required:"true"`
	Labels       []string `key:"LABELS" required:"true"`
	RunnerGroup  string   `key:"RUNNER_GROUP" default-value:"default"`
	DockerHosts  []string `key:"DOCKER_HOSTS" required:"true"`
	Runtime      string   `key:"RUNTIME" default-value:"runc"`
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

func GetAutoscalerConfig() (autoscalerCfg *AutoscalerConfig, configErrors []error) {
	utils.InitEnvFromFile()

	configErrors = []error{}

	autoscalerCfg = &AutoscalerConfig{}

	t := reflect.TypeOf(AutoscalerConfig{})

	for i := 0; i < t.NumField(); i++ {
		// On parse les fields de la struct autoscalerConfig pour récupérer les valeurs des variables d'environnement correspondantes
		field := t.Field(i)
		key := field.Tag.Get("key")
		// Si la clé est vide, on ignore ce champ
		if key == "" {
			continue
		}
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

	return autoscalerCfg, configErrors
}
