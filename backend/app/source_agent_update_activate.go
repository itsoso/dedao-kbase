package app

import "context"

// SourceAgentUpdaterActivator starts only the separately supervised updater
// job whose identity is derived from a known worker type.
type SourceAgentUpdaterActivator interface {
	StartUpdater(context.Context) error
}

// SourceAgentLaunchctlRunner is injectable so the fixed launchctl invocation
// can be verified without mutating a real LaunchAgent.
type SourceAgentLaunchctlRunner interface {
	Run(context.Context, string, ...string) error
}
