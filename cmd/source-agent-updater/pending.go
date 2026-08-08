package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
	"github.com/yann0917/dedao-gui/internal/sourceagentupdate"
)

const sourceAgentUpdaterConfigBasename = ".source-agent-updater-config.json"
const sourceAgentUpdaterMaintenanceBasename = ".managed-worker-maintenance"

var (
	sourceAgentUpdaterExecutable      = os.Executable
	sourceAgentUpdaterConfigLoader    = sourceagentupdate.LoadConfig
	sourceAgentUpdaterConfigSaver     = sourceagentupdate.SaveConfig
	sourceAgentUpdaterGetenv          = os.Getenv
	sourceAgentUpdaterTokenLoader     = sourceagentsecret.LoadTransportToken
	sourceAgentUpdaterPendingExecutor = app.RunSourceAgentPendingUpdate
)

func sourceAgentProtectedConfigPath() (string, error) {
	executable, err := sourceAgentUpdaterExecutable()
	if err != nil || !filepath.IsAbs(executable) {
		return "", errSourceAgentPendingExecutionUnavailable
	}
	executable, err = filepath.EvalSymlinks(filepath.Clean(executable))
	if err != nil || filepath.Base(executable) != "source-agent-updater" {
		return "", errSourceAgentPendingExecutionUnavailable
	}
	return filepath.Join(filepath.Dir(executable), sourceAgentUpdaterConfigBasename), nil
}

func installSourceAgentProtectedConfig() error {
	configPath, err := sourceAgentProtectedConfigPath()
	if err != nil {
		return err
	}
	config := sourceagentupdate.Config{
		Schema:   sourceagentupdate.ConfigSchemaV1,
		KBaseURL: sourceAgentUpdaterGetenv("KBASE_REMOTE_URL"),
		AgentID:  sourceAgentUpdaterGetenv("KBASE_SOURCE_AGENT_ID"),
	}
	if err := sourceAgentUpdaterConfigSaver(configPath, config); err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	return checkSourceAgentProtectedConfig()
}

func checkSourceAgentProtectedConfig() error {
	configPath, err := sourceAgentProtectedConfigPath()
	if err != nil {
		return err
	}
	if _, err := sourceAgentUpdaterConfigLoader(configPath); err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	return nil
}

func checkSourceAgentUninstallSafe() error {
	configPath, err := sourceAgentProtectedConfigPath()
	if err != nil {
		return err
	}
	root := filepath.Dir(configPath)
	for _, directory := range []string{".source-agent-staging", ".source-agent-handoff"} {
		entries, readErr := os.ReadDir(filepath.Join(root, directory))
		if readErr != nil || len(entries) != 0 {
			return errSourceAgentPendingExecutionUnavailable
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".source-agent-backup" || name == ".source-agent-backup.pending" ||
			strings.HasPrefix(name, ".source-agent-prepared-") {
			return errSourceAgentPendingExecutionUnavailable
		}
	}
	return nil
}

type sourceAgentPendingProcessAdapter struct {
	control sourceAgentProcessControl
}

func (a sourceAgentPendingProcessAdapter) Restart(ctx context.Context) error {
	if a.control == nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	return a.control.RestartWorker(ctx)
}

func runSourceAgentPendingFromProtectedState(
	ctx context.Context,
	workerType string,
	control sourceAgentProcessControl,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	configPath, err := sourceAgentProtectedConfigPath()
	if err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	maintenancePath := filepath.Join(filepath.Dir(configPath), sourceAgentUpdaterMaintenanceBasename)
	if _, err := os.Lstat(maintenancePath); err == nil || !os.IsNotExist(err) {
		return errSourceAgentPendingExecutionUnavailable
	}
	executable := filepath.Join(filepath.Dir(configPath), "source-agent-updater")
	config, err := sourceAgentUpdaterConfigLoader(configPath)
	if err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	token, err := sourceAgentUpdaterTokenLoader(ctx)
	if err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	receiptRoot := filepath.Join(filepath.Dir(executable), ".source-agent-handoff")
	client, err := app.NewSourceAgentClient(app.SourceAgentConfig{
		RemoteURL:  config.KBaseURL,
		AgentToken: token,
		AgentID:    config.AgentID,
		StateDir:   receiptRoot,
	})
	if err != nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	if control == nil || sourceAgentUpdaterPendingExecutor == nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	if err := sourceAgentUpdaterPendingExecutor(ctx, app.SourceAgentPendingUpdateRunnerConfig{
		UpdaterExecutable: executable,
		WorkerType:        workerType,
		Guard:             client,
		ProcessControl:    sourceAgentPendingProcessAdapter{control: control},
	}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errSourceAgentPendingExecutionUnavailable
	}
	return nil
}
