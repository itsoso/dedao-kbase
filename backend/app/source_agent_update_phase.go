package app

import "errors"

type sourceAgentUpdatePhaseEvidence struct {
	HandoffPublished       bool
	UpdaterRequested       bool
	JournalStage           string
	Outcome                *SourceAgentUpdateResult
	RuntimeIdentityMatches bool
	ReadyAfterHeartbeat    bool
	RollbackRequested      bool
	PermanentFailureCode   string
}

type sourceAgentUpdatePhaseDecision struct {
	Waiting bool
	Report  SourceAgentUpgradeResult
}

func mapSourceAgentUpdatePhase(
	serverState string,
	evidence sourceAgentUpdatePhaseEvidence,
) (sourceAgentUpdatePhaseDecision, error) {
	if !validSourceAgentUpdatePhaseJournalStage(evidence.JournalStage) ||
		!validSourceAgentUpdatePermanentFailure(evidence.PermanentFailureCode) {
		return sourceAgentUpdatePhaseDecision{}, errors.New("source agent update evidence is invalid")
	}
	outcome, err := sourceAgentUpdatePhaseOutcome(evidence.Outcome)
	if err != nil || (evidence.RollbackRequested && outcome == sourceAgentUpdatePhaseOutcomeSucceeded) {
		return sourceAgentUpdatePhaseDecision{}, errors.New("source agent update evidence conflicts")
	}

	switch serverState {
	case SourceAgentCommandClaimed:
		return sourceAgentUpdatePhaseReport(SourceAgentCommandDownloading, "", ""), nil
	case SourceAgentCommandDownloading:
		if evidence.PermanentFailureCode != "" {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandFailed, evidence.PermanentFailureCode, ""), nil
		}
		if evidence.HandoffPublished {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandVerified, "", ""), nil
		}
		return sourceAgentUpdatePhaseWait(), nil
	case SourceAgentCommandVerified:
		if evidence.PermanentFailureCode != "" {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandFailed, evidence.PermanentFailureCode, ""), nil
		}
		if !evidence.HandoffPublished {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandFailed, SourceAgentCommandCodeVerificationFailed, ""), nil
		}
		return sourceAgentUpdatePhaseReport(SourceAgentCommandInstalling, "", ""), nil
	case SourceAgentCommandInstalling:
		if sourceAgentUpdatePhaseNeedsRollback(evidence, outcome) {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandRollback, "", ""), nil
		}
		if outcome == sourceAgentUpdatePhaseOutcomeFailed {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandFailed, evidence.Outcome.Code, ""), nil
		}
		if outcome == sourceAgentUpdatePhaseOutcomeSucceeded ||
			sourceAgentUpdateJournalStageIs(evidence.JournalStage, "restart_requested", "restarted", "ready") {
			if !evidence.UpdaterRequested {
				return sourceAgentUpdatePhaseDecision{}, errors.New("source agent updater request evidence is missing")
			}
			return sourceAgentUpdatePhaseReport(SourceAgentCommandRestarting, "", ""), nil
		}
		return sourceAgentUpdatePhaseWait(), nil
	case SourceAgentCommandRestarting:
		if sourceAgentUpdatePhaseNeedsRollback(evidence, outcome) {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandRollback, "", ""), nil
		}
		if outcome == sourceAgentUpdatePhaseOutcomeSucceeded {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandVerifying, "", ""), nil
		}
		return sourceAgentUpdatePhaseWait(), nil
	case SourceAgentCommandVerifying:
		if sourceAgentUpdatePhaseNeedsRollback(evidence, outcome) {
			return sourceAgentUpdatePhaseReport(SourceAgentCommandRollback, "", ""), nil
		}
		if outcome == sourceAgentUpdatePhaseOutcomeSucceeded &&
			evidence.RuntimeIdentityMatches && evidence.ReadyAfterHeartbeat {
			return sourceAgentUpdatePhaseReport(
				SourceAgentCommandSucceeded,
				SourceAgentCommandCodeUpgradeComplete,
				evidence.Outcome.RuntimeVersion,
			), nil
		}
		return sourceAgentUpdatePhaseWait(), nil
	case SourceAgentCommandRollback:
		switch outcome {
		case sourceAgentUpdatePhaseOutcomeRestored:
			return sourceAgentUpdatePhaseReport(SourceAgentCommandRolledBack, SourceAgentCommandCodeRollbackComplete, ""), nil
		case sourceAgentUpdatePhaseOutcomeRollbackFailed:
			return sourceAgentUpdatePhaseReport(SourceAgentCommandFailed, SourceAgentCommandCodeRollbackFailed, ""), nil
		default:
			return sourceAgentUpdatePhaseWait(), nil
		}
	default:
		return sourceAgentUpdatePhaseDecision{}, errors.New("source agent update server state is invalid")
	}
}

