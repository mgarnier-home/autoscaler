package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	githubScaleSet "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
	"mgarnier11.fr/docker-autoscaler/config"
)

type Scaler struct {
	logger         *slog.Logger
	scalesetClient *githubScaleSet.Client
	config         *config.AutoscalerConfig

	runnerScaleSet       *githubScaleSet.RunnerScaleSet
	messageSessionClient *githubScaleSet.MessageSessionClient
	listener             *listener.Listener

	runners runnerState

	nextDockerClientIndex int
	dockerClientMutex     sync.Mutex
	dockerClients         []*DockerClientWithMetadata
}

func (this *Scaler) Run(ctx context.Context) error {
	this.logger.Info("Starting listener for runner scale set", slog.Int("scaleSetID", this.runnerScaleSet.ID))

	return this.listener.Run(ctx, this)
}

// removeGitHubRunner désenregistre un runner côté github.
//
// À appeler chaque fois qu'un conteneur est détruit sans que son runner ait
// terminé un job : github ne nettoie tout seul que les runners éphémères ayant
// effectivement exécuté un job. Sans ça, chaque conteneur de réserve détruit
// laisse un runner « offline » dans la liste de l'organisation, indéfiniment.
//
// Un échec n'est pas bloquant : si github a déjà supprimé le runner, l'appel
// répond en erreur et il n'y a rien à réparer.
func (this *Scaler) removeGitHubRunner(ctx context.Context, name string, runnerID int) {
	if runnerID == 0 {
		return
	}

	if err := this.scalesetClient.RemoveRunner(ctx, int64(runnerID)); err != nil {
		this.logger.Warn(
			"Failed to remove runner registration from github",
			slog.String("name", name),
			slog.Int("runnerID", runnerID),
			slog.String("error", err.Error()),
		)
		return
	}

	this.logger.Info(
		"Removed runner registration from github",
		slog.String("name", name),
		slog.Int("runnerID", runnerID),
	)
}

func (this *Scaler) Shutdown(ctx context.Context) {
	// Shutdown all the runners
	this.logger.Info("Shutting down runners")

	// On copie l'état sous verrou, puis on le relâche avant les appels docker et
	// github : ceux-ci peuvent être lents, voire pendre si un daemon ne répond
	// plus, et rien ne doit bloquer le reste du scaler pendant ce temps.
	// Un nom donné est soit dans idle soit dans busy, jamais dans les deux.
	this.runners.mu.Lock()
	toRemove := make(map[string]runnerInfo, len(this.runners.idle)+len(this.runners.busy))
	maps.Copy(toRemove, this.runners.idle)
	maps.Copy(toRemove, this.runners.busy)
	clear(this.runners.idle)
	clear(this.runners.busy)
	this.runners.mu.Unlock()

	for name, info := range toRemove {
		this.logger.Info(
			"Removing runner",
			slog.String("name", name),
			slog.String("containerID", info.containerID),
		)
		if err := info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true}); err != nil {
			this.logger.Error(
				"Failed to remove runner container",
				slog.String("name", name),
				slog.String("containerID", info.containerID),
				slog.String("error", err.Error()),
			)
		}

		this.removeGitHubRunner(ctx, name, info.runnerID)
	}

	// Close the docker clients
	for _, client := range this.dockerClients {
		if err := client.Close(); err != nil {
			this.logger.Error(
				"Failed to close docker client",
				slog.String("dockerHost", client.DaemonHost()),
				slog.String("error", err.Error()),
			)
		}
	}

	// Close the message session client
	this.messageSessionClient.Close(ctx)

	// Delete the runner scale set, uniquement si c'est explicitement demandé.
	// Sur un simple redémarrage on le conserve : le supprimer perdrait les jobs
	// déjà assignés et forcerait la recréation d'un scale set avec un nouvel ID.
	if !this.config.DeleteScaleSetOnShutdown {
		this.logger.Info(
			"Keeping runner scale set",
			slog.Int("scaleSetID", this.runnerScaleSet.ID),
		)
		return
	}

	this.logger.Info(
		"Deleting runner scale set",
		slog.Int("scaleSetID", this.runnerScaleSet.ID),
	)
	if err := this.scalesetClient.DeleteRunnerScaleSet(context.WithoutCancel(ctx), this.runnerScaleSet.ID); err != nil {
		this.logger.Error(
			"Failed to delete runner scale set",
			slog.Int("scaleSetID", this.runnerScaleSet.ID),
			slog.String("error", err.Error()),
		)
	}
}

// desiredRunnerCount calcule le nombre total de runners à maintenir.
//
// L'invariant voulu est : MinRunners conteneurs *libres* en permanence, en plus
// de ceux qui exécutent déjà un job. Dès qu'un job démarre sur le conteneur
// libre, un remplaçant est donc démarré aussitôt pour absorber le job suivant
// sans attendre le boot d'un runner.
//
// assignedJobs vient de github et busy de notre état local. Les deux peuvent
// diverger le temps qu'un message soit délivré (ou s'il est perdu) : on prend
// le plus grand des deux pour ne jamais sous-provisionner le pool.
//
// MaxRunners reste une borne dure : à saturation, la réserve est sacrifiée au
// profit des jobs réellement en attente.
func desiredRunnerCount(assignedJobs, busy, minRunners, maxRunners int) int {
	inFlight := max(assignedJobs, busy)
	return min(maxRunners, inFlight+minRunners)
}

