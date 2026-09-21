package scaler

import (
	"fmt"
	"sync"
)

type runnerInfo struct {
	containerID string
	// ID du runner tel qu'enregistré côté github par la config JIT. Sert à le
	// désenregistrer quand son conteneur disparaît sans avoir exécuté de job.
	runnerID     int
	dockerClient *DockerClientWithMetadata
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

func (runnerState *runnerState) addIdle(name string, info runnerInfo) {
	runnerState.mu.Lock()
	runnerState.idle[name] = info
	runnerState.mu.Unlock()
}
