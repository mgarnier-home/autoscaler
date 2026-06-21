package scaler

import "sync"

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

func (runnerState *runnerState) markBusy(name string) {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	state, ok := runnerState.idle[name]
	if !ok {
		panic("marking non-existent runner busy")
	}
	delete(runnerState.idle, name)
	runnerState.busy[name] = state
}

func (runnerState *runnerState) markDone(name string) runnerInfo {
	runnerState.mu.Lock()
	defer runnerState.mu.Unlock()
	return runnerState.markDoneUnlocked(name)
}

func (runnerState *runnerState) markDoneUnlocked(name string) runnerInfo {
	info, ok := runnerState.busy[name]
	if ok {
		delete(runnerState.busy, name)
		return info
	}
	info, ok = runnerState.idle[name]
	if ok {
		delete(runnerState.idle, name)
		return info
	}
	panic("marking non-existent runner done")
}

func (runnerState *runnerState) addIdle(name string, info runnerInfo) {
	runnerState.mu.Lock()
	runnerState.idle[name] = info
	runnerState.mu.Unlock()
}
