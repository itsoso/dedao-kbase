package evolutionworkercli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

const (
	defaultLeaseSeconds = 60
	defaultRenewSeconds = 20
	defaultPollSeconds  = 2
)

type EnvironmentLookup func(string) (string, bool)

type Metadata struct {
	Component  string
	Version    string
	Revision   string
	Capability app.EvolutionWorkerCapability
}

type parsedConfig struct {
	client        app.EvolutionWorkerClientConfig
	leaseDuration time.Duration
	renewInterval time.Duration
	pollInterval  time.Duration
}

func Run(ctx context.Context, args []string, getenv EnvironmentLookup, stdout io.Writer, metadata Metadata) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if len(args) != 1 {
		return errors.New("usage: evolution worker build-info|check-config|run")
	}
	switch args[0] {
	case "build-info":
		return json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int    `json:"schema_version"`
			Component     string `json:"component"`
			Version       string `json:"version"`
			Revision      string `json:"revision"`
			Protocol      string `json:"protocol"`
		}{1, metadata.Component, metadata.Version, metadata.Revision, "evolution-worker.v1"})
	case "check-config":
		config, err := parseConfig(getenv)
		if err != nil {
			return err
		}
		if _, err := buildWorker(config, metadata); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int    `json:"schema_version"`
			Status        string `json:"status"`
		}{1, "ok"})
	case "run":
		config, err := parseConfig(getenv)
		if err != nil {
			return err
		}
		worker, err := buildWorker(config, metadata)
		if err != nil {
			return err
		}
		return worker.Run(ctx)
	default:
		return errors.New("usage: evolution worker build-info|check-config|run")
	}
}

func buildWorker(config parsedConfig, metadata Metadata) (*app.EvolutionGenerationWorker, error) {
	client, err := app.NewEvolutionWorkerClient(config.client)
	if err != nil {
		return nil, errors.New("invalid evolution worker control configuration")
	}
	worker, err := app.NewEvolutionGenerationWorker(app.EvolutionGenerationWorkerConfig{
		Client: client, Capability: metadata.Capability, Version: metadata.Version, Revision: metadata.Revision,
		LeaseDuration: config.leaseDuration, RenewInterval: config.renewInterval, PollInterval: config.pollInterval,
	})
	if err != nil {
		return nil, errors.New("invalid evolution worker runtime configuration")
	}
	return worker, nil
}

func parseConfig(getenv EnvironmentLookup) (parsedConfig, error) {
	if getenv == nil {
		getenv = os.LookupEnv
	}
	remoteURL := lookup(getenv, "KBASE_REMOTE_URL")
	token := lookup(getenv, "KBASE_SOURCE_AGENT_TOKEN")
	workerID := lookup(getenv, "KBASE_EVOLUTION_WORKER_ID")
	if remoteURL == "" || token == "" || workerID == "" {
		return parsedConfig{}, errors.New("KBASE_REMOTE_URL, KBASE_SOURCE_AGENT_TOKEN, and KBASE_EVOLUTION_WORKER_ID are required")
	}
	lease, err := parseSeconds(getenv, "KBASE_EVOLUTION_LEASE_SECONDS", defaultLeaseSeconds, 2, 900)
	if err != nil {
		return parsedConfig{}, err
	}
	renew, err := parseSeconds(getenv, "KBASE_EVOLUTION_RENEW_SECONDS", defaultRenewSeconds, 1, 899)
	if err != nil {
		return parsedConfig{}, err
	}
	poll, err := parseSeconds(getenv, "KBASE_EVOLUTION_POLL_SECONDS", defaultPollSeconds, 1, 300)
	if err != nil {
		return parsedConfig{}, err
	}
	if renew >= lease {
		return parsedConfig{}, errors.New("KBASE_EVOLUTION_RENEW_SECONDS must be shorter than the lease")
	}
	return parsedConfig{
		client:        app.EvolutionWorkerClientConfig{RemoteURL: remoteURL, Token: token, WorkerID: workerID},
		leaseDuration: time.Duration(lease) * time.Second,
		renewInterval: time.Duration(renew) * time.Second,
		pollInterval:  time.Duration(poll) * time.Second,
	}, nil
}

func parseSeconds(getenv EnvironmentLookup, name string, fallback, minimum, maximum int) (int, error) {
	raw, exists := getenv(name)
	if !exists || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func lookup(getenv EnvironmentLookup, name string) string {
	value, _ := getenv(name)
	return strings.TrimSpace(value)
}
