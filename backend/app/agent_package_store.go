package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const agentPackageStoreVersion = "1"

var (
	ErrAgentPackageIdempotencyConflict = errors.New("agent package idempotency conflict")
	ErrAgentPackageVersionConflict     = errors.New("agent package version conflict")
	agentPackageArtifactLoadHook       = func(context.Context, string) error { return nil }
)

type AgentPackageRecord struct {
	PackageID      string                         `json:"package_id"`
	Version        string                         `json:"version"`
	ContentHash    string                         `json:"content_hash"`
	LifecycleState string                         `json:"lifecycle_state"`
	Supersedes     string                         `json:"supersedes,omitempty"`
	PublishedAt    string                         `json:"published_at"`
	URL            string                         `json:"url"`
	Runtime        *AgentPackageRuntimeDescriptor `json:"runtime,omitempty"`
}

type AgentPackageRuntimeDescriptor struct {
	SchemaVersion  string `json:"schema_version"`
	PackageID      string `json:"package_id"`
	Version        string `json:"version"`
	ContentHash    string `json:"content_hash"`
	TimeoutMS      int    `json:"timeout_ms"`
	DescriptorHash string `json:"descriptor_hash"`
}

type AgentPackageIdempotencyRecord struct {
	IdempotencyKey string `json:"idempotency_key"`
	PackageID      string `json:"package_id"`
	Version        string `json:"version"`
	ContentHash    string `json:"content_hash"`
}

type AgentPackageManifest struct {
	Version     string                          `json:"version"`
	UpdatedAt   string                          `json:"updated_at"`
	Packages    []AgentPackageRecord            `json:"packages"`
	Idempotency []AgentPackageIdempotencyRecord `json:"idempotency"`
}

func (s *BookKnowledgeStore) AgentPackageDir() string {
	return filepath.Join(s.root, "agent-packages")
}

func (s *BookKnowledgeStore) AgentPackageManifestPath() string {
	return filepath.Join(s.AgentPackageDir(), "manifest.json")
}

func (s *BookKnowledgeStore) AgentPackagePath(contentHash string) string {
	name := strings.TrimPrefix(strings.TrimSpace(contentHash), "sha256:")
	return filepath.Join(s.AgentPackageDir(), sanitizeBookKnowledgeID(name)+".json")
}

