package scaler

import (
	"errors"
	"testing"

	githubScaleSet "github.com/actions/scaleset"
	"mgarnier11.fr/docker-autoscaler/config"
)

func newStatsTestScaler() *Scaler {
	return &Scaler{
		config: &config.AutoscalerConfig{ScaleSetName: "apollon", MinRunners: 2, MaxRunners: 10},
		runners: runnerState{
			idle: map[string]runnerInfo{"runner-a": {}, "runner-b": {}},
			busy: map[string]runnerInfo{"runner-c": {}},
		},
	}
}

func TestStatsRunners(t *testing.T) {
	got := newStatsTestScaler().Stats()

	want := RunnerStats{Idle: 2, Busy: 1, Min: 2, Max: 10}
	if got.ScaleSetName != "apollon" || got.Runners != want {
		t.Fatalf("Stats() = %+v, attendu scale set apollon et runners %+v", got, want)
	}
}

func TestStatsGithubAbsentAvantLePremierMessage(t *testing.T) {
	if got := newStatsTestScaler().Stats().Github; got != nil {
		t.Fatalf("Github = %+v, attendu nil", got)
	}
}

func TestStatsGithubReprendLesDernieresStatistiques(t *testing.T) {
	s := newStatsTestScaler()
	s.stats.RecordStatistics(&githubScaleSet.RunnerScaleSetStatistic{TotalAssignedJobs: 1})
	s.stats.RecordStatistics(&githubScaleSet.RunnerScaleSetStatistic{TotalAssignedJobs: 3, TotalRunningJobs: 2})

	got := s.Stats().Github
	if got == nil || got.TotalAssignedJobs != 3 || got.TotalRunningJobs != 2 {
		t.Fatalf("Github = %+v, attendu les dernières statistiques reçues", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt est vide")
	}
}

func TestStatsGithubIgnoreUnMessageSansStatistiques(t *testing.T) {
	s := newStatsTestScaler()
	s.stats.RecordStatistics(nil)

	if got := s.Stats().Github; got != nil {
		t.Fatalf("Github = %+v, attendu nil", got)
	}
}

func TestStatsEchecsDeDemarrage(t *testing.T) {
	s := newStatsTestScaler()

	s.stats.recordStart(errors.New("pull failed"))
	s.stats.recordStart(errors.New("no space left"))
	got := s.Stats()
	if got.ConsecutiveStartFailures != 2 {
		t.Fatalf("ConsecutiveStartFailures = %d, attendu 2", got.ConsecutiveStartFailures)
	}
	if got.LastStartError == nil || got.LastStartError.Message != "no space left" || got.LastStartError.At.IsZero() {
		t.Fatalf("LastStartError = %+v, attendu la dernière erreur datée", got.LastStartError)
	}

	// Un démarrage réussi remet le compteur à zéro mais garde la dernière erreur,
	// utile pour comprendre un incident passé.
	s.stats.recordStart(nil)
	got = s.Stats()
	if got.ConsecutiveStartFailures != 0 {
		t.Fatalf("ConsecutiveStartFailures = %d après un succès, attendu 0", got.ConsecutiveStartFailures)
	}
	if got.LastStartError == nil || got.LastStartError.Message != "no space left" {
		t.Fatalf("LastStartError = %+v, attendu conservée", got.LastStartError)
	}
}

func TestStatsSansEchecDeDemarrage(t *testing.T) {
	got := newStatsTestScaler().Stats()
	if got.ConsecutiveStartFailures != 0 || got.LastStartError != nil {
		t.Fatalf("Stats() = %+v, attendu aucun échec", got)
	}
}
