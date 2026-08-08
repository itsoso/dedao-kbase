//go:build darwin

package app

import (
	"context"
	"reflect"
	"testing"
)

type sourceAgentLaunchctlCall struct {
	path string
	args []string
}

type sourceAgentLaunchctlRecorder struct {
	calls []sourceAgentLaunchctlCall
}

func (r *sourceAgentLaunchctlRecorder) Run(_ context.Context, path string, args ...string) error {
	r.calls = append(r.calls, sourceAgentLaunchctlCall{path: path, args: append([]string(nil), args...)})
	return nil
}

func TestSourceAgentUpdaterJobUsesFixedIndependentLaunchAgent(t *testing.T) {
	tests := []struct {
		workerType string
		label      string
	}{
		{workerType: "wechat-worker", label: "life.executor.kbase.source-agent.updater"},
		{workerType: "wcplus-worker", label: "life.executor.kbase.wcplus-agent.updater"},
	}
	for _, test := range tests {
		t.Run(test.workerType, func(t *testing.T) {
			runner := &sourceAgentLaunchctlRecorder{}
			activator, err := NewSourceAgentUpdaterActivator(test.workerType, 501, runner)
			if err != nil {
				t.Fatalf("NewSourceAgentUpdaterActivator() error = %v", err)
			}
			if err := activator.StartUpdater(context.Background()); err != nil {
				t.Fatalf("StartUpdater() error = %v", err)
			}
			want := []sourceAgentLaunchctlCall{{
				path: "/bin/launchctl",
				args: []string{"kickstart", "gui/501/" + test.label},
			}}
			if !reflect.DeepEqual(runner.calls, want) {
				t.Fatalf("launchctl calls = %#v, want %#v", runner.calls, want)
			}
		})
	}
}

func TestSourceAgentUpdaterJobRejectsRemoteSelectedIdentity(t *testing.T) {
	for _, workerType := range []string{"", "unknown", "wechat-worker/other", "life.executor.kbase.source-agent.updater"} {
		if _, err := NewSourceAgentUpdaterActivator(workerType, 501, &sourceAgentLaunchctlRecorder{}); err == nil {
			t.Fatalf("NewSourceAgentUpdaterActivator(%q) succeeded", workerType)
		}
	}
	if _, err := NewSourceAgentUpdaterActivator("wechat-worker", -1, &sourceAgentLaunchctlRecorder{}); err == nil {
		t.Fatal("NewSourceAgentUpdaterActivator() accepted negative uid")
	}
}
