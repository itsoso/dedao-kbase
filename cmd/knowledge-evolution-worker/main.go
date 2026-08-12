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
	knowledgeEvolutionWorkerVersion  = "development"
	knowledgeEvolutionWorkerRevision = "development"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runKnowledgeEvolutionWorkerCLI(ctx, os.Args[1:], os.LookupEnv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "knowledge-evolution-worker failed")
		os.Exit(1)
	}
}

func runKnowledgeEvolutionWorkerCLI(ctx context.Context, args []string, getenv evolutionworkercli.EnvironmentLookup, stdout io.Writer) error {
	return evolutionworkercli.Run(ctx, args, getenv, stdout, evolutionworkercli.Metadata{
		Component: "knowledge-evolution-worker", Version: knowledgeEvolutionWorkerVersion,
		Revision: knowledgeEvolutionWorkerRevision, Capability: app.EvolutionCapabilityKnowledge,
	})
}
