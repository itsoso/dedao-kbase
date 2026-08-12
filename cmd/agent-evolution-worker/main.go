package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/evolutionworkercli"
)

var (
	agentEvolutionWorkerVersion  = "development"
	agentEvolutionWorkerRevision = "development"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runAgentEvolutionWorkerCLI(ctx, os.Args[1:], os.LookupEnv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "agent-evolution-worker failed")
		os.Exit(1)
	}
}

func runAgentEvolutionWorkerCLI(ctx context.Context, args []string, getenv evolutionworkercli.EnvironmentLookup, stdout io.Writer) error {
	return evolutionworkercli.Run(ctx, args, getenv, stdout, evolutionworkercli.Metadata{
		Component: "agent-evolution-worker", Version: agentEvolutionWorkerVersion,
		Revision: agentEvolutionWorkerRevision, Capability: app.EvolutionCapabilityAgent,
	})
}
