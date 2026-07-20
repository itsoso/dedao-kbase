package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const agentSemanticVectorIndexVersion = "agent-semantic-vector-index.v1"

type AgentSemanticEmbedder interface {
	Identity() string
	Embed(context.Context, []string) ([][]float64, error)
}

type agentSemanticVectorRecord struct {
	ClaimID string    `json:"claim_id"`
	Vector  []float64 `json:"vector"`
}

type agentSemanticVectorIndex struct {
	SchemaVersion    string                      `json:"schema_version"`
	ReleaseID        string                      `json:"release_id"`
	ReleaseHash      string                      `json:"release_hash"`
	EmbedderIdentity string                      `json:"embedder_identity"`
	Vectors          []agentSemanticVectorRecord `json:"vectors"`
}

type openAICompatibleAgentEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func (e *openAICompatibleAgentEmbedder) Identity() string {
	return "openai-compatible:" + strings.TrimSpace(e.model)
}

func (e *openAICompatibleAgentEmbedder) Embed(ctx context.Context, inputs []string) ([][]float64, error) {
	payload, err := json.Marshal(map[string]any{"model": e.model, "input": inputs})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.baseURL, "/")+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+e.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := e.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("semantic embedding request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("semantic embedding request returned HTTP %d", response.StatusCode)
	}
	var decoded struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode semantic embedding response: %w", err)
	}
	vectors := make([][]float64, len(inputs))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(vectors) || len(item.Embedding) == 0 {
			return nil, fmt.Errorf("semantic embedding response has invalid index or empty vector")
		}
		vectors[item.Index] = item.Embedding
	}
	for index, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("semantic embedding response omitted input %d", index)
		}
	}
	return vectors, nil
}

func (s *BookKnowledgeStore) configuredAgentSemanticEmbedder() (AgentSemanticEmbedder, error) {
	s.mu.RLock()
	embedder := s.agentSemanticEmbedder
	s.mu.RUnlock()
	if embedder != nil {
		return embedder, nil
	}
	baseURL := strings.TrimSpace(os.Getenv("KBASE_EMBEDDING_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("KBASE_EMBEDDING_MODEL"))
	apiKey := strings.TrimSpace(os.Getenv("KBASE_EMBEDDING_API_KEY"))
	if baseURL == "" || model == "" || apiKey == "" {
		return nil, fmt.Errorf("semantic embedder is not configured; KBASE_EMBEDDING_BASE_URL, KBASE_EMBEDDING_MODEL, and KBASE_EMBEDDING_API_KEY are required")
	}
	return &openAICompatibleAgentEmbedder{
		baseURL: baseURL, apiKey: apiKey, model: model, httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *BookKnowledgeStore) agentSemanticVectorIndexPath(release KnowledgeRelease, embedder AgentSemanticEmbedder) string {
	seed := release.ReleaseID + "\x00" + release.ContentHash + "\x00" + embedder.Identity()
	sum := sha256.Sum256([]byte(seed))
	return filepath.Join(s.AgentPackageDir(), "vector-indexes", hex.EncodeToString(sum[:])+".json")
}

func (s *BookKnowledgeStore) loadOrBuildAgentSemanticVectorIndex(ctx context.Context, release KnowledgeRelease, embedder AgentSemanticEmbedder) (*agentSemanticVectorIndex, error) {
	path := s.agentSemanticVectorIndexPath(release, embedder)
	var existing agentSemanticVectorIndex
	if err := readJSONFile(path, &existing); err == nil {
		if existing.SchemaVersion == agentSemanticVectorIndexVersion && existing.ReleaseID == release.ReleaseID &&
			existing.ReleaseHash == release.ContentHash && existing.EmbedderIdentity == embedder.Identity() {
			return &existing, nil
		}
		return nil, fmt.Errorf("semantic vector index identity mismatch")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if release.Analysis == nil {
		return &agentSemanticVectorIndex{SchemaVersion: agentSemanticVectorIndexVersion}, nil
	}
	inputs := make([]string, 0, len(release.Analysis.Claims))
	for _, claim := range release.Analysis.Claims {
		inputs = append(inputs, strings.Join(append([]string{claim.Statement}, claim.Scope...), " "))
	}
	vectors, err := embedder.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(release.Analysis.Claims) {
		return nil, fmt.Errorf("semantic embedder returned %d vectors for %d claims", len(vectors), len(release.Analysis.Claims))
	}
	index := &agentSemanticVectorIndex{
		SchemaVersion: agentSemanticVectorIndexVersion, ReleaseID: release.ReleaseID,
		ReleaseHash: release.ContentHash, EmbedderIdentity: embedder.Identity(),
		Vectors: make([]agentSemanticVectorRecord, 0, len(vectors)),
	}
	for claimIndex, vector := range vectors {
		if err := validateAgentSemanticVector(vector); err != nil {
			return nil, fmt.Errorf("claim %q: %w", release.Analysis.Claims[claimIndex].ID, err)
		}
		index.Vectors = append(index.Vectors, agentSemanticVectorRecord{ClaimID: release.Analysis.Claims[claimIndex].ID, Vector: vector})
	}
	payload, err := encodeJSONFile(index)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, err
	}
	if err := writeFileAtomically(path, payload); err != nil {
		return nil, err
	}
	return index, nil
}

func validateAgentSemanticVector(vector []float64) error {
	if len(vector) == 0 {
		return fmt.Errorf("semantic vector is empty")
	}
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("semantic vector contains a non-finite value")
		}
	}
	return nil
}

func agentDenseVectorScore(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	dot, leftNorm, rightNorm := 0.0, 0.0, 0.0
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if dot <= 0 || leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}