func PublishAgentPackage(store *BookKnowledgeStore, pkg AgentPackage, idempotencyKey string, knownTools []string, now time.Time) (*AgentPackage, bool, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, false, fmt.Errorf("idempotency_key is required")
	}
	if err := ValidateAgentPackage(pkg, store, knownTools); err != nil {
		return nil, false, err
	}
	if err := ValidateAgentPackageEvaluationGate(store, pkg); err != nil {
		return nil, false, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	timestamp := now.UTC().Format(time.RFC3339Nano)

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.MkdirAll(store.AgentPackageDir(), os.ModePerm); err != nil {
		return nil, false, err
	}
	manifest, err := store.loadAgentPackageManifestUnlocked()
	if err != nil {
		return nil, false, err
	}
	for _, record := range manifest.Idempotency {
		if record.IdempotencyKey != idempotencyKey {
			continue
		}
		if record.ContentHash != pkg.ContentHash {
			return nil, false, fmt.Errorf("%w: key %q already published different content", ErrAgentPackageIdempotencyConflict, idempotencyKey)
		}
		published, err := store.loadAgentPackageByIdentityUnlocked(manifest, record.PackageID, record.Version)
		return published, false, err
	}

	var previous *AgentPackageRecord
	for index := range manifest.Packages {
		record := &manifest.Packages[index]
		if record.PackageID != pkg.PackageID {
			continue
		}
		if record.Version == pkg.Version {
			if record.ContentHash != pkg.ContentHash {
				return nil, false, fmt.Errorf("%w: %s", ErrAgentPackageVersionConflict, agentPackageReference(pkg.PackageID, pkg.Version))
			}
			manifest.Idempotency = append(manifest.Idempotency, AgentPackageIdempotencyRecord{
				IdempotencyKey: idempotencyKey,
				PackageID:      record.PackageID,
				Version:        record.Version,
				ContentHash:    record.ContentHash,
			})
			manifest.UpdatedAt = timestamp
			if err := store.writeAgentPackageManifestUnlocked(manifest); err != nil {
				return nil, false, err
			}
			published, err := store.loadAgentPackageRecordUnlocked(*record)
			return published, false, err
		}
		if record.LifecycleState == AgentPackagePublished {
			previous = record
		}
	}

	published := pkg
	published.LifecycleState = AgentPackagePublished
	published.CreatedAt = timestamp
	published.PublishedAt = timestamp
	if previous != nil {
		published.Supersedes = agentPackageReference(previous.PackageID, previous.Version)
	}
	artifact, err := encodeJSONFile(published)
	if err != nil {
		return nil, false, err
	}
	artifactPath := store.AgentPackagePath(published.ContentHash)
	if _, err := os.Stat(artifactPath); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomically(artifactPath, artifact); err != nil {
			return nil, false, err
		}
	} else if err != nil {
		return nil, false, err
	}

	if previous != nil {
		previous.LifecycleState = AgentPackageSuperseded
	}
	record := AgentPackageRecord{
		PackageID:      published.PackageID,
		Version:        published.Version,
		ContentHash:    published.ContentHash,
		LifecycleState: AgentPackagePublished,
		Supersedes:     published.Supersedes,
		PublishedAt:    published.PublishedAt,
		URL:            agentPackageURL(published.PackageID, published.Version),
	}
	if published.SchemaVersion == AgentPackageSchemaVersionV2 {
		descriptor, descriptorErr := newAgentPackageRuntimeDescriptor(published)
		if descriptorErr != nil {
			return nil, false, descriptorErr
		}
		record.Runtime = &descriptor
	}
	manifest.Version = agentPackageStoreVersion
	manifest.UpdatedAt = timestamp
	manifest.Packages = append(manifest.Packages, record)
	manifest.Idempotency = append(manifest.Idempotency, AgentPackageIdempotencyRecord{
		IdempotencyKey: idempotencyKey,
		PackageID:      published.PackageID,
		Version:        published.Version,
		ContentHash:    published.ContentHash,
	})
	if err := store.writeAgentPackageManifestUnlocked(manifest); err != nil {
		return nil, false, err
	}
	return &published, true, nil
}

func (s *BookKnowledgeStore) LoadAgentPackage(packageID, version string) (*AgentPackage, error) {
	return s.LoadAgentPackageContext(context.Background(), packageID, version)
}

