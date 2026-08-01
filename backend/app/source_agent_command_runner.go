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

func sourceAgentUpgradeCommandReports(result SourceAgentUpgradeResult) []sourceAgentPendingCommandReport {
	transition := SourceAgentCommandTransition{
		State: result.State, ResultCode: result.Code, ActualVersion: result.ActualVersion,
	}
	normalized, err := normalizeSourceAgentCommandTransition(transition)
	terminalState := result.State == SourceAgentCommandSucceeded || result.State == SourceAgentCommandFailed || result.State == SourceAgentCommandRolledBack
	if !terminalState || err != nil || validateSourceAgentCommandTerminalResult(SourceAgentCommandUpgrade, normalized) != nil {
		normalized = SourceAgentCommandTransition{
			State: SourceAgentCommandFailed, ResultCode: SourceAgentCommandCodeUpgradeFailed,
			Message: "Upgrade result was invalid.",
		}
	} else {
		normalized.Message = sourceAgentUpgradeResultMessage(normalized.ResultCode)
	}

	switch normalized.State {
	case SourceAgentCommandSucceeded:
		return append(sourceAgentUpgradeProgressReports(
			SourceAgentCommandVerified,
			SourceAgentCommandInstalling,
			SourceAgentCommandRestarting,
			SourceAgentCommandVerifying,
		), sourceAgentPendingReport(normalized))
	case SourceAgentCommandRolledBack:
		return append(sourceAgentUpgradeProgressReports(
			SourceAgentCommandVerified,
			SourceAgentCommandInstalling,
			SourceAgentCommandRollback,
		), sourceAgentPendingReport(normalized))
	default:
		return []sourceAgentPendingCommandReport{sourceAgentPendingReport(normalized)}
	}
}

func sourceAgentDiagnosticResultMessage(state string) string {
	if state == SourceAgentCommandSucceeded {
		return "Diagnostics completed."
	}
	return "Diagnostics failed."
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

func sourceAgentUpgradeProgressReports(states ...string) []sourceAgentPendingCommandReport {
	reports := make([]sourceAgentPendingCommandReport, len(states))
	for index, state := range states {
		reports[index] = sourceAgentPendingCommandReport{state: state}
	}
	return reports
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
		if r.updater == nil || !validSourceAgentUpgradeHandoff(command) {
			r.setPendingCommandReports([]sourceAgentPendingCommandReport{{
				state: SourceAgentCommandFailed, code: SourceAgentCommandCodeUpgradeFailed,
				message: "Upgrade is unavailable.",
			}})
			return r.reportPendingCommand(ctx)
		}
		if command.State == SourceAgentCommandClaimed {
			r.setPendingCommandReports([]sourceAgentPendingCommandReport{{state: SourceAgentCommandDownloading}})
			return r.reportPendingCommand(ctx)
		}
		if command.State != SourceAgentCommandDownloading {
			return fmt.Errorf("upgrade command is not ready for updater handoff")
		}
		result := r.updater.Upgrade(ctx, command)
		r.setPendingCommandReports(sourceAgentUpgradeCommandReports(result))
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
	for {
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
	}
}
