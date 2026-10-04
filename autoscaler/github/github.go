// Package github isole tous les appels à l'API GitHub derrière un client
// unique. Le reste du programme n'a plus à connaître la mécanique du service
// actions : scale set, session de messages, config JIT et désenregistrement des
// runners passent tous par ici.
//
// Le package `scaler` continue d'importer la lib pour ses *types* — l'interface
// `listener.Scaler` impose `*JobStarted` et `*JobCompleted` dans ses signatures
// — mais plus aucun appel réseau vers GitHub n'y subsiste.
package github

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	scaleset "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/google/uuid"
	"mgarnier11.fr/docker-autoscaler/config"
)

// Sentinelles réexportées. Elles permettent à l'appelant de classer le résultat
// d'un désenregistrement — la seule source fiable pour savoir si un runner
// travaille — sans importer la lib juste pour comparer une erreur.
var (
	ErrJobStillRunning = scaleset.JobStillRunningError
	ErrRunnerNotFound  = scaleset.RunnerNotFoundError
)

// systemInfo identifie l'autoscaler auprès du service actions.
func systemInfo() scaleset.SystemInfo {
	return scaleset.SystemInfo{
		System:    "dockerscaleset",
		Subsystem: "dockerscaleset",
		CommitSHA: "NA",    // TODO: passer ce parametre au build
		Version:   "0.1.0", // TODO: passer ce parametre au build
	}
}

type GithubClient struct {
	client *scaleset.Client
	logger *slog.Logger
	config *config.AutoscalerConfig

	scaleSet *scaleset.RunnerScaleSet
	session  *scaleset.MessageSessionClient
}

