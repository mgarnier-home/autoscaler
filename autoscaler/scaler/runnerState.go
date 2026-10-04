package scaler

import (
	"fmt"
	"sync"

	"mgarnier11.fr/docker-autoscaler/docker"
)

type runnerInfo struct {
	containerID  string
	dockerClient *docker.Client

	// runnerID est l'identifiant du runner côté github, relevé sur la config
	// JIT au démarrage du conteneur. Il évite une recherche par nom au moment
	// de désenregistrer le runner.
	runnerID int64
}

type runnerState struct {
	mu   sync.Mutex
	idle map[string]runnerInfo
	busy map[string]runnerInfo
}

func (runnerState *runnerState) count() int {
	idle, busy := runnerState.counts()
	return idle + busy
}

// counts renvoie le détail du pool : conteneurs libres et conteneurs en train
// d'exécuter un job. Le calcul du nombre de runners souhaité a besoin des deux
// séparément, et pas seulement du total.
func (runnerState *runnerState) counts() (idle int, busy int) {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	return len(runnerState.idle), len(runnerState.busy)
}

func (runnerState *runnerState) markBusy(name string) error {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	state, ok := runnerState.idle[name]
	if !ok {
		return fmt.Errorf("marking non-existent runner busy: %s", name)
	}
	delete(runnerState.idle, name)
	runnerState.busy[name] = state
	return nil
}

func (runnerState *runnerState) markDone(name string) (runnerInfo, error) {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	return runnerState.markDoneUnlocked(name)
}

func (runnerState *runnerState) markDoneUnlocked(name string) (runnerInfo, error) {
	info, ok := runnerState.busy[name]
	if ok {
		delete(runnerState.busy, name)
		return info, nil
	}
	info, ok = runnerState.idle[name]
	if ok {
		delete(runnerState.idle, name)
		return info, nil
	}
	return runnerInfo{}, fmt.Errorf("runner %s not found in busy or idle state", name)
}

// drainIdle retire et renvoie d'un coup tous les runners libres.
//
// L'arrêt doit les traiter sans tenir le verrou : chaque runner coûte un appel
// github puis un appel docker, et aucune I/O ne doit se faire sous mutex. Rendre
// la map en une opération atomique évite d'avoir à s'en souvenir.
func (runnerState *runnerState) drainIdle() map[string]runnerInfo {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	idle := runnerState.idle
	runnerState.idle = make(map[string]runnerInfo)
	return idle
}

// clear vide l'état sans toucher aux conteneurs. Les runners occupés survivent à
// l'autoscaler : ils ont leur config JIT et poussent leurs logs directement sur
// github, donc ils terminent leur job seuls.
func (runnerState *runnerState) clear() {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	runnerState.idle = make(map[string]runnerInfo)
	runnerState.busy = make(map[string]runnerInfo)
}

func (runnerState *runnerState) addIdle(name string, info runnerInfo) {
	runnerState.mu.Lock()
	runnerState.idle[name] = info
	runnerState.mu.Unlock()
}