const (
	sourceAgentUpdatePhaseOutcomeNone = iota
	sourceAgentUpdatePhaseOutcomeSucceeded
	sourceAgentUpdatePhaseOutcomeFailed
	sourceAgentUpdatePhaseOutcomeRestored
	sourceAgentUpdatePhaseOutcomeRollbackFailed
)

func sourceAgentUpdatePhaseOutcome(result *SourceAgentUpdateResult) (int, error) {
	if result == nil {
		return sourceAgentUpdatePhaseOutcomeNone, nil
	}
	switch {
	case result.Outcome == SourceAgentUpdateOutcomeSucceeded &&
		result.Code == SourceAgentCommandCodeUpgradeComplete &&
		!result.BinaryRestored && isSourceAgentArtifactVersion(result.RuntimeVersion):
		return sourceAgentUpdatePhaseOutcomeSucceeded, nil
	case result.Outcome == SourceAgentUpdateOutcomeRolledBack &&
		result.Code == SourceAgentCommandCodeRollbackComplete && result.BinaryRestored:
		return sourceAgentUpdatePhaseOutcomeRestored, nil
	case result.Outcome == SourceAgentUpdateOutcomeFailed &&
		result.Code == SourceAgentCommandCodeRollbackFailed:
		return sourceAgentUpdatePhaseOutcomeRollbackFailed, nil
	case result.Outcome == SourceAgentUpdateOutcomeFailed &&
		(result.Code == SourceAgentCommandCodeVerificationFailed ||
			result.Code == SourceAgentCommandCodeInstallFailed ||
			result.Code == SourceAgentCommandCodeRestartFailed ||
			result.Code == SourceAgentUpdateCodeCanceled) && !result.BinaryRestored:
		return sourceAgentUpdatePhaseOutcomeFailed, nil
	default:
		return sourceAgentUpdatePhaseOutcomeNone, errors.New("source agent update outcome is invalid")
	}
}

func sourceAgentUpdatePhaseNeedsRollback(evidence sourceAgentUpdatePhaseEvidence, outcome int) bool {
	if evidence.RollbackRequested || evidence.JournalStage == "rollback_restored" ||
		outcome == sourceAgentUpdatePhaseOutcomeRestored || outcome == sourceAgentUpdatePhaseOutcomeRollbackFailed {
		return true
	}
	return sourceAgentUpdateJournalStageIs(
		evidence.JournalStage,
		"replaced", "restart_requested", "restarted", "ready", "terminal_cleanup",
	) && outcome == sourceAgentUpdatePhaseOutcomeFailed
}

func validSourceAgentUpdatePhaseJournalStage(stage string) bool {
	switch stage {
	case "", "started", "backup_durable", "replaced", "restart_requested", "restarted", "ready", "rollback_restored", "terminal_cleanup":
		return true
	default:
		return false
	}
}

func validSourceAgentUpdatePermanentFailure(code string) bool {
	return code == "" || code == SourceAgentCommandCodeDownloadFailed || code == SourceAgentCommandCodeVerificationFailed
}

func sourceAgentUpdatePhaseWait() sourceAgentUpdatePhaseDecision {
	return sourceAgentUpdatePhaseDecision{Waiting: true}
}

func sourceAgentUpdatePhaseReport(state, code, actualVersion string) sourceAgentUpdatePhaseDecision {
	return sourceAgentUpdatePhaseDecision{Report: SourceAgentUpgradeResult{
		State: state, Code: code, ActualVersion: actualVersion,
	}}
}
