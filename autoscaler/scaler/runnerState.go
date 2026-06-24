package scaler

import (
	"fmt"
	"sync"
)

type runnerInfo struct {
	containerID  string
	dockerClient *DockerClientWithMetadata
}

type runnerState struct {
	mu   sync.Mutex
	idle map[string]runnerInfo
	busy map[string]runnerInfo
}

func (runnerState *runnerState) count() int {
	runnerState.mu.Lock()
	count := len(runnerState.idle) + len(runnerState.busy)
	runnerState.mu.Unlock()
	return count
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
