package scaler

import (
	"sync"
	"time"

	githubScaleSet "github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

// Stats est l'état de l'autoscaler servi par GET /stats, pour le dashboard.
type Stats struct {
	ScaleSetName string      `json:"scaleSetName"`
	Runners      RunnerStats `json:"runners"`

	// Github vaut nil tant qu'aucun message n'a été reçu du scale set.
	Github *GithubStats `json:"github"`

	ConsecutiveStartFailures int         `json:"consecutiveStartFailures"`
	LastStartError           *StartError `json:"lastStartError"`
}

type RunnerStats struct {
	Idle int `json:"idle"`
	Busy int `json:"busy"`
	Min  int `json:"min"`
	Max  int `json:"max"`
}

// GithubStats reprend les dernières statistiques du scale set telles que github
// les joint à chaque message : elles ne coûtent aucun appel d'API.
type GithubStats struct {
	githubScaleSet.RunnerScaleSetStatistic
	UpdatedAt time.Time `json:"updatedAt"`
}

type StartError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// statsState accumule ce que le listener et le scaler observent. Il est lu par
// le serveur HTTP pendant que le listener écrit, d'où le mutex.
type statsState struct {
	mu                       sync.Mutex
	github                   *GithubStats
	consecutiveStartFailures int
	lastStartError           *StartError
}

func (this *statsState) RecordStatistics(statistics *githubScaleSet.RunnerScaleSetStatistic) {
	if statistics == nil {
		return
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	this.github = &GithubStats{RunnerScaleSetStatistic: *statistics, UpdatedAt: time.Now()}
}

func (this *statsState) RecordJobStarted(*githubScaleSet.JobStarted)     {}
func (this *statsState) RecordJobCompleted(*githubScaleSet.JobCompleted) {}
func (this *statsState) RecordDesiredRunners(int)                        {}

// recordStart note le résultat d'un démarrage de runner. Un succès remet le
// compteur d'échecs à zéro mais garde la dernière erreur, utile pour comprendre
// un incident passé.
func (this *statsState) recordStart(err error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	if err == nil {
		this.consecutiveStartFailures = 0
		return
	}
	this.consecutiveStartFailures++
	this.lastStartError = &StartError{At: time.Now(), Message: err.Error()}
}

func (this *Scaler) Stats() Stats {
	idle, busy := this.runners.counts()

	this.stats.mu.Lock()
	defer this.stats.mu.Unlock()

	stats := Stats{
		ScaleSetName:             this.config.ScaleSetName,
		Runners:                  RunnerStats{Idle: idle, Busy: busy, Min: this.config.MinRunners, Max: this.config.MaxRunners},
		ConsecutiveStartFailures: this.stats.consecutiveStartFailures,
	}
	// Copies : le JSON est encodé hors du verrou.
	if this.stats.github != nil {
		github := *this.stats.github
		stats.Github = &github
	}
	if this.stats.lastStartError != nil {
		lastStartError := *this.stats.lastStartError
		stats.LastStartError = &lastStartError
	}
	return stats
}

var _ listener.MetricsRecorder = (*statsState)(nil)