func (s *BookKnowledgeStore) LoadAgentPackageContext(
	ctx context.Context, packageID, version string,
) (*AgentPackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	manifest, err := s.loadAgentPackageManifestUnlocked()
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	record, err := selectAgentPackageRecord(
		manifest, strings.TrimSpace(packageID), strings.TrimSpace(version),
	)
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	pkg, err := s.loadAgentPackageRecordContextUnlocked(ctx, *record)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateAgentPackage(*pkg, s, AgentReadOnlyToolIDs()); err != nil {
		return nil, fmt.Errorf("validate persisted agent package: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pkg, nil
}

func (s *BookKnowledgeStore) LoadAgentPackageV2RuntimeDescriptor(
	packageID, version, contentHash string,
) (*AgentPackageRuntimeDescriptor, error) {
	return s.LoadAgentPackageV2RuntimeDescriptorContext(
		context.Background(), packageID, version, contentHash,
	)
}

func (s *BookKnowledgeStore) LoadAgentPackageV2RuntimeDescriptorContext(
	ctx context.Context,
	packageID, version, contentHash string,
) (*AgentPackageRuntimeDescriptor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	manifest, err := s.loadAgentPackageManifestUnlocked()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := selectAgentPackageRecord(
		manifest, strings.TrimSpace(packageID), strings.TrimSpace(version),
	)
	if err != nil {
		return nil, err
	}
	if record.ContentHash != strings.TrimSpace(contentHash) {
		return nil, fmt.Errorf("agent package runtime descriptor content hash does not match audit")
	}
	if err := validateAgentPackageRuntimeDescriptor(*record, nil); err != nil {
		return nil, err
	}
	descriptor := *record.Runtime
	return &descriptor, nil
}

func (s *BookKnowledgeStore) ListAgentPackages(after string, limit int) ([]AgentPackageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	manifest, err := s.loadAgentPackageManifestUnlocked()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	start := 0
	after = strings.TrimSpace(after)
	if after != "" {
		start = len(manifest.Packages)
		for index, record := range manifest.Packages {
			if agentPackageReference(record.PackageID, record.Version) == after {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(manifest.Packages) {
		end = len(manifest.Packages)
	}
	return append([]AgentPackageRecord{}, manifest.Packages[start:end]...), nil
}

func (s *BookKnowledgeStore) loadAgentPackageByIdentityUnlocked(manifest *AgentPackageManifest, packageID, version string) (*AgentPackage, error) {
	selected, err := selectAgentPackageRecord(manifest, packageID, version)
	if err != nil {
		return nil, err
	}
	return s.loadAgentPackageRecordUnlocked(*selected)
}

func selectAgentPackageRecord(
	manifest *AgentPackageManifest, packageID, version string,
) (*AgentPackageRecord, error) {
	if packageID == "" {
		return nil, fmt.Errorf("package_id is required")
	}
	var selected *AgentPackageRecord
	for index := range manifest.Packages {
		record := &manifest.Packages[index]
		if record.PackageID != packageID {
			continue
		}
		if version != "" {
			if record.Version == version {
				selected = record
				break
			}
			continue
		}
		if record.LifecycleState == AgentPackagePublished {
			selected = record
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("%w: agent package %q", os.ErrNotExist, packageID)
	}
	return selected, nil
}

func (s *BookKnowledgeStore) loadAgentPackageRecordUnlocked(record AgentPackageRecord) (*AgentPackage, error) {
	return s.loadAgentPackageRecordContextUnlocked(context.Background(), record)
}

func (s *BookKnowledgeStore) loadAgentPackageRecordContextUnlocked(
	ctx context.Context, record AgentPackageRecord,
) (*AgentPackage, error) {
	artifactPath := s.AgentPackagePath(record.ContentHash)
	if err := agentPackageArtifactLoadHook(ctx, artifactPath); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var pkg AgentPackage
	if err := readJSONFile(artifactPath, &pkg); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pkg.PackageID != record.PackageID || pkg.Version != record.Version {
		return nil, fmt.Errorf("agent package artifact identity does not match manifest record")
	}
	if pkg.SchemaVersion == AgentPackageSchemaVersionV2 {
		if err := validateAgentPackageRuntimeDescriptor(record, &pkg); err != nil {
			return nil, err
		}
	}
	wantHash, err := AgentPackageContentHash(pkg)
	if err != nil {
		return nil, fmt.Errorf("hash persisted agent package: %w", err)
	}
	if pkg.ContentHash != record.ContentHash || wantHash != record.ContentHash {
		return nil, fmt.Errorf("agent package artifact content hash does not match manifest record")
	}
	if pkg.LifecycleState != AgentPackagePublished || pkg.Supersedes != record.Supersedes ||
		pkg.CreatedAt != record.PublishedAt || pkg.PublishedAt != record.PublishedAt {
		return nil, fmt.Errorf("agent package artifact publication metadata does not match manifest record")
	}
	pkg.LifecycleState = record.LifecycleState
	pkg.Supersedes = record.Supersedes
	pkg.PublishedAt = record.PublishedAt
	return &pkg, nil
}

func newAgentPackageRuntimeDescriptor(pkg AgentPackage) (AgentPackageRuntimeDescriptor, error) {
	descriptor := AgentPackageRuntimeDescriptor{
		SchemaVersion: pkg.SchemaVersion,
		PackageID:     pkg.PackageID,
		Version:       pkg.Version,
		ContentHash:   pkg.ContentHash,
		TimeoutMS:     pkg.ModelPolicy.TimeoutMS,
	}
	hash, err := agentPackageRuntimeDescriptorHash(descriptor)
	if err != nil {
		return AgentPackageRuntimeDescriptor{}, err
	}
	descriptor.DescriptorHash = hash
	return descriptor, nil
}

func validateAgentPackageRuntimeDescriptor(record AgentPackageRecord, pkg *AgentPackage) error {
	if record.Runtime == nil {
		return fmt.Errorf("agent package v2 runtime descriptor is required")
	}
	descriptor := *record.Runtime
	if descriptor.SchemaVersion != AgentPackageSchemaVersionV2 ||
		descriptor.PackageID != record.PackageID ||
		descriptor.Version != record.Version ||
		descriptor.ContentHash != record.ContentHash ||
		descriptor.TimeoutMS <= 0 {
		return fmt.Errorf("agent package runtime descriptor does not match manifest record")
	}
	wantHash, err := agentPackageRuntimeDescriptorHash(descriptor)
	if err != nil {
		return err
	}
	if descriptor.DescriptorHash != wantHash {
		return fmt.Errorf("agent package runtime descriptor hash does not match")
	}
	if pkg != nil && (pkg.SchemaVersion != descriptor.SchemaVersion ||
		pkg.PackageID != descriptor.PackageID ||
		pkg.Version != descriptor.Version ||
		pkg.ContentHash != descriptor.ContentHash ||
		pkg.ModelPolicy.TimeoutMS != descriptor.TimeoutMS) {
		return fmt.Errorf("agent package runtime descriptor does not match artifact")
	}
	return nil
}

func agentPackageRuntimeDescriptorHash(descriptor AgentPackageRuntimeDescriptor) (string, error) {
	payload, err := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		PackageID     string `json:"package_id"`
		Version       string `json:"version"`
		ContentHash   string `json:"content_hash"`
		TimeoutMS     int    `json:"timeout_ms"`
	}{
		SchemaVersion: descriptor.SchemaVersion,
		PackageID:     descriptor.PackageID,
		Version:       descriptor.Version,
		ContentHash:   descriptor.ContentHash,
		TimeoutMS:     descriptor.TimeoutMS,
	})
	if err != nil {
		return "", err
	}
	return sha256Fingerprint(payload), nil
}

func (s *BookKnowledgeStore) loadAgentPackageManifestUnlocked() (*AgentPackageManifest, error) {
	var manifest AgentPackageManifest
	if err := readJSONFile(s.AgentPackageManifestPath(), &manifest); errors.Is(err, os.ErrNotExist) {
		return &AgentPackageManifest{
			Version:     agentPackageStoreVersion,
			Packages:    []AgentPackageRecord{},
			Idempotency: []AgentPackageIdempotencyRecord{},
		}, nil
	} else if err != nil {
		return nil, err
	}
	sort.SliceStable(manifest.Packages, func(i, j int) bool {
		if manifest.Packages[i].PublishedAt != manifest.Packages[j].PublishedAt {
			return manifest.Packages[i].PublishedAt < manifest.Packages[j].PublishedAt
		}
		return agentPackageReference(manifest.Packages[i].PackageID, manifest.Packages[i].Version) <
			agentPackageReference(manifest.Packages[j].PackageID, manifest.Packages[j].Version)
	})
	return &manifest, nil
}

func (s *BookKnowledgeStore) writeAgentPackageManifestUnlocked(manifest *AgentPackageManifest) error {
	payload, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	return writeFileAtomically(s.AgentPackageManifestPath(), payload)
}

func agentPackageReference(packageID, version string) string {
	return strings.TrimSpace(packageID) + "@" + strings.TrimSpace(version)
}

func agentPackageURL(packageID, version string) string {
	return "/api/agent-packages/" + url.PathEscape(strings.TrimSpace(packageID)) +
		"?version=" + url.QueryEscape(strings.TrimSpace(version))
}
