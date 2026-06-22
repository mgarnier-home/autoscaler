package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-github/v79/github"
	"github.com/stretchr/testify/require"
)

func createTempConfigFile(dir string) (string, error) {
	tempFile, err := os.CreateTemp(dir, "config-e2e.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp config file: %w", err)
	}

	tempFileContent := `
- scaleSetName: "scaleset-e2e-1"
  maxRunners: 10
  minRunners: 0
  labels:
    - "scaleset-e2e-1"
  runnerGroup: "default"
  dockerHosts:
    - name: "local"
      runtime: "sysbox-runc"
      url: "unix:///var/run/docker.sock"
- scaleSetName: "scaleset-e2e-2"
  maxRunners: 10
  minRunners: 0
  labels:
    - "scaleset-e2e-2"
  runnerGroup: "default"
  dockerHosts:
    - name: "local"
      runtime: "sysbox-runc"
      url: "unix:///var/run/docker.sock"
`

	_, err = tempFile.WriteString(tempFileContent)
	if err != nil {
		return "", fmt.Errorf("failed to write to temp config file: %w", err)
	}

	err = tempFile.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close temp config file: %w", err)
	}

	return tempFile.Name(), nil
}

type runCmdMonitor struct {
	cmd     *exec.Cmd
	readyCh chan struct{}
	errCh   chan error
	waitCh  chan error
	once    sync.Once
}

func newRunCmdMonitor(ctx context.Context, cmd *exec.Cmd, stdout, stderr io.ReadCloser, readyText string, readyCount int) *runCmdMonitor {
	monitor := &runCmdMonitor{
		cmd:     cmd,
		readyCh: make(chan struct{}),
		errCh:   make(chan error, 1),
		waitCh:  make(chan error, 1),
	}

	go func() {
		monitor.waitCh <- cmd.Wait()
	}()

	go monitor.watchStdout(ctx, stdout, readyText, readyCount)
	go monitor.watchStderr(stderr)

	return monitor
}

func (m *runCmdMonitor) watchStdout(ctx context.Context, stdout io.Reader, readyText string, readyCount int) {
	started := 0
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), readyText) {
			started++
			if started >= readyCount {
				close(m.readyCh)
				return
			}
		}
		select {
		case <-ctx.Done():
			m.fail(ctx.Err())
			return
		default:
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		m.fail(fmt.Errorf("failed to read stdout: %w", scanErr))
		return
	}

	m.fail(fmt.Errorf("process exited before seeing %d %q log lines", readyCount, readyText))
}

func (m *runCmdMonitor) watchStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			m.fail(fmt.Errorf("stderr output detected: %s", line))
			return
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		m.fail(fmt.Errorf("failed to read stderr: %w", scanErr))
	}
}

func (m *runCmdMonitor) fail(err error) {
	m.once.Do(func() {
		m.interrupt()
		select {
		case m.errCh <- err:
		default:
		}
	})
}

func (m *runCmdMonitor) interrupt() {
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(os.Interrupt)
	}
}