// New construit le client. Aucun appel réseau n'est fait ici : le scale set et
// la session sont ouverts explicitement, pour que l'ordre des opérations reste
// visible chez l'appelant.
func New(logger *slog.Logger, config *config.AutoscalerConfig) (*GithubClient, error) {
	client, err := scaleset.NewClientWithPersonalAccessToken(
		scaleset.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     config.RegistrationURL,
			PersonalAccessToken: config.Token,
			SystemInfo:          systemInfo(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scaleset client: %w", err)
	}

	return &GithubClient{
		client: client,
		logger: logger,
		config: config,
	}, nil
}

// EnsureScaleSet récupère le scale set par son nom, et le crée s'il n'existe
// pas encore. Le retrouver plutôt que le recréer est ce qui permet à un
// redémarrage de conserver les jobs déjà en file et l'ID du scale set.
//
// Conséquence : les labels ne sont appliqués qu'à la création. Modifier LABELS
// sur un scale set existant n'a aucun effet tant qu'il n'est pas recréé.
func (this *GithubClient) EnsureScaleSet(ctx context.Context) error {
	runnerGroupID, err := this.runnerGroupID(ctx)
	if err != nil {
		return err
	}

	scaleSet, err := this.client.GetRunnerScaleSet(ctx, runnerGroupID, this.config.ScaleSetName)
	if err != nil {
		return fmt.Errorf("failed to get runner scale set: %w", err)
	}

	if scaleSet != nil {
		this.logger.Info(
			"Found existing runner scale set",
			slog.String("scaleSetName", this.config.ScaleSetName),
			slog.Int("scaleSetID", scaleSet.ID),
		)
		this.scaleSet = scaleSet
		return nil
	}

	this.logger.Info(
		"Runner scale set not found, creating a new one",
		slog.String("scaleSetName", this.config.ScaleSetName),
	)

	labels := make([]scaleset.Label, len(this.config.Labels))
	for i, name := range this.config.Labels {
		labels[i] = scaleset.Label{Name: strings.TrimSpace(name)}
	}

	scaleSet, err = this.client.CreateRunnerScaleSet(ctx, &scaleset.RunnerScaleSet{
		Name:          this.config.ScaleSetName,
		RunnerGroupID: runnerGroupID,
		Labels:        labels,
		RunnerSetting: scaleset.RunnerSetting{
			DisableUpdate: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create runner scale set: %w", err)
	}

	this.logger.Info(
		"Created runner scale set",
		slog.String("scaleSetName", this.config.ScaleSetName),
		slog.Int("scaleSetID", scaleSet.ID),
	)
	this.scaleSet = scaleSet
	return nil
}

// runnerGroupID résout le nom du groupe de runners. "default" vaut toujours 1,
// ce qui évite un appel API dans le cas courant.
func (this *GithubClient) runnerGroupID(ctx context.Context) (int, error) {
	if this.config.RunnerGroup == "default" {
		this.logger.Info(
			"Using runner group",
			slog.String("runnerGroup", this.config.RunnerGroup),
			slog.Int("runnerGroupID", 1),
		)
		return 1, nil
	}

	runnerGroup, err := this.client.GetRunnerGroupByName(ctx, this.config.RunnerGroup)
	if err != nil {
		return 0, fmt.Errorf("failed to get runner group ID: %w", err)
	}

	this.logger.Info(
		"Using runner group",
		slog.String("runnerGroup", this.config.RunnerGroup),
		slog.Int("runnerGroupID", runnerGroup.ID),
	)
	return runnerGroup.ID, nil
}

// OpenMessageSession ouvre la session de long poll sur la file du scale set.
func (this *GithubClient) OpenMessageSession(ctx context.Context) error {
	if this.scaleSet == nil {
		return fmt.Errorf("runner scale set is not initialized")
	}

	// Le propriétaire de la session identifie l'autoscaler côté github.
	owner, err := os.Hostname()
	if err != nil {
		owner = uuid.NewString()
		this.logger.Info("Failed to get hostname, fallback to uuid", "uuid", owner, "error", err)
	}

	session, err := this.client.MessageSessionClient(ctx, this.scaleSet.ID, owner)
	if err != nil {
		return fmt.Errorf("failed to create message session client: %w", err)
	}

	this.session = session
	return nil
}

func (this *GithubClient) ScaleSetID() int {
	if this.scaleSet == nil {
		return 0
	}
	return this.scaleSet.ID
}

// MessageSession expose la session sous la forme attendue par le listener de la
// lib, qui ne demande qu'une interface.
func (this *GithubClient) MessageSession() listener.Client {
	return this.session
}

// GenerateJitConfig produit la configuration à usage unique d'un runner. Le
// RunnerReference renvoyé porte l'ID github du runner, seul moyen de le
// désenregistrer plus tard sans le rechercher par son nom.
func (this *GithubClient) GenerateJitConfig(ctx context.Context, runnerName string) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
	if this.scaleSet == nil {
		return nil, fmt.Errorf("runner scale set is not initialized")
	}

	jit, err := this.client.GenerateJitRunnerConfig(
		ctx,
		&scaleset.RunnerScaleSetJitRunnerSetting{Name: runnerName},
		this.scaleSet.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JIT config: %w", err)
	}
	if jit.Runner == nil {
		return nil, fmt.Errorf("github returned a JIT config without a runner reference")
	}
	// Un ID nul rendrait le désenregistrement dangereux : github répondrait
	// « introuvable », donc « runner libre », et l'arrêt détruirait un conteneur
	// peut-être en plein job. On refuse cet ID à la source, seul endroit où il
	// entre dans le programme.
	if jit.Runner.ID <= 0 {
		return nil, fmt.Errorf("github returned a runner reference with a non-positive id: %d", jit.Runner.ID)
	}

	return jit, nil
}

// RemoveRunner désenregistre un runner. L'appel est atomique côté github, ce
// qui en fait aussi la seule sonde fiable de l'occupation d'un runner :
// github refuse de désenregistrer un runner qui exécute un job et renvoie
// ErrJobStillRunning. Un succès garantit à l'inverse qu'aucun job ne pourra
// plus lui être affecté, puisque le runner n'existe plus.
func (this *GithubClient) RemoveRunner(ctx context.Context, runnerID int64) error {
	return this.client.RemoveRunner(ctx, runnerID)
}

// DeleteScaleSet supprime le scale set. Les jobs en file sont perdus et un
// prochain démarrage en recréera un avec un ID différent.
func (this *GithubClient) DeleteScaleSet(ctx context.Context) error {
	if this.scaleSet == nil {
		return nil
	}
	return this.client.DeleteRunnerScaleSet(ctx, this.scaleSet.ID)
}

// SessionStatistics renvoie les compteurs relevés à l'ouverture de la session.
// Ils sont agrégés : ils disent combien de runners github voit occupés, jamais
// lesquels — de quoi contrôler la cohérence d'une reprise d'état, pas de
// reconstituer le détail. Aucun appel réseau, la lib met la valeur en cache.
func (this *GithubClient) SessionStatistics() *scaleset.RunnerScaleSetStatistic {
	if this.session == nil {
		return nil
	}
	return this.session.Session().Statistics
}

func (this *GithubClient) CloseSession(ctx context.Context) error {
	if this.session == nil {
		return nil
	}
	return this.session.Close(ctx)
}
