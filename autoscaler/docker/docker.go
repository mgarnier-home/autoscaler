// Package docker isole tout ce qui touche au daemon docker : clients, image du
// runner, volumes de cache, cycle de vie des conteneurs runner.
//
// Il ne connaît ni github ni le scaler. La configuration JIT d'un runner y entre
// sous forme de chaîne opaque, pas sous le type de la lib github : un conteneur
// à démarrer n'a pas à savoir d'où vient son jeton.
package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const (
	runnerNpmCacheVolumeName = "runner-npm-cache"

	// Labels posés sur les conteneurs runner. Ils portent tout ce dont
	// l'autoscaler a besoin pour reconstruire son état au démarrage depuis
	// docker plutôt que depuis un fichier : un label vit sur le conteneur, donc
	// il ne peut pas décrire un conteneur qui n'existe plus.
	//
	// Ils servent aussi à retrouver ces conteneurs à la main :
	//   docker ps -a --filter "label=mgarnier11.fr/docker-autoscaler.scaleset=<nom>"
	runnerScaleSetLabel = "mgarnier11.fr/docker-autoscaler.scaleset"
	runnerNameLabel     = "mgarnier11.fr/docker-autoscaler.runner"
	runnerIDLabel       = "mgarnier11.fr/docker-autoscaler.runner-id"
)

// ShortID rend l'identifiant court d'un conteneur, celui que docker lui-même
// affiche. Les 64 caractères complets noient les lignes de log sans jamais
// servir : `docker` accepte le préfixe partout.
func ShortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

type Client struct {
	*dockerclient.Client
	Runtime string

	logger *slog.Logger
}

// newClients ouvre un client par hôte docker configuré.
func newClients(logger *slog.Logger, dockerHosts []string, runtime string) ([]*Client, error) {
	var clients []*Client

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
		clients = append(clients, &Client{
			Client:  client,
			Runtime: runtime,
			logger:  logger,
		})
	}

	return clients, nil
}

// Pool est l'ensemble des hôtes docker sur lesquels l'autoscaler place ses
// runners, et le seul point d'entrée du package.
//
// Il porte la responsabilité « pour chaque hôte », qui était auparavant
// éparpillée dans le scaler : préparation des hôtes au démarrage, répartition
// des nouveaux runners, fermeture des clients. Les méthodes par hôte restent
// privées pour qu'il n'y ait qu'une façon de s'adresser aux hôtes.
type Pool struct {
	clients []*Client
	logger  *slog.Logger

	// nextIndex tourne sur les hôtes à chaque runner démarré. Le mutex est le
	// sien : plusieurs démarrages peuvent se croiser.
	mu        sync.Mutex
	nextIndex int
}

func NewPool(logger *slog.Logger, dockerHosts []string, runtime string) (*Pool, error) {
	clients, err := newClients(logger, dockerHosts, runtime)
	if err != nil {
		return nil, err
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("no usable docker host in %q", strings.Join(dockerHosts, ","))
	}

	return &Pool{clients: clients, logger: logger}, nil
}

// Next désigne l'hôte qui accueillera le prochain runner
func (this *Pool) Next() *Client {
	this.mu.Lock()
	defer this.mu.Unlock()

	client := this.clients[this.nextIndex]
	this.logger.Info(
		"Selected docker client",
		slog.String("dockerHost", client.DaemonHost()),
		slog.Int("clientIndex", this.nextIndex),
	)
	this.nextIndex = (this.nextIndex + 1) % len(this.clients)

	return client
}

