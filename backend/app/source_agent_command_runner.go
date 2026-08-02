package app

import (
	"context"
	"fmt"
)

type SourceAgentCommandClient interface {
	ClaimCommand(context.Context) (*SourceAgentCommand, error)
	ReportCommand(context.Context, string, string, string, string, string) (SourceAgentCommand, error)
}

type SourceAgentDiagnoser interface {
	Diagnose(context.Context) SourceAgentDiagnosticReport
}

type SourceAgentUpdater interface {
	Upgrade(context.Context, SourceAgentCommand) SourceAgentUpgradeResult
}

type SourceAgentDiagnosticReport struct {
	State   string `json:"state"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

type SourceAgentUpgradeResult struct {
	State         string `json:"state"`
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
	ActualVersion string `json:"actual_version,omitempty"`
}

type sourceAgentPendingCommandReport struct {
	state         string
	code          string
	message       string
	actualVersion string
}

func sourceAgentDiagnosticCommandReports(report SourceAgentDiagnosticReport) []sourceAgentPendingCommandReport {
	transition := SourceAgentCommandTransition{
		State: report.State, ResultCode: report.Code,
	}
	if report.State == SourceAgentCommandSucceeded || report.State == SourceAgentCommandFailed {
		if normalized, err := normalizeSourceAgentCommandTransition(transition); err == nil {
			if validateSourceAgentCommandTerminalResult(SourceAgentCommandDiagnose, normalized) == nil {
				normalized.Message = sourceAgentDiagnosticResultMessage(normalized.State)
				return []sourceAgentPendingCommandReport{sourceAgentPendingReport(normalized)}
			}
		}
	}
	return []sourceAgentPendingCommandReport{{
		state: SourceAgentCommandFailed, code: SourceAgentCommandCodeDiagnosticFailed,
		message: "Diagnostic result was invalid.",
	}}
}

func sourceAgentUpgradeCommandReport(command SourceAgentCommand, result SourceAgentUpgradeResult) sourceAgentPendingCommandReport {
	transition := SourceAgentCommandTransition{
		State: result.State, ResultCode: result.Code, ActualVersion: result.ActualVersion,
	}
	normalized, err := normalizeSourceAgentCommandTransition(transition)
	if err == nil && sourceAgentCommandTransitionAllowed(SourceAgentCommandUpgrade, command.State, normalized.State) &&
		validateSourceAgentCommandTerminalResult(SourceAgentCommandUpgrade, normalized) == nil {
		if isTerminalSourceAgentCommandState(normalized.State) {
			normalized.Message = sourceAgentUpgradeResultMessage(normalized.ResultCode)
		}
		return sourceAgentPendingReport(normalized)
	}
	return sourceAgentInvalidUpgradeReport(command.State)
}

func sourceAgentInvalidUpgradeReport(commandState string) sourceAgentPendingCommandReport {
	if commandState == SourceAgentCommandRestarting || commandState == SourceAgentCommandVerifying {
		return sourceAgentPendingCommandReport{state: SourceAgentCommandRollback}
	}
	if sourceAgentCommandTransitionAllowed(SourceAgentCommandUpgrade, commandState, SourceAgentCommandFailed) {
		return sourceAgentPendingCommandReport{
			state: SourceAgentCommandFailed, code: SourceAgentCommandCodeUpgradeFailed,
			message: "Upgrade result was invalid.",
		}
	}
	return sourceAgentPendingCommandReport{
		state: SourceAgentCommandFailed, code: SourceAgentCommandCodeUpgradeFailed,
		message: "Upgrade state was invalid.",
	}
}

func sourceAgentUpgradeUnavailableReport(commandState string) sourceAgentPendingCommandReport {
	if commandState == SourceAgentCommandRestarting || commandState == SourceAgentCommandVerifying {
		return sourceAgentPendingCommandReport{state: SourceAgentCommandRollback}
	}
	return sourceAgentPendingCommandReport{
		state: SourceAgentCommandFailed, code: SourceAgentCommandCodeUpgradeFailed,
		message: "Upgrade is unavailable.",
	}
}

func sourceAgentUpgradeReportForState(command SourceAgentCommand, updater SourceAgentUpdater, ctx context.Context) sourceAgentPendingCommandReport {
	if updater == nil {
		return sourceAgentUpgradeUnavailableReport(command.State)
	}
	return sourceAgentUpgradeCommandReport(command, updater.Upgrade(ctx, command))
}

func sourceAgentUpgradeResultMessage(code string) string {
	switch code {
	case SourceAgentCommandCodeUpgradeComplete:
		return "Upgrade completed."
	case SourceAgentCommandCodeDownloadFailed:
		return "Upgrade download failed."
	case SourceAgentCommandCodeVerificationFailed:
		return "Upgrade verification failed."
	case SourceAgentCommandCodeInstallFailed:
		return "Upgrade installation failed."
	case SourceAgentCommandCodeRestartFailed:
		return "Upgrade restart failed."
	case SourceAgentCommandCodeRollbackComplete:
		return "Upgrade rolled back."
	case SourceAgentCommandCodeRollbackFailed:
		return "Upgrade rollback failed."
	default:
		return "Upgrade failed."
	}
}

func sourceAgentDiagnosticResultMessage(state string) string {
	if state == SourceAgentCommandSucceeded {
		return "Diagnostics completed."
	}
	return "Diagnostics failed."
}

// Each upgrade cycle performs at most one local stage and reports exactly one
// durable transition. The next stage starts only after that report succeeds.
func (r *SourceAgentRunner) executeUpgradeCommand(ctx context.Context, command SourceAgentCommand) error {
	if !validSourceAgentUpgradeHandoff(command) {
		r.setPendingCommandReports([]sourceAgentPendingCommandReport{sourceAgentInvalidUpgradeReport(command.State)})
		return r.reportPendingCommand(ctx)
	}

	var report sourceAgentPendingCommandReport
	switch command.State {
	case SourceAgentCommandClaimed:
		report = sourceAgentPendingCommandReport{state: SourceAgentCommandDownloading}
	case SourceAgentCommandDownloading, SourceAgentCommandInstalling,
		SourceAgentCommandRestarting, SourceAgentCommandVerifying, SourceAgentCommandRollback:
		report = sourceAgentUpgradeReportForState(command, r.updater, ctx)
	case SourceAgentCommandVerified:
		report = sourceAgentPendingCommandReport{state: SourceAgentCommandInstalling}
	default:
		return fmt.Errorf("upgrade command state is not executable")
	}
	r.setPendingCommandReports([]sourceAgentPendingCommandReport{report})
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.reportPendingCommand(ctx)
}

func sourceAgentPendingReport(transition SourceAgentCommandTransition) sourceAgentPendingCommandReport {
	return sourceAgentPendingCommandReport{
		state: transition.State, code: transition.ResultCode,
		message: transition.Message, actualVersion: transition.ActualVersion,
	}
}

func (r *SourceAgentRunner) executeCurrentCommand(ctx context.Context, command SourceAgentCommand) error {
	switch command.Type {
	case SourceAgentCommandDiagnose:
		report := SourceAgentDiagnosticReport{
			State: SourceAgentCommandFailed, Code: SourceAgentCommandCodeDiagnosticFailed,
			Message: "Diagnostics are unavailable.",
		}
		if r.diagnoser != nil {
			report = r.diagnoser.Diagnose(ctx)
		}
		r.setPendingCommandReports(sourceAgentDiagnosticCommandReports(report))
	case SourceAgentCommandUpgrade:
		return r.executeUpgradeCommand(ctx, command)
	default:
		return fmt.Errorf("unsupported claimed source-agent command")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.reportPendingCommand(ctx)
}

func validSourceAgentUpgradeHandoff(command SourceAgentCommand) bool {
	if command.UpgradeSpec == nil {
		return false
	}
	artifactID, err := normalizeSourceAgentCommandIdentifier("artifact_id", command.UpgradeSpec.ArtifactID, true)
	if err != nil || artifactID != command.UpgradeSpec.ArtifactID {
		return false
	}
	expectedVersion, err := normalizeSourceAgentVersion("expected_current_version", command.UpgradeSpec.ExpectedCurrentVersion)
	if err != nil || expectedVersion == "" || expectedVersion != command.UpgradeSpec.ExpectedCurrentVersion {
		return false
	}
	return command.ExpectedCurrentVersion == "" || command.ExpectedCurrentVersion == expectedVersion
}

func (r *SourceAgentRunner) reportPendingCommand(ctx context.Context) error {
	command, report, ok := r.nextPendingCommandReport()
	if !ok {
		return nil
	}
	updated, err := r.commandClient.ReportCommand(
		ctx, command.ID, report.state, report.code, report.message, report.actualVersion,
	)
	if err != nil {
		return fmt.Errorf("report source-agent command: %w", err)
	}
	r.commandReportSucceeded(updated)
	return nil
}