func (this *Scaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	idle, busy := this.runners.counts()
	currentCount := idle + busy
	targetRunnerCount := desiredRunnerCount(count, busy, this.config.MinRunners, this.config.MaxRunners)

	switch {
	case targetRunnerCount == currentCount:
		// No scaling needed
		return currentCount, nil
	case targetRunnerCount > currentCount:
		// Scale up
		scaleUp := targetRunnerCount - currentCount
		this.logger.Info(
			"Scaling up runners",
			slog.Int("assignedJobs", count),
			slog.Int("idleCount", idle),
			slog.Int("busyCount", busy),
			slog.Int("desiredCount", targetRunnerCount),
			slog.Int("scaleUp", scaleUp),
		)

		for range scaleUp {
			if _, err := this.startRunner(ctx); err != nil {
				// On renvoie le compte réel : les runners déjà démarrés existent
				// bel et bien, et le prochain lot de messages retentera le reste.
				this.logger.Error("Failed to start runner", slog.String("error", err.Error()))
				return this.runners.count(), nil
			}
		}

		return this.runners.count(), nil
	default:
		// No need to handle scale down events, since:
		// 1. JobCompleted events will first remove runners
		// 2. If the count is still below the current runner count, the JobCompleted event will be delivered in the next batch.
		// 3. Removal after JobCompleted events is handled synchronously.
		// 4. If the job is cancelled, the JobCompleted event will still be delivered.
	}
	return this.runners.count(), nil
}

func (this *Scaler) HandleJobStarted(ctx context.Context, jobInfo *githubScaleSet.JobStarted) error {
	this.logger.Info(
		"Job started",
		slog.Int64("runnerRequestId", jobInfo.RunnerRequestID),
		slog.String("jobId", jobInfo.JobID),
	)

	err := this.runners.markBusy(jobInfo.RunnerName)
	if err != nil {
		this.logger.Error(
			"Failed to mark runner busy",
			slog.String("name", jobInfo.RunnerName),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (this *Scaler) HandleJobCompleted(ctx context.Context, jobInfo *githubScaleSet.JobCompleted) error {
	this.logger.Info("Job completed", slog.Int64("runnerRequestId", jobInfo.RunnerRequestID), slog.String("jobId", jobInfo.JobID))

	info, err := this.runners.markDone(jobInfo.RunnerName)
	if err != nil {
		// Le runner n'est plus suivi : message redélivré par github, runner déjà
		// nettoyé, ou état perdu suite à un redémarrage de l'autoscaler.
		// Il n'y a donc pas de conteneur à supprimer, et `info` est vide :
		// continuer déréférencerait un client docker nil.
		this.logger.Warn(
			"Failed to mark runner done, skipping container removal",
			slog.String("name", jobInfo.RunnerName),
			slog.String("error", err.Error()),
		)
		return nil
	}

	err = info.dockerClient.ContainerRemove(ctx, info.containerID, container.RemoveOptions{Force: true})
	if err != nil {
		this.logger.Error(
			"Failed to remove runner container",
			slog.String("name", jobInfo.RunnerName),
			slog.String("containerID", info.containerID),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (this *Scaler) startRunner(ctx context.Context) (string, error) {
	containerName := fmt.Sprintf("runner-%s", uuid.NewString()[:8])

	jit, err := this.GenerateJitRunnerConfig(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to generate JIT config: %w", err)
	}

	// Select the next Docker client in a round-robin fashion
	this.dockerClientMutex.Lock()
	client := this.dockerClients[this.nextDockerClientIndex]
	this.logger.Info(
		"Selected docker client",
		slog.String("dockerHost", client.DaemonHost()),
		slog.Int("clientIndex", this.nextDockerClientIndex),
	)
	this.nextDockerClientIndex = (this.nextDockerClientIndex + 1) % len(this.dockerClients)
	this.dockerClientMutex.Unlock()

	runnerID := 0
	if jit.Runner != nil {
		runnerID = jit.Runner.ID
	}

	containerID, err := startRunnerContainer(
		ctx,
		client,
		&startContainerParams{
			containerName:    containerName,
			jitConfig:        jit,
			registryURL:      this.config.RegistryURL,
			registryUsername: this.config.RegistryUsername,
			registryPassword: this.config.RegistryPassword,
			runnerImage:      this.config.RunnerImage,
			buildkitHostURL:  this.config.BuildkitHostURL,
		},
	)
	if err != nil {
		// Le runner est déjà enregistré côté github : sans ce nettoyage il y
		// resterait listé alors qu'aucun conteneur ne le porte.
		this.removeGitHubRunner(ctx, containerName, runnerID)
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	this.runners.addIdle(containerName, runnerInfo{
		containerID:  containerID,
		runnerID:     runnerID,
		dockerClient: client,
	})

	return containerName, nil
}

func (this *Scaler) GenerateJitRunnerConfig(ctx context.Context, containerName string) (*githubScaleSet.RunnerScaleSetJitRunnerConfig, error) {
	// Generate JIT config for the runner
	jit, err := this.scalesetClient.GenerateJitRunnerConfig(
		ctx,
		&githubScaleSet.RunnerScaleSetJitRunnerSetting{
			Name: containerName,
		},
		this.runnerScaleSet.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JIT config: %w", err)
	}

	return jit, nil
}

var _ listener.Scaler = (*Scaler)(nil)