// PullRunnerImage tire l'image du runner sur chaque hôte.
func (this *Pool) PullRunnerImage(ctx context.Context, params *PullImageParams) error {
	for _, client := range this.clients {
		this.logger.Info("Pulling runner image", slog.String("dockerHost", client.DaemonHost()))
		if err := client.pullRunnerImage(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

// CreateCacheVolumes crée les volumes de cache partagés sur chaque hôte.
func (this *Pool) CreateCacheVolumes(ctx context.Context) error {
	for _, client := range this.clients {
		this.logger.Info("Creating cache volumes", slog.String("dockerHost", client.DaemonHost()))
		if err := client.createCacheVolumes(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ListRunners rassemble les conteneurs runner encore en cours sur tous les
// hôtes. Chaque conteneur porte le client de l'hôte où il a été trouvé, sans
// quoi l'appelant ne saurait pas à qui parler pour le supprimer.
func (this *Pool) ListRunners(ctx context.Context, scaleSetName string) ([]RunnerContainer, error) {
	var runners []RunnerContainer

	for _, client := range this.clients {
		this.logger.Info("Listing runner containers", slog.String("dockerHost", client.DaemonHost()))
		hostRunners, err := client.listRunners(ctx, scaleSetName)
		if err != nil {
			return nil, err
		}
		runners = append(runners, hostRunners...)
	}

	return runners, nil
}

// Close ferme tous les clients. Les erreurs sont agrégées plutôt que remontées
// à la première : un hôte injoignable ne doit pas empêcher de fermer les autres.
func (this *Pool) Close() error {
	var errs []error
	for _, client := range this.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close docker client for host %s: %w", client.DaemonHost(), err))
		}
	}
	return errors.Join(errs...)
}

type PullImageParams struct {
	RegistryURL      string
	RegistryUsername string
	RegistryPassword string
	RunnerImage      string
}

func (this *Client) pullRunnerImage(ctx context.Context, params *PullImageParams) error {
	// Check if image already exists locally
	_, localErr := this.ImageInspect(ctx, params.RunnerImage)
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
	pull, err := this.ImagePull(ctx, params.RunnerImage, image.PullOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		// Pull failed.
		// If we already have the image locally, continue.
		if imageExistsLocally {
			this.logger.Warn(
				"Failed to pull image, using local copy",
				slog.String("dockerHost", this.DaemonHost()),
				slog.String("image", params.RunnerImage),
				slog.String("error", err.Error()),
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

func (this *Client) createCacheVolumes(ctx context.Context) error {
	for _, volName := range []string{runnerNpmCacheVolumeName} {
		_, err := this.VolumeCreate(ctx, volume.CreateOptions{
			Name: volName,
		})
		if err != nil {
			return fmt.Errorf("failed to create volume %s: %w", volName, err)
		}
	}
	return nil
}

type StartRunnerParams struct {
	ContainerName string
	RunnerID      int64
	ScaleSetName  string

	// JitConfig est la configuration à usage unique du runner, déjà encodée.
	// Volontairement une chaîne opaque : ce package n'a pas à dépendre de la lib
	// github pour démarrer un conteneur.
	JitConfig string

	RegistryURL      string
	RegistryUsername string
	RegistryPassword string
	RunnerImage      string
	BuildkitHostURL  string
	PipeRunnerLogs   bool
}

func (this *Client) StartRunner(ctx context.Context, params *StartRunnerParams) (containerID string, err error) {
	runnerContainer, err := this.ContainerCreate(
		ctx,
		&container.Config{
			Image: params.RunnerImage,
			User:  "runner",
			Cmd:   []string{"/home/runner/run.sh"},
			Labels: map[string]string{
				runnerScaleSetLabel: params.ScaleSetName,
				runnerNameLabel:     params.ContainerName,
				runnerIDLabel:       strconv.FormatInt(params.RunnerID, 10),
			},
			Env: []string{
				fmt.Sprintf("ACTIONS_RUNNER_INPUT_JITCONFIG=%s", params.JitConfig),
				fmt.Sprintf("DOCKER_REGISTRY_URL=%s", params.RegistryURL),
				fmt.Sprintf("DOCKER_REGISTRY_USERNAME=%s", params.RegistryUsername),
				fmt.Sprintf("DOCKER_REGISTRY_PASSWORD=%s", params.RegistryPassword),
				fmt.Sprintf("BUILDKIT_HOST_URL=%s", params.BuildkitHostURL),
				"START_DOCKER_SERVICE=true",
			},
		},
		&container.HostConfig{
			Runtime: this.Runtime,
			// Un runner est éphémère : il exécute un job puis sort. AutoRemove le
			// fait disparaître à cet instant, y compris quand l'autoscaler n'est
			// plus là pour le supprimer — c'est ce qui empêche les conteneurs de
			// s'accumuler entre deux redémarrages.
			//
			// En contrepartie les logs d'un runner mort ne sont plus consultables
			// avec `docker logs` : pour déboguer un runner qui ne démarre pas,
			// passer PIPE_RUNNER_LOGS à true.
			//
			// Les caches ne risquent rien : AutoRemove ne détruit que les volumes
			// anonymes, or ceux montés ici sont nommés.
			AutoRemove: true,
			ExtraHosts: []string{
				// Permet au runner de joindre le conteneur buildkit qui publie
				// son port sur l'hôte docker.
				"buildkit:host-gateway",
			},
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: runnerNpmCacheVolumeName,
					Target: "/home/runner/.npm",
				},
			},
		},
		nil, nil,
		params.ContainerName,
	)

	if err != nil {
		return "", fmt.Errorf("failed to create runner container: %w", err)
	}

	if err := this.ContainerStart(ctx, runnerContainer.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	if params.PipeRunnerLogs {
		this.attachContainerLogsToStdout(ctx, runnerContainer.ID)
	}

	return runnerContainer.ID, nil
}

// RemoveContainer supprime un conteneur.
//
// Un conteneur introuvable n'est pas une erreur mais le résultat recherché :
// les conteneurs runner portent AutoRemove, donc docker peut les avoir ramassés
// avant l'appel.
func (this *Client) RemoveContainer(ctx context.Context, containerID string) error {
	err := this.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	if err != nil && !dockerclient.IsErrNotFound(err) {
		return fmt.Errorf("failed to remove container %s: %w", ShortID(containerID), err)
	}
	return nil
}

// RunnerContainer est un conteneur runner retrouvé sur un hôte docker, décodé
// depuis ses labels. C'est de quoi reconstruire l'état de l'autoscaler après un
// redémarrage sans avoir persisté quoi que ce soit.
type RunnerContainer struct {
	ID       string
	Name     string
	RunnerID int64

	// Client est l'hôte docker sur lequel ce conteneur tourne. Sans lui,
	// l'appelant ne saurait pas à quel daemon s'adresser pour le supprimer.
	Client *Client
}

// listRunners renvoie les conteneurs runner du scale set encore en cours
// d'exécution sur cet hôte.
//
// Sans All:true, docker ne renvoie que les conteneurs qui tournent — exactement
// ce qu'on veut : un conteneur arrêté ne peut plus prendre de job, et AutoRemove
// le fait de toute façon disparaître.
func (this *Client) listRunners(ctx context.Context, scaleSetName string) ([]RunnerContainer, error) {
	containers, err := this.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("%s=%s", runnerScaleSetLabel, scaleSetName)),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list runner containers on %s: %w", this.DaemonHost(), err)
	}

	runners := make([]RunnerContainer, 0, len(containers))
	for _, runnerContainer := range containers {
		name, runnerID, ok := runnerFromLabels(runnerContainer.Labels)
		if !ok {
			// Conteneur créé avant l'introduction du label, ou label illisible.
			// Le rendre sans son ID github serait pire que de l'ignorer :
			// l'arrêt ne pourrait pas sonder son occupation. Il reste en vie et
			// terminera son job ; c'est à nettoyer à la main.
			this.logger.Warn(
				"Skipping runner container without a usable runner id label",
				slog.String("dockerHost", this.DaemonHost()),
				slog.String("containerID", ShortID(runnerContainer.ID)),
			)
			continue
		}

		runners = append(runners, RunnerContainer{
			ID:       runnerContainer.ID,
			Name:     name,
			RunnerID: runnerID,
			Client:   this,
		})
	}

	return runners, nil
}

// runnerFromLabels reconstruit l'identité d'un runner à partir des labels de son
// conteneur.
//
// Un ID absent, illisible ou non strictement positif fait échouer la reprise
// plutôt que de produire un ID nul : github répondrait « introuvable » au
// désenregistrement, donc « runner libre », et l'arrêt détruirait un conteneur
// peut-être en plein job.
func runnerFromLabels(labels map[string]string) (name string, runnerID int64, ok bool) {
	name = labels[runnerNameLabel]
	if name == "" {
		return "", 0, false
	}

	id, err := strconv.ParseInt(labels[runnerIDLabel], 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false
	}

	return name, id, true
}

type prefixWriter struct {
	w      io.Writer
	prefix []byte
	buf    bytes.Buffer
}

func (p *prefixWriter) Write(data []byte) (int, error) {
	p.buf.Write(data)

	for {
		line, err := p.buf.ReadBytes('\n')
		if err == io.EOF {
			// incomplete line, put it back
			p.buf.Write(line)
			break
		}

		if _, err := p.w.Write(p.prefix); err != nil {
			return len(data), err
		}
		if _, err := p.w.Write(line); err != nil {
			return len(data), err
		}
	}

	return len(data), nil
}

func (this *Client) attachContainerLogsToStdout(ctx context.Context, containerID string) {
	// On démarre une goroutine afin de ne pas bloquer l'exécution du programme principal.
	// Cette goroutine va lire les logs du conteneur et les afficher sur la sortie standard.
	go func() {
		logsReader, err := this.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Tail:       "all",
		})
		if err != nil {
			this.logger.Warn(
				"Failed to attach container logs",
				slog.String("dockerHost", this.DaemonHost()),
				slog.String("containerID", ShortID(containerID)),
				slog.String("error", err.Error()),
			)
			return
		}
		defer logsReader.Close()

		stdout := &prefixWriter{
			w:      os.Stdout,
			prefix: []byte(fmt.Sprintf("[%s] ", ShortID(containerID))),
		}

		stderr := &prefixWriter{
			w:      os.Stderr,
			prefix: []byte(fmt.Sprintf("[%s] ", ShortID(containerID))),
		}

		// Le conteneur portant AutoRemove, docker le supprime en fin de job et le
		// flux se rompt : ce n'est pas une anomalie, seulement la fin du runner.
		if _, err := stdcopy.StdCopy(stdout, stderr, logsReader); err != nil && ctx.Err() == nil {
			this.logger.Debug(
				"Container log stream ended",
				slog.String("dockerHost", this.DaemonHost()),
				slog.String("containerID", ShortID(containerID)),
				slog.String("error", err.Error()),
			)
		}
	}()
}
