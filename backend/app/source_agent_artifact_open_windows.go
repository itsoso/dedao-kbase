package app

import "os"

func validateSourceAgentArtifactRoot(string) error {
	return ErrSourceAgentArtifactCatalogInvalid
}

func openSourceAgentArtifactRelative(string, []string) (*os.File, error) {
	return nil, ErrSourceAgentArtifactIntegrity
}
