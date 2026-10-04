package scaler

import (
	"testing"
)

func TestDesiredRunnerCount(t *testing.T) {
	tests := []struct {
		name         string
		assignedJobs int
		busy         int
		minRunners   int
		maxRunners   int
		want         int
	}{
		{
			name: "au démarrage, la réserve est pré-démarrée",
			// Aucun job, aucun runner : on veut quand même le conteneur libre.
			assignedJobs: 0, busy: 0, minRunners: 1, maxRunners: 10, want: 1,
		},
		{
			name: "un job démarre sur la réserve, un remplaçant est créé",
			// Le job tourne (busy=1) : il faut 1 runner pour lui + 1 libre.
			assignedJobs: 1, busy: 1, minRunners: 1, maxRunners: 10, want: 2,
		},
		{
			name: "le job est assigné mais pas encore démarré",
			// github annonce le job avant le JobStarted : on provisionne déjà.
			assignedJobs: 1, busy: 0, minRunners: 1, maxRunners: 10, want: 2,
		},
		{
			name:         "deux jobs en parallèle gardent une seule réserve",
			assignedJobs: 2, busy: 2, minRunners: 1, maxRunners: 10, want: 3,
		},
		{
			name:         "le job est terminé, on retombe sur la réserve",
			assignedJobs: 0, busy: 0, minRunners: 1, maxRunners: 10, want: 1,
		},
		{
			name:         "MaxRunners est une borne dure, la réserve est sacrifiée",
			assignedJobs: 10, busy: 10, minRunners: 1, maxRunners: 10, want: 10,
		},
		{
			name:         "MaxRunners borne aussi la réserve seule",
			assignedJobs: 0, busy: 0, minRunners: 5, maxRunners: 3, want: 3,
		},
		{
			name:         "sans réserve demandée, on suit strictement les jobs",
			assignedJobs: 3, busy: 3, minRunners: 0, maxRunners: 10, want: 3,
		},
		{
			name:         "aucun job, aucune réserve : pool vide",
			assignedJobs: 0, busy: 0, minRunners: 0, maxRunners: 10, want: 0,
		},
		{
			name: "statistiques github en retard sur l'état local",
			// Un JobCompleted perdu laisserait busy en avance : on se cale sur
			// le plus grand des deux pour ne pas supprimer la réserve.
			assignedJobs: 0, busy: 2, minRunners: 1, maxRunners: 10, want: 3,
		},
		{
			name:         "état local en retard sur les statistiques github",
			assignedJobs: 4, busy: 1, minRunners: 1, maxRunners: 10, want: 5,
		},
		{
			name:         "plusieurs conteneurs libres demandés",
			assignedJobs: 2, busy: 2, minRunners: 3, maxRunners: 10, want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := desiredRunnerCount(tt.assignedJobs, tt.busy, tt.minRunners, tt.maxRunners)
			if got != tt.want {
				t.Fatalf(
					"desiredRunnerCount(assignedJobs=%d, busy=%d, min=%d, max=%d) = %d, attendu %d",
					tt.assignedJobs, tt.busy, tt.minRunners, tt.maxRunners, got, tt.want,
				)
			}
		})
	}
}

// Le cycle de vie complet d'un job avec MIN_RUNNERS=1 : à chaque étape il doit
// rester exactement un conteneur libre.
func TestDesiredRunnerCountKeepsOneSpareThroughJobLifecycle(t *testing.T) {
	const minRunners, maxRunners = 1, 10

	state := runnerState{
		idle: make(map[string]runnerInfo),
		busy: make(map[string]runnerInfo),
	}

	// Démarrage : le pool est vide, on doit provisionner la réserve.
	idle, busy := state.counts()
	if target := desiredRunnerCount(0, busy, minRunners, maxRunners); target != idle+busy+1 {
		t.Fatalf("au démarrage: cible %d, attendu %d", target, idle+busy+1)
	}
	state.addIdle("runner-1", runnerInfo{containerID: "c1"})

	// Un job arrive et démarre sur le conteneur libre.
	if err := state.markBusy("runner-1"); err != nil {
		t.Fatalf("markBusy: %v", err)
	}

	// Il ne reste plus de conteneur libre : la cible doit en réclamer un.
	idle, busy = state.counts()
	if idle != 0 || busy != 1 {
		t.Fatalf("après markBusy: idle=%d busy=%d, attendu idle=0 busy=1", idle, busy)
	}
	target := desiredRunnerCount(1, busy, minRunners, maxRunners)
	if target != 2 {
		t.Fatalf("pendant le job: cible %d, attendu 2", target)
	}
	if scaleUp := target - (idle + busy); scaleUp != 1 {
		t.Fatalf("pendant le job: scaleUp %d, attendu 1", scaleUp)
	}
	state.addIdle("runner-2", runnerInfo{containerID: "c2"})

	// Le pool est stable : 1 occupé + 1 libre, plus rien à démarrer.
	idle, busy = state.counts()
	if idle != 1 || busy != 1 {
		t.Fatalf("pool stabilisé: idle=%d busy=%d, attendu 1/1", idle, busy)
	}
	if target := desiredRunnerCount(1, busy, minRunners, maxRunners); target != idle+busy {
		t.Fatalf("pool stabilisé: cible %d, attendu %d", target, idle+busy)
	}

	// Fin du job : le conteneur est supprimé, la réserve reste en place.
	if _, err := state.markDone("runner-1"); err != nil {
		t.Fatalf("markDone: %v", err)
	}
	idle, busy = state.counts()
	if idle != 1 || busy != 0 {
		t.Fatalf("après le job: idle=%d busy=%d, attendu 1/0", idle, busy)
	}
	if target := desiredRunnerCount(0, busy, minRunners, maxRunners); target != idle+busy {
		t.Fatalf("après le job: cible %d, attendu %d", target, idle+busy)
	}
}

// Un runner dont le conteneur a échoué à démarrer n'est jamais ajouté à l'état,
// et markDone sur un nom inconnu doit renvoyer une info vide et une erreur
// plutôt que de laisser un appelant déréférencer un client docker nil.
func TestMarkDoneOnUnknownRunner(t *testing.T) {
	state := runnerState{
		idle: make(map[string]runnerInfo),
		busy: make(map[string]runnerInfo),
	}

	info, err := state.markDone("runner-inconnu")
	if err == nil {
		t.Fatal("markDone sur un runner inconnu devrait renvoyer une erreur")
	}
	if info.dockerClient != nil || info.containerID != "" {
		t.Fatalf("markDone devrait renvoyer une runnerInfo vide, obtenu %+v", info)
	}
}
