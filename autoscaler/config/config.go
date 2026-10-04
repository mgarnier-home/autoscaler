package config

import (
	"log/slog"
	"os"
	"strings"

	"mgarnier11.fr/docker-autoscaler/utils"
)

type AutoscalerConfig struct {
	RegistrationURL string `key:"REGISTRATION_URL" required:"true"`
	Token           string `key:"GITHUB_TOKEN" required:"true"`

	RunnerImage      string `key:"RUNNER_IMAGE" required:"true"`
	RegistryURL      string `key:"DOCKER_REGISTRY_URL" required:"true"`
	RegistryUsername string `key:"DOCKER_REGISTRY_USERNAME" required:"true"`
	RegistryPassword string `key:"DOCKER_REGISTRY_PASSWORD" required:"true"`
	BuildkitHostURL  string `key:"BUILDKIT_HOST_URL" default-value:""`

	LogLevel       string `key:"LOG_LEVEL" default-value:"info"`
	LogFormat      string `key:"LOG_FORMAT" default-value:"text"`
	PipeRunnerLogs bool   `key:"PIPE_RUNNER_LOGS" default-value:"false"`

	MaxRunners   int      `key:"MAX_RUNNERS" default-value:"10"`
	MinRunners   int      `key:"MIN_RUNNERS" default-value:"0"`
	ScaleSetName string   `key:"SCALE_SET_NAME" required:"true"`
	Labels       []string `key:"LABELS" required:"true"`
	RunnerGroup  string   `key:"RUNNER_GROUP" default-value:"default"`

	// Par défaut, un arrêt de l'autoscaler laisse le scale set en place sur
	// github : le supprimer à chaque redémarrage invaliderait les jobs déjà
	// assignés et changerait l'ID du scale set. On ne le supprime que lorsque
	// c'est explicitement demandé (démontage d'un environnement, tests e2e).
	DeleteScaleSetOnShutdown bool     `key:"DELETE_SCALE_SET_ON_SHUTDOWN" default-value:"false"`
	DockerHosts              []string `key:"DOCKER_HOSTS" required:"true"`
	Runtime                  string   `key:"RUNTIME" default-value:"runc"`
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

	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     lvl,
	}

	// Une valeur inconnue retombe sur le format lisible, comme pour le niveau de
	// log ci-dessus. Surtout pas de DiscardHandler ici : une faute de frappe
	// dans LOG_FORMAT rendrait l'autoscaler totalement muet.
	switch strings.ToLower(c.LogFormat) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	case "logfmt":
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	default:
		return slog.New(utils.NewPrettyHandler(os.Stdout, opts))
	}
}
