package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"mgarnier11.fr/docker-autoscaler/utils"
)

type DockerHost struct {
	Name    string `yaml:"name"`
	Runtime string `yaml:"runtime"`
	Url     string `yaml:"url"`
}

type ScaleSetConfig struct {
	MaxRunners   int      `yaml:"maxRunners"`
	MinRunners   int      `yaml:"minRunners"`
	ScaleSetName string   `yaml:"scaleSetName"`
	Labels       []string `yaml:"labels"`
	RunnerGroup  string   `yaml:"runnerGroup"`

	DockerHosts []DockerHost `yaml:"dockerHosts"`
}

type AutoscalerConfig struct {
	ConfigurationFilePath string `key:"CONFIG_FILE_PATH" default-value:"./config.yaml"`
	ScaleSetsConfigs      []ScaleSetConfig

	RegistrationURL string `key:"REGISTRATION_URL" required:"true"`
	Token           string `key:"GITHUB_TOKEN" required:"true"`

	RunnerImage       string `key:"RUNNER_IMAGE" required:"true"`
	RegistryURL       string `key:"DOCKER_REGISTRY_URL" required:"true"`
	RegistryUsername  string `key:"DOCKER_REGISTRY_USERNAME" required:"true"`
	RegistryPassword  string `key:"DOCKER_REGISTRY_PASSWORD" required:"true"`
	RegistryMirrorURL string `key:"DOCKER_REGISTRY_MIRROR_URL" default-value:""`
	ArtifactoryToken  string `key:"ARTIFACTORY_TOKEN" required:"true"`

	LogLevel  string `key:"LOG_LEVEL" default-value:"info"`
	LogFormat string `key:"LOG_FORMAT" default-value:"text"`
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

func DecodeScaleSetsConfigs(configFilePath string) ([]ScaleSetConfig, []error) {
	var scaleSets []ScaleSetConfig

	fileContent, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, []error{fmt.Errorf("failed to read config file: %w", err)}
	}

	err = yaml.Unmarshal(fileContent, &scaleSets)
	if err != nil {
		return nil, []error{fmt.Errorf("failed to decode config file: %w", err)}
	}

	var errs []error

	// Validate config and set default values
	for i, scaleSet := range scaleSets {
		// Validate scaleSetName
		if strings.TrimSpace(scaleSet.ScaleSetName) == "" {
			errs = append(errs, fmt.Errorf("scale set %d: 'name' cannot be empty", i))
		}

		// Validate maxRunners and minRunners
		if scaleSet.MaxRunners < scaleSet.MinRunners {
			errs = append(errs, fmt.Errorf("scale set %s: 'maxRunners' (%d) cannot be less than 'minRunners' (%d)", scaleSet.ScaleSetName, scaleSet.MaxRunners, scaleSet.MinRunners))
		}

		// Validate labels
		if scaleSet.Labels == nil || len(scaleSet.Labels) == 0 {
			errs = append(errs, fmt.Errorf("scale set %s: 'labels' cannot be empty", scaleSet.ScaleSetName))
		}

		// Set default runner group if not provided
		if strings.TrimSpace(scaleSet.RunnerGroup) == "" {
			scaleSet.RunnerGroup = "default"
		}

		// Validate DockerHosts
		if scaleSet.DockerHosts == nil || len(scaleSet.DockerHosts) == 0 {
			errs = append(errs, fmt.Errorf("scale set %s: 'dockerHosts' cannot be empty", scaleSet.ScaleSetName))
		} else {
			for j, host := range scaleSet.DockerHosts {
				// Validate dockerHostName
				if strings.TrimSpace(host.Name) == "" {
					errs = append(errs, fmt.Errorf("scale set %s: docker host %d: 'name' cannot be empty", scaleSet.ScaleSetName, j))
				}

				// Validate dockerHostUrl
				if strings.TrimSpace(host.Url) == "" {
					errs = append(errs, fmt.Errorf("scale set %s: docker host %s: 'url' cannot be empty", scaleSet.ScaleSetName, host.Name))
				} else if _, err := url.ParseRequestURI(host.Url); err != nil {
					errs = append(errs, fmt.Errorf("scale set %s: docker host %s: invalid 'url': %v", scaleSet.ScaleSetName, host.Name, err))
				}

				// Validate dockerHostRuntime
				if strings.TrimSpace(host.Runtime) == "" {
					errs = append(errs, fmt.Errorf("scale set %s: docker host %s: 'runtime' cannot be empty", scaleSet.ScaleSetName, host.Name))
				} else if host.Runtime != "runc" && host.Runtime != "sysbox-runc" {
					errs = append(errs, fmt.Errorf("scale set %s: docker host %s: unsupported 'runtime': %s. Supported runtimes are: runc, sysbox-runc", scaleSet.ScaleSetName, host.Name, host.Runtime))
				}
			}
		}
	}

	return scaleSets, errs
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

	scaleSets, errs := DecodeScaleSetsConfigs(autoscalerCfg.ConfigurationFilePath)
	if errs != nil {
		configErrors = append(configErrors, errs...)
	} else {
		autoscalerCfg.ScaleSetsConfigs = scaleSets
	}

	return autoscalerCfg, configErrors
}
