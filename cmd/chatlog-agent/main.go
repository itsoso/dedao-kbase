package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
)

const (
	chatlogAgentWorkerType      = "chatlog-worker"
	chatlogAgentVersion         = "0.1.0"
	chatlogAgentProtocolVersion = "2026-08-01"
	chatlogAgentCapability      = "chatlog_read"
)

var chatlogAgentRevision = "0000000000000000000000000000000000000000"
var chatlogTransportTokenLoader = sourceagentsecret.LoadTransportToken
var chatlogAgentUpgradeFactory = newChatlogProductionUpgradeRuntime

type chatlogEnvironmentLookup func(string) (string, bool)

type chatlogAgentConfig struct {
	Source     app.SourceAgentConfig
	ChatlogURL string
}

type chatlogAgentRuntime struct {
	sourceClient   *app.SourceAgentClient
	researchClient *app.ResearchWorkerClient
	reader         *app.ChatlogHTTPReader
	control        chatlogControlRunner
	outbox         *app.SourceAgentOutbox
	upgrade        interface{ Close() error }
	agentID        string
}

type chatlogControlRunner interface {
	RunOnce(context.Context) (app.SourceAgentCycleResult, error)
	ControlActive() bool
}

type chatlogWorkerUpgradeRuntime struct {
	updater app.SourceAgentUpdater
	state   app.SourceAgentProtectedUpgradeState
	closer  interface{ Close() error }
}

