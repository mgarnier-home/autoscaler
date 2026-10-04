package scaler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	githubScaleSet "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/google/uuid"
	"mgarnier11.fr/docker-autoscaler/config"
	"mgarnier11.fr/docker-autoscaler/docker"
	"mgarnier11.fr/docker-autoscaler/github"
)

type Scaler struct {
	logger       *slog.Logger
	githubClient *github.GithubClient
	config       *config.AutoscalerConfig

	listener *listener.Listener

	runners runnerState

	// Le pool porte la répartition sur les hôtes docker et la fermeture des
	// clients ; le scaler n'itère plus jamais sur les hôtes lui-même.
	dockerPool *docker.Pool
}

func (this *Scaler) Run(ctx context.Context) error {
	this.logger.Info("Starting listener for runner scale set", slog.Int("scaleSetID", this.githubClient.ScaleSetID()))

	return this.listener.Run(ctx, this)
}

func (this *Scaler) Shutdown(ctx context.Context) {
	// Seuls les runners libres sont détruits. Ceux qui exécutent un job restent
	// en vie : un runner est autonome une fois démarré — config JIT, logs poussés
	// directement sur github — donc il termine son job sans l'autoscaler, et le
	// détruire couperait ce job. AutoRemove fait disparaître son conteneur dès
	// qu'il sortira, sans que personne ait à repasser derrière.
	//
	// drainIdle rend la map en une seule opération : chaque runner coûte ensuite
	// un appel github puis un appel docker, et aucune I/O ne doit se faire en
	// tenant le mutex de runnerState.
	idleRunners := this.runners.drainIdle()

	removedCount := 0
	for name, info := range idleRunners {
		if this.removeIdleRunner(ctx, name, info) {
			removedCount++
		}
	}

	// Ce qui reste debout : les runners occupés, plus les « libres » que github a
	// signalés occupés ou dont le sort n'a pas pu être établi.
	_, busyCount := this.runners.counts()
	survivingCount := busyCount + len(idleRunners) - removedCount

	this.logger.Info(
		"Runner containers cleaned up",
		slog.Int("removedCount", removedCount),
		slog.Int("keptCount", survivingCount),
	)

	this.runners.clear()

	// Close the docker clients
	if err := this.dockerPool.Close(); err != nil {
		this.logger.Error("Failed to close docker clients", slog.String("error", err.Error()))
	}

	// Close the message session client
	if err := this.githubClient.CloseSession(ctx); err != nil {
		this.logger.Error("Failed to close message session", slog.String("error", err.Error()))
	}

	// Delete the runner scale set, uniquement si c'est explicitement demandé.
	// Sur un simple redémarrage on le conserve : le supprimer perdrait les jobs
	// déjà assignés et forcerait la recréation d'un scale set avec un nouvel ID.
	if !this.config.DeleteScaleSetOnShutdown {
		this.logger.Info(
			"Keeping runner scale set",
			slog.Int("scaleSetID", this.githubClient.ScaleSetID()),
		)
		return
	}

	// Supprimer le scale set pendant que des runners travaillent encore couperait
	// leurs jobs, ce que tout l'arrêt cherche justement à éviter.
	if survivingCount > 0 {
		this.logger.Warn(
			"Not deleting runner scale set: runner containers are still alive",
			slog.Int("scaleSetID", this.githubClient.ScaleSetID()),
			slog.Int("runners", survivingCount),
		)
		return
	}

	this.logger.Info(
		"Deleting runner scale set",
		slog.Int("scaleSetID", this.githubClient.ScaleSetID()),
	)
	if err := this.githubClient.DeleteScaleSet(context.WithoutCancel(ctx)); err != nil {
		this.logger.Error(
			"Failed to delete runner scale set",
			slog.Int("scaleSetID", this.githubClient.ScaleSetID()),
			slog.String("error", err.Error()),
		)
	}
}

// removeIdleRunner désenregistre un runner que l'état local croit libre, puis
// supprime son conteneur si github a confirmé qu'il l'était. Renvoie true si le
// conteneur a bien été supprimé.
func (this *Scaler) removeIdleRunner(ctx context.Context, name string, info runnerInfo) bool {
	removeErr := this.githubClient.RemoveRunner(ctx, info.runnerID)

	if removeErr == nil || errors.Is(removeErr, github.ErrRunnerNotFound) {
		if err := info.dockerClient.RemoveContainer(ctx, info.containerID); err != nil {
			this.logger.Error(
				"Failed to remove idle runner container",
				slog.String("name", name),
				slog.String("containerID", docker.ShortID(info.containerID)),
				slog.String("error", err.Error()),
			)
			return false
		}
	} else if errors.Is(removeErr, github.ErrJobStillRunning) {
		this.logger.Info(
			"Leaving runner container alive: a job is still running on it",
			slog.String("name", name),
			slog.String("containerID", docker.ShortID(info.containerID)),
		)
		return false
	} else {
		this.logger.Warn(
			"Leaving runner container alive: failed to determine if it is running a job",
			slog.String("name", name),
			slog.String("containerID", docker.ShortID(info.containerID)),
			slog.String("error", removeErr.Error()),
		)
		return false
	}

	this.logger.Info(
		"Removed idle runner container",
		slog.String("name", name),
		slog.String("containerID", docker.ShortID(info.containerID)),
	)
	return true
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

	// RemoveContainer absorbe le cas « déjà disparu » : le conteneur porte
	// AutoRemove, donc un runner éphémère sort de lui-même en fin de job et
	// docker le ramasse souvent avant que ce message n'arrive.
	if err := info.dockerClient.RemoveContainer(ctx, info.containerID); err != nil {
		this.logger.Error(
			"Failed to remove runner container",
			slog.String("name", jobInfo.RunnerName),
			slog.String("containerID", docker.ShortID(info.containerID)),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (this *Scaler) startRunner(ctx context.Context) (string, error) {
	containerName := fmt.Sprintf("runner-%s", uuid.NewString()[:8])

	jit, err := this.githubClient.GenerateJitConfig(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to generate JIT config: %w", err)
	}

	// Select the next Docker client in a round-robin fashion
	client := this.dockerPool.Next()

	containerID, err := client.StartRunner(
		ctx,
		&docker.StartRunnerParams{
			ContainerName:    containerName,
			RunnerID:         int64(jit.Runner.ID),
			ScaleSetName:     this.config.ScaleSetName,
			JitConfig:        jit.EncodedJITConfig,
			RegistryURL:      this.config.RegistryURL,
			RegistryUsername: this.config.RegistryUsername,
			RegistryPassword: this.config.RegistryPassword,
			RunnerImage:      this.config.RunnerImage,
			BuildkitHostURL:  this.config.BuildkitHostURL,
			PipeRunnerLogs:   this.config.PipeRunnerLogs,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to start runner container: %w", err)
	}

	// L'ID github du runner est relevé maintenant : c'est ce qui permettra de le
	// désenregistrer à l'arrêt sans avoir à le rechercher par son nom. Il est
	// aussi gravé en label sur le conteneur, pour survivre à un redémarrage de
	// l'autoscaler.
	this.runners.addIdle(containerName, runnerInfo{
		containerID:  containerID,
		dockerClient: client,
		runnerID:     int64(jit.Runner.ID),
	})

	return containerName, nil
}

var _ listener.Scaler = (*Scaler)(nil)