func (m *runCmdMonitor) waitForReady(ctx context.Context) error {
	select {
	case <-m.readyCh:
		return nil
	case err := <-m.errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *runCmdMonitor) wait() error {
	return <-m.waitCh
}

func TestE2E(t *testing.T) {
	if os.Getenv("E2E") != "true" {
		t.Skip("Skipping E2E test; set E2E=true to run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tempDir, err := os.MkdirTemp("", "e2e-dockerscaleset")
	require.NoError(t, err, "Failed to create temp dir")
	defer os.RemoveAll(tempDir)

	configFilePath, err := createTempConfigFile(tempDir)
	require.NoError(t, err, "Failed to create temp config file")
	defer os.Remove(configFilePath)

	registrationUrl := mustGetEnv(t, "E2E_REGISTRATION_URL")
	githubToken := mustGetEnv(t, "E2E_GITHUB_TOKEN")
	dockerRegistryUrl := mustGetEnv(t, "E2E_DOCKER_REGISTRY_URL")
	dockerRegistryUsername := mustGetEnv(t, "E2E_DOCKER_REGISTRY_USERNAME")
	dockerRegistryPassword := mustGetEnv(t, "E2E_DOCKER_REGISTRY_PASSWORD")
	artifactoryToken := mustGetEnv(t, "E2E_ARTIFACTORY_TOKEN")
	runnerImage := mustGetEnv(t, "E2E_RUNNER_IMAGE")

	binaryPath := filepath.Join(tempDir, "dockerscaleset")

	// Build the dockerscaleset binary in temp dir
	{
		cmd := exec.Command("go", "build", "-o", binaryPath, ".")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "Failed to build dockerscaleset: %s", output)
	}

	runCmd := exec.Command(binaryPath)
	runCmd.Env = []string{
		"CONFIG_FILE_PATH=" + configFilePath,
		"REGISTRATION_URL=" + registrationUrl,
		"GITHUB_TOKEN=" + githubToken,
		"DOCKER_REGISTRY_URL=" + dockerRegistryUrl,
		"DOCKER_REGISTRY_USERNAME=" + dockerRegistryUsername,
		"DOCKER_REGISTRY_PASSWORD=" + dockerRegistryPassword,
		"ARTIFACTORY_TOKEN=" + artifactoryToken,
		"RUNNER_IMAGE=" + runnerImage,
	}
	stdout, err := runCmd.StdoutPipe()
	require.NoError(t, err, "Failed to get stdout pipe")
	stderr, err := runCmd.StderrPipe()
	require.NoError(t, err, "Failed to get stderr pipe")
	err = runCmd.Start()
	require.NoError(t, err, "Failed to start dockerscaleset")

	monitor := newRunCmdMonitor(ctx, runCmd, stdout, stderr, "Starting listener for runner scale set", 2)
	t.Cleanup(func() {
		monitor.interrupt()
		require.NoError(t, monitor.wait())
	})

	require.NoError(t, monitor.waitForReady(ctx), "dockerscaleset did not start both listeners")

	// env := mustE2EWorkflowEnv(t, "scaleset-e2e-1")
	// runResult, err := env.runWorkflowOnNewRunner(t, ctx, 15*time.Minute)
	// require.NoError(t, err, "workflow run failed")
	// require.Equal(t, "success", runResult.Conclusion, "workflow run did not succeed")

}

type e2eWorkflowEnv struct {
	targetOrg  string
	targetRepo string
	targetFile string

	scalesetName string
	client       *github.Client
}

type WorkflowRun struct {
	ID         int    `json:"id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	CreatedAt  string `json:"created_at"`
}

func (env *e2eWorkflowEnv) triggerWorkflowDispatch(t *testing.T, ctx context.Context) (int, error) {
	dispatchTime := time.Now().UTC()

	resp, err := env.client.Actions.CreateWorkflowDispatchEventByFileName(
		ctx,
		env.targetOrg,
		env.targetRepo,
		env.targetFile,
		github.CreateWorkflowDispatchEventRequest{
			Ref: "main",
			Inputs: map[string]any{
				"scaleset_name": env.scalesetName,
			},
		},
	)
	require.NoError(t, err, "Failed to create workflow dispatch")
	require.Equal(t, 204, resp.StatusCode, "Unexpected status code from workflow dispatch")

	// Wait a bit for the run to be created
	time.Sleep(10 * time.Second)

	// List runs with event=workflow_dispatch and since=dispatchTime
	opts := &github.ListWorkflowRunsOptions{
		Event:   "workflow_dispatch",
		Created: ">=" + dispatchTime.Format(time.RFC3339),
		ListOptions: github.ListOptions{
			PerPage: 10,
		},
	}
	runs, _, err := env.client.Actions.ListWorkflowRunsByFileName(
		ctx,
		env.targetOrg,
		env.targetRepo,
		env.targetFile,
		opts,
	)
	require.NoError(t, err, "Failed to list workflow runs")
	require.Greater(t, len(runs.WorkflowRuns), 0, "No workflow runs found after dispatch")

	// Sort by created_at desc, take the first (most recent)
	var latestRun *github.WorkflowRun
	var latestTime time.Time
	for _, run := range runs.WorkflowRuns {
		createdAt := run.CreatedAt.Time
		if createdAt.After(latestTime) {
			latestTime = createdAt
			latestRun = run
		}
	}

	if latestRun == nil {
		return 0, fmt.Errorf("no workflow runs found after dispatch")
	}

	return int(latestRun.GetID()), nil
}

func (env *e2eWorkflowEnv) waitForWorkflowCompletion(t *testing.T, ctx context.Context, runID int, timeout time.Duration) (*WorkflowRun, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			run, _, err := env.client.Actions.GetWorkflowRunByID(ctx, env.targetOrg, env.targetRepo, int64(runID))
			require.NoError(t, err, "Failed to get workflow run by ID")

			if run.GetStatus() == "completed" {
				return &WorkflowRun{
					ID:         int(run.GetID()),
					Status:     run.GetStatus(),
					Conclusion: run.GetConclusion(),
					CreatedAt:  run.GetCreatedAt().Format(time.RFC3339),
				}, nil
			}
		}
	}
}

func mustGetEnv(t *testing.T, key string) string {
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("Environment variable %s not set", key)
	}
	return value
}