type chatlogAgentCycleResult struct {
	OK            bool   `json:"ok"`
	Heartbeat     bool   `json:"heartbeat"`
	JobID         string `json:"job_id,omitempty"`
	JobState      string `json:"job_state,omitempty"`
	EvidenceCount int    `json:"evidence_count,omitempty"`
	Code          string `json:"code,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runChatlogAgentCLI(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runChatlogAgentCLI(ctx context.Context, args []string, lookup chatlogEnvironmentLookup, stdout, stderr io.Writer) (returnErr error) {
	if len(args) != 1 {
		return fmt.Errorf("usage: chatlog-agent build-info|check-config|doctor|once|run")
	}
	switch args[0] {
	case "build-info":
		return writeChatlogAgentBuildInfo(stdout)
	case "check-config":
		_, err := loadChatlogAgentConfigOnly(lookup)
		return err
	case "doctor", "once", "run":
	default:
		return fmt.Errorf("usage: chatlog-agent build-info|check-config|doctor|once|run")
	}
	config, err := loadChatlogAgentConfig(ctx, lookup)
	if err != nil {
		return err
	}
	if args[0] == "doctor" {
		runtime, err := newChatlogAgentDoctorRuntime(config)
		if err != nil {
			return err
		}
		return runtime.doctor(ctx, stdout)
	}
	runtime, err := newChatlogAgentRuntime(config)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, runtime.close()) }()
	if args[0] == "once" {
		result, err := runtime.once(ctx)
		if err != nil {
			return err
		}
		return writeChatlogAgentJSON(stdout, result)
	}
	interval := chatlogAgentPollInterval(lookup)
	for {
		result, runErr := runtime.once(ctx)
		if runErr != nil {
			fmt.Fprintln(stderr, "chatlog-agent cycle failed")
		} else if err := writeChatlogAgentJSON(stdout, result); err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func newChatlogAgentDoctorRuntime(config chatlogAgentConfig) (*chatlogAgentRuntime, error) {
	sourceClient, err := app.NewSourceAgentClient(config.Source)
	if err != nil {
		return nil, err
	}
	reader, err := app.NewChatlogHTTPReader(app.ChatlogHTTPConfig{BaseURL: config.ChatlogURL})
	if err != nil {
		return nil, err
	}
	return &chatlogAgentRuntime{sourceClient: sourceClient, reader: reader, agentID: config.Source.AgentID}, nil
}

func writeChatlogAgentBuildInfo(output io.Writer) error {
	if output == nil || !validChatlogAgentRevision(chatlogAgentRevision) {
		return fmt.Errorf("chatlog agent build identity is invalid")
	}
	return writeChatlogAgentJSON(output, map[string]string{
		"worker_type": chatlogAgentWorkerType, "version": chatlogAgentVersion,
		"protocol_version": chatlogAgentProtocolVersion, "platform": runtime.GOOS,
		"architecture": runtime.GOARCH, "revision": chatlogAgentRevision,
	})
}

func validChatlogAgentRevision(value string) bool {
	if (len(value) != 40 && len(value) != 64) || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func loadChatlogAgentConfig(ctx context.Context, lookup chatlogEnvironmentLookup) (chatlogAgentConfig, error) {
	config, err := loadChatlogAgentConfigOnly(lookup)
	if err != nil {
		return chatlogAgentConfig{}, err
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	rawToken, provided := lookup("KBASE_SOURCE_AGENT_TOKEN")
	token, err := sourceagentsecret.ResolveTransportToken(ctx, rawToken, provided, chatlogTransportTokenLoader)
	if err != nil {
		return chatlogAgentConfig{}, err
	}
	config.Source.AgentToken = token
	return config, nil
}

func loadChatlogAgentConfigOnly(lookup chatlogEnvironmentLookup) (chatlogAgentConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	stateDir := chatlogLookupValue(lookup, "CHATLOG_AGENT_STATE_DIR")
	if stateDir == "" {
		return chatlogAgentConfig{}, fmt.Errorf("CHATLOG_AGENT_STATE_DIR is required")
	}
	config := chatlogAgentConfig{
		Source: app.SourceAgentConfig{
			RemoteURL: chatlogLookupValue(lookup, "KBASE_REMOTE_URL"), AgentToken: "pending-transport-token",
			AgentID: chatlogLookupValue(lookup, "KBASE_SOURCE_AGENT_ID"), StateDir: stateDir,
		},
		ChatlogURL: chatlogLookupValue(lookup, "CHATLOG_BASE_URL"),
	}
	if config.ChatlogURL == "" {
		config.ChatlogURL = "http://127.0.0.1:5030"
	}
	normalized, err := config.Source.Normalized()
	if err != nil {
		return chatlogAgentConfig{}, err
	}
	config.Source = normalized
	if _, err := app.NewChatlogHTTPReader(app.ChatlogHTTPConfig{BaseURL: config.ChatlogURL}); err != nil {
		return chatlogAgentConfig{}, err
	}
	return config, nil
}

func newChatlogAgentRuntime(config chatlogAgentConfig) (*chatlogAgentRuntime, error) {
	sourceClient, err := app.NewSourceAgentClient(config.Source)
	if err != nil {
		return nil, err
	}
	researchClient, err := app.NewResearchWorkerClient(app.ResearchWorkerClientConfig{
		RemoteURL: config.Source.RemoteURL, Token: config.Source.AgentToken, AgentID: config.Source.AgentID,
	})
	if err != nil {
		return nil, err
	}
	reader, err := app.NewChatlogHTTPReader(app.ChatlogHTTPConfig{BaseURL: config.ChatlogURL})
	if err != nil {
		return nil, err
	}
	outbox, err := app.NewSourceAgentOutbox(config.Source.StateDir)
	if err != nil {
		return nil, err
	}
	upgrade, err := chatlogAgentUpgradeFactory(sourceClient)
	if err != nil {
		return nil, errors.Join(err, outbox.Close())
	}
	adapter := &chatlogControlAdapter{reader: reader}
	control, err := app.NewSourceAgentRunner(app.SourceAgentRunnerConfig{
		Client: sourceClient, Outbox: outbox, Adapter: adapter, Diagnoser: adapter,
		Updater: upgrade.updater, UpgradeState: upgrade.state, WorkerType: chatlogAgentWorkerType,
		Version: chatlogAgentVersion, ProtocolVersion: chatlogAgentProtocolVersion,
		Revision: chatlogAgentRevision, LeaseDuration: 2 * time.Minute, ControlOnly: true,
	})
	if err != nil {
		return nil, errors.Join(err, upgrade.closer.Close(), outbox.Close())
	}
	return &chatlogAgentRuntime{
		sourceClient: sourceClient, researchClient: researchClient, reader: reader,
		control: control, outbox: outbox, upgrade: upgrade.closer, agentID: config.Source.AgentID,
	}, nil
}

type chatlogControlAdapter struct{ reader *app.ChatlogHTTPReader }

func (*chatlogControlAdapter) Name() string         { return chatlogAgentCapability }
func (*chatlogControlAdapter) Operations() []string { return []string{chatlogAgentCapability} }
func (a *chatlogControlAdapter) Status(ctx context.Context) app.SourceCapabilityHealth {
	if a == nil || a.reader == nil {
		return app.SourceCapabilityHealth{Healthy: false, Code: "dependency_unavailable", Version: chatlogAgentVersion}
	}
	if _, err := a.reader.ListSessions(ctx, "", 1, 0); err != nil {
		return app.SourceCapabilityHealth{Healthy: false, Code: "dependency_unavailable", Version: chatlogAgentVersion}
	}
	return app.SourceCapabilityHealth{Healthy: true, Version: chatlogAgentVersion}
}
func (a *chatlogControlAdapter) Diagnose(ctx context.Context) app.SourceAgentDiagnosticReport {
	if a.Status(ctx).Healthy {
		return app.SourceAgentDiagnosticReport{State: app.SourceAgentCommandSucceeded, Code: app.SourceAgentCommandCodeDiagnosticComplete}
	}
	return app.SourceAgentDiagnosticReport{State: app.SourceAgentCommandFailed, Code: app.SourceAgentCommandCodeDiagnosticFailed}
}
func (*chatlogControlAdapter) Execute(context.Context, app.SourceSyncRun, app.SourceEnvelopeSink) (app.SourceAdapterResult, error) {
	return app.SourceAdapterResult{}, fmt.Errorf("chatlog read jobs use the research worker protocol")
}

func (r *chatlogAgentRuntime) doctor(ctx context.Context, output io.Writer) error {
	if _, err := r.reader.ListSessions(ctx, "", 1, 0); err != nil {
		return fmt.Errorf("check local Chatlog API: dependency unavailable")
	}
	if err := r.sourceClient.CheckAuth(ctx); err != nil {
		return fmt.Errorf("check remote source-agent authentication failed")
	}
	return writeChatlogAgentJSON(output, map[string]any{
		"ok": true, "chatlog_read": true, "remote_auth": true, "agent_version": chatlogAgentVersion,
	})
}

func (r *chatlogAgentRuntime) once(ctx context.Context) (chatlogAgentCycleResult, error) {
	if r.control == nil {
		return chatlogAgentCycleResult{}, fmt.Errorf("chatlog control runner is not configured")
	}
	controlResult, err := r.control.RunOnce(ctx)
	if err != nil {
		return chatlogAgentCycleResult{}, err
	}
	if !controlResult.OK {
		return chatlogAgentCycleResult{OK: false, Heartbeat: true, Code: "dependency_unavailable"}, nil
	}
	if r.control.ControlActive() {
		return chatlogAgentCycleResult{OK: true, Heartbeat: true}, nil
	}
	job, err := r.researchClient.Claim(ctx, 2*time.Minute)
	if err != nil {
		return chatlogAgentCycleResult{}, err
	}
	if job == nil {
		return chatlogAgentCycleResult{OK: true, Heartbeat: true}, nil
	}
	result, err := r.executeJob(ctx, *job)
	if err != nil {
		failed, reportErr := r.researchClient.Fail(ctx, *job, chatlogWorkerFailureCode(err), isRetryableChatlogWorkerError(err))
		if reportErr != nil {
			return chatlogAgentCycleResult{}, reportErr
		}
		return chatlogAgentCycleResult{OK: false, Heartbeat: true, JobID: failed.JobID, JobState: failed.State, Code: failed.FailureCode}, nil
	}
	completed, err := r.researchClient.Complete(ctx, *job, result)
	if err != nil {
		return chatlogAgentCycleResult{}, err
	}
	return chatlogAgentCycleResult{
		OK: true, Heartbeat: true, JobID: completed.JobID, JobState: completed.State, EvidenceCount: len(result.Items),
	}, nil
}

func (r *chatlogAgentRuntime) close() error {
	if r == nil {
		return nil
	}
	var closeErrors []error
	if r.upgrade != nil {
		closeErrors = append(closeErrors, r.upgrade.Close())
	}
	if r.outbox != nil {
		closeErrors = append(closeErrors, r.outbox.Close())
	}
	return errors.Join(closeErrors...)
}

func newChatlogProductionUpgradeRuntime(client *app.SourceAgentClient) (chatlogWorkerUpgradeRuntime, error) {
	workerExecutable, err := os.Executable()
	if err != nil {
		return chatlogWorkerUpgradeRuntime{}, fmt.Errorf("resolve chatlog agent executable: %w", err)
	}
	workerExecutable, err = filepath.EvalSymlinks(filepath.Clean(workerExecutable))
	if err != nil || filepath.Base(workerExecutable) != "chatlog-agent" {
		return chatlogWorkerUpgradeRuntime{}, fmt.Errorf("chatlog agent must run from its fixed installed executable")
	}
	activator, err := app.NewSourceAgentUpdaterActivator(chatlogAgentWorkerType, os.Getuid(), nil)
	if err != nil {
		return chatlogWorkerUpgradeRuntime{}, err
	}
	bridge, err := newChatlogWorkerUpgradeBridge(client, workerExecutable, activator)
	if err != nil {
		return chatlogWorkerUpgradeRuntime{}, err
	}
	return chatlogWorkerUpgradeRuntime{updater: bridge, state: bridge, closer: bridge}, nil
}

func newChatlogWorkerUpgradeBridge(client *app.SourceAgentClient, workerExecutable string, activator app.SourceAgentUpdaterActivator) (*app.SourceAgentUpdateBridge, error) {
	if client == nil || filepath.Base(workerExecutable) != "chatlog-agent" {
		return nil, fmt.Errorf("invalid fixed Chatlog upgrade runtime")
	}
	return app.NewSourceAgentUpdateBridge(app.SourceAgentUpdateBridgeConfig{
		Downloader: client, UpdaterExecutable: filepath.Join(filepath.Dir(workerExecutable), "source-agent-updater"),
		WorkerType: chatlogAgentWorkerType, CurrentVersion: chatlogAgentVersion,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		ProtocolVersion: chatlogAgentProtocolVersion, Revision: chatlogAgentRevision, Activator: activator,
	})
}

type chatlogAgentFailClosedUpdater struct{}

func (*chatlogAgentFailClosedUpdater) Upgrade(context.Context, app.SourceAgentCommand) app.SourceAgentUpgradeResult {
	return app.SourceAgentUpgradeResult{State: app.SourceAgentCommandFailed, Code: app.SourceAgentCommandCodeUpgradeFailed}
}

func (r *chatlogAgentRuntime) executeJob(ctx context.Context, job app.ResearchWorkerJob) (app.ResearchWorkerResult, error) {
	switch job.Tool {
	case app.ResearchWorkerToolSearchChatlog:
		var args app.ResearchWorkerSearchChatlogArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		messages, err := r.reader.SearchMessages(ctx, app.ChatlogQuery{
			Time: chatlogDateRange(args.TimeFrom, args.TimeTo), Talker: args.TalkerRef,
			Sender: args.SenderRef, Keyword: args.Keyword, Limit: args.Limit, Offset: args.Offset,
		})
		if err != nil {
			return app.ResearchWorkerResult{}, err
		}
		return r.resultForMessages(messages), nil
	case app.ResearchWorkerToolExpandChatContext:
		var args app.ResearchWorkerExpandChatContextArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		messages, err := r.reader.SearchMessages(ctx, app.ChatlogQuery{Time: args.Time, Talker: args.ConversationRef, Limit: 500})
		if err != nil {
			return app.ResearchWorkerResult{}, err
		}
		contextMessages, err := selectChatlogContext(messages, args.MessageRef, args.Before, args.After)
		if err != nil {
			return app.ResearchWorkerResult{}, err
		}
		return r.resultForMessages(contextMessages), nil
	case app.ResearchWorkerToolFetchChatMessage:
		var args app.ResearchWorkerFetchChatMessageArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		messages, err := r.reader.SearchMessages(ctx, app.ChatlogQuery{Time: args.Time, Talker: args.ConversationRef, Limit: 500})
		if err != nil {
			return app.ResearchWorkerResult{}, err
		}
		selected, err := selectChatlogContext(messages, args.MessageRef, 0, 0)
		if err != nil {
			return app.ResearchWorkerResult{}, err
		}
		return r.resultForMessages(selected), nil
	case app.ResearchWorkerToolResolveChatIdentity:
		var args app.ResearchWorkerResolveChatIdentityArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		if _, err := r.reader.ListContacts(ctx, args.IdentityRef, 50, 0); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		return app.ResearchWorkerResult{SearchedSources: []string{app.ResearchSourceChatlog}}, nil
	case app.ResearchWorkerToolListIdentityConversations:
		var args app.ResearchWorkerListIdentityConversationsArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		if _, err := r.reader.ListSessions(ctx, args.IdentityRef, args.Limit, args.Offset); err != nil {
			return app.ResearchWorkerResult{}, err
		}
		return app.ResearchWorkerResult{SearchedSources: []string{app.ResearchSourceChatlog}}, nil
	default:
		return app.ResearchWorkerResult{}, fmt.Errorf("unsupported research job")
	}
}

func (r *chatlogAgentRuntime) resultForMessages(messages []app.ChatlogMessage) app.ResearchWorkerResult {
	items := make([]app.ResearchWorkerEvidenceCandidate, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		items = append(items, app.ResearchWorkerEvidenceCandidate{
			SourceType: app.ResearchEvidenceSourceChatlog, SourceRole: app.ResearchEvidenceRoleUserHistory,
			AuthorIdentityID: chatlogOpaqueIdentity(message.Sender), OccurredAt: message.Time.Format(time.RFC3339),
			Content: content, Locator: app.ResearchEvidenceLocator{
				WorkerID: r.agentID, ConversationRef: message.Talker, MessageRef: message.MessageRef,
			},
			Privacy: app.ResearchEvidencePrivacyPrivate, Selected: true,
		})
	}
	return app.ResearchWorkerResult{SearchedSources: []string{app.ResearchSourceChatlog}, Items: items}
}

func selectChatlogContext(messages []app.ChatlogMessage, messageRef string, before, after int) ([]app.ChatlogMessage, error) {
	index := -1
	for candidateIndex := range messages {
		if messages[candidateIndex].MessageRef == messageRef {
			index = candidateIndex
			break
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("chatlog message is unavailable")
	}
	start, end := index-before, index+after+1
	if start < 0 {
		start = 0
	}
	if end > len(messages) {
		end = len(messages)
	}
	return append([]app.ChatlogMessage(nil), messages[start:end]...), nil
}

func chatlogDateRange(fromValue, toValue string) string {
	from, fromErr := time.Parse(time.RFC3339, strings.TrimSpace(fromValue))
	to, toErr := time.Parse(time.RFC3339, strings.TrimSpace(toValue))
	if fromErr != nil || toErr != nil {
		return ""
	}
	fromDate, toDate := from.Format("2006-01-02"), to.Format("2006-01-02")
	if fromDate == toDate {
		return fromDate
	}
	return fromDate + "~" + toDate
}

func chatlogOpaqueIdentity(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "chat-identity-" + hex.EncodeToString(digest[:16])
}

func chatlogWorkerFailureCode(err error) string {
	if errors.Is(err, app.ErrChatlogUnavailable) {
		return "dependency_unavailable"
	}
	if errors.Is(err, app.ErrChatlogInvalidResult) || errors.Is(err, app.ErrChatlogUnsafeRedirect) {
		return "source_invalid"
	}
	return "job_failed"
}

func isRetryableChatlogWorkerError(err error) bool {
	return errors.Is(err, app.ErrChatlogUnavailable)
}

func chatlogAgentPollInterval(lookup chatlogEnvironmentLookup) time.Duration {
	seconds, err := strconv.Atoi(chatlogLookupValue(lookup, "CHATLOG_AGENT_POLL_SECONDS"))
	if err != nil || seconds <= 0 {
		seconds = 15
	}
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func chatlogLookupValue(lookup chatlogEnvironmentLookup, key string) string {
	value, _ := lookup(key)
	return strings.TrimSpace(value)
}

func writeChatlogAgentJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
