package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	sourceAgentArtifactCatalogMaxBytes = 1 << 20
	sourceAgentArtifactMaxEntries      = 100
	sourceAgentArtifactListDefault     = 50
	sourceAgentArtifactListMax         = 100
	sourceAgentArtifactMaxBytes        = 256 << 20
	sourceAgentArtifactStorageKeyMax   = 256
	sourceAgentArtifactNotesMaxRunes   = 2000
)

var (
	ErrSourceAgentArtifactCatalogInvalid = errors.New("source agent artifact catalog invalid")
	ErrSourceAgentArtifactNotFound       = errors.New("source agent artifact not found")
	ErrSourceAgentArtifactNotAllowed     = errors.New("source agent artifact is not allowed for rollout")
	ErrSourceAgentArtifactIncompatible   = errors.New("source agent artifact is incompatible")
	ErrSourceAgentArtifactIntegrity      = errors.New("source agent artifact integrity check failed")
)

type SourceAgentArtifact struct {
	ID                string `json:"id"`
	WorkerType        string `json:"worker_type"`
	Platform          string `json:"platform"`
	Architecture      string `json:"architecture"`
	Revision          string `json:"revision"`
	Version           string `json:"version"`
	ProtocolVersion   string `json:"protocol_version"`
	MinimumVersion    string `json:"minimum_version,omitempty"`
	Channel           string `json:"channel"`
	ReleaseNotes      string `json:"release_notes,omitempty"`
	Size              int64  `json:"size"`
	SHA256            string `json:"sha256"`
	StorageKey        string `json:"storage_key"`
	BuildGate         string `json:"build_gate"`
	AllowedForRollout bool   `json:"allowed_for_rollout"`
}

type SourceAgentArtifactPublic struct {
	ID                string `json:"id"`
	WorkerType        string `json:"worker_type"`
	Platform          string `json:"platform"`
	Architecture      string `json:"architecture"`
	Revision          string `json:"revision"`
	Version           string `json:"version"`
	ProtocolVersion   string `json:"protocol_version"`
	MinimumVersion    string `json:"minimum_version,omitempty"`
	Channel           string `json:"channel"`
	ReleaseNotes      string `json:"release_notes,omitempty"`
	Size              int64  `json:"size"`
	SHA256            string `json:"sha256"`
	BuildGate         string `json:"build_gate"`
	AllowedForRollout bool   `json:"allowed_for_rollout"`
}

type SourceAgentArtifactTarget struct {
	WorkerType     string
	Platform       string
	Architecture   string
	CurrentVersion string
}

type SourceAgentArtifactCatalog struct {
	root                  string
	snapshotSlots         chan struct{}
	snapshotTempDir       string
	snapshotLeaseObserver func()
	openArtifact          func(string, []string) (*os.File, error)
}

type sourceAgentArtifactSelection struct {
	artifact SourceAgentArtifact
}

type sourceAgentArtifactCatalogDocument struct {
	Artifacts []SourceAgentArtifact `json:"artifacts"`
}

func NewSourceAgentArtifactCatalog(root string) (*SourceAgentArtifactCatalog, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("%w: root is required", ErrSourceAgentArtifactCatalogInvalid)
	}
	if err := validateSourceAgentArtifactRoot(root); err != nil {
		return nil, fmt.Errorf("%w: root is unavailable", ErrSourceAgentArtifactCatalogInvalid)
	}
	return &SourceAgentArtifactCatalog{
		root:          root,
		snapshotSlots: make(chan struct{}, sourceAgentArtifactSnapshotConcurrency),
		openArtifact:  openSourceAgentArtifactRelative,
	}, nil
}

func (c *SourceAgentArtifactCatalog) List(limit int) ([]SourceAgentArtifactPublic, error) {
	artifacts, err := c.load()
	if err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, fmt.Errorf("%w: invalid list limit", ErrSourceAgentArtifactCatalogInvalid)
	}
	if limit == 0 {
		limit = sourceAgentArtifactListDefault
	}
	if limit > sourceAgentArtifactListMax {
		limit = sourceAgentArtifactListMax
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].ID < artifacts[j].ID })
	if len(artifacts) > limit {
		artifacts = artifacts[:limit]
	}
	result := make([]SourceAgentArtifactPublic, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, artifact.public())
	}
	return result, nil
}

func (c *SourceAgentArtifactCatalog) selectForRollout(id string, target SourceAgentArtifactTarget) (sourceAgentArtifactSelection, error) {
	rawID := id
	id, err := normalizeSourceAgentCommandIdentifier("artifact_id", rawID, true)
	if err != nil || id != rawID {
		return sourceAgentArtifactSelection{}, ErrSourceAgentArtifactNotFound
	}
	artifacts, err := c.load()
	if err != nil {
		return sourceAgentArtifactSelection{}, err
	}
	var selected *SourceAgentArtifact
	for index := range artifacts {
		if artifacts[index].ID == id {
			selected = &artifacts[index]
			break
		}
	}
	if selected == nil {
		return sourceAgentArtifactSelection{}, ErrSourceAgentArtifactNotFound
	}
	if !selected.AllowedForRollout {
		return sourceAgentArtifactSelection{}, ErrSourceAgentArtifactNotAllowed
	}
	if err := selected.validateTarget(target); err != nil {
		return sourceAgentArtifactSelection{}, err
	}
	return sourceAgentArtifactSelection{artifact: *selected}, nil
}

func (c *SourceAgentArtifactCatalog) load() ([]SourceAgentArtifact, error) {
	file, err := openSourceAgentArtifactRelative(c.root, []string{"catalog.json"})
	if err != nil {
		return nil, ErrSourceAgentArtifactCatalogInvalid
	}
	defer file.Close()
	data, err := readBoundedRegularSourceAgentArtifact(file, sourceAgentArtifactCatalogMaxBytes)
	if err != nil {
		return nil, ErrSourceAgentArtifactCatalogInvalid
	}
	if err := rejectDuplicateJSONFields(data); err != nil {
		return nil, ErrSourceAgentArtifactCatalogInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document sourceAgentArtifactCatalogDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrSourceAgentArtifactCatalogInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrSourceAgentArtifactCatalogInvalid
	}
	if document.Artifacts == nil || len(document.Artifacts) > sourceAgentArtifactMaxEntries {
		return nil, ErrSourceAgentArtifactCatalogInvalid
	}
	seen := make(map[string]struct{}, len(document.Artifacts))
	for index := range document.Artifacts {
		if err := document.Artifacts[index].validate(); err != nil {
			return nil, ErrSourceAgentArtifactCatalogInvalid
		}
		if _, exists := seen[document.Artifacts[index].ID]; exists {
			return nil, ErrSourceAgentArtifactCatalogInvalid
		}
		seen[document.Artifacts[index].ID] = struct{}{}
	}
	return document.Artifacts, nil
}

func (artifact SourceAgentArtifact) public() SourceAgentArtifactPublic {
	return SourceAgentArtifactPublic{
		ID: artifact.ID, WorkerType: artifact.WorkerType, Platform: artifact.Platform,
		Architecture: artifact.Architecture, Revision: artifact.Revision, Version: artifact.Version,
		ProtocolVersion: artifact.ProtocolVersion, MinimumVersion: artifact.MinimumVersion,
		Channel: artifact.Channel, ReleaseNotes: artifact.ReleaseNotes, Size: artifact.Size,
		SHA256: artifact.SHA256, BuildGate: artifact.BuildGate, AllowedForRollout: artifact.AllowedForRollout,
	}
}

func (artifact SourceAgentArtifact) validate() error {
	if !isExactSourceAgentArtifactName("id", artifact.ID) || artifact.ID == "." || artifact.ID == ".." {
		return errors.New("invalid artifact id")
	}
	if !isExactSourceAgentArtifactName("worker_type", artifact.WorkerType) {
		return errors.New("invalid worker type")
	}
	if artifact.Platform != "darwin" {
		return errors.New("unsupported platform")
	}
	if artifact.Architecture != "arm64" && artifact.Architecture != "amd64" {
		return errors.New("unsupported architecture")
	}
	if !isExactLowerHex(artifact.Revision, 40) && !isExactLowerHex(artifact.Revision, 64) {
		return errors.New("invalid revision")
	}
	if !isSourceAgentArtifactVersion(artifact.Version) {
		return errors.New("invalid version")
	}
	if artifact.MinimumVersion != "" && !isSourceAgentArtifactVersion(artifact.MinimumVersion) {
		return errors.New("invalid minimum version")
	}
	if _, err := time.Parse("2006-01-02", artifact.ProtocolVersion); err != nil || len(artifact.ProtocolVersion) != len("2006-01-02") {
		return errors.New("invalid protocol version")
	}
	if artifact.Channel != "staging" && artifact.Channel != "production" {
		return errors.New("unsupported channel")
	}
	if artifact.ReleaseNotes != strings.TrimSpace(artifact.ReleaseNotes) || len([]rune(artifact.ReleaseNotes)) > sourceAgentArtifactNotesMaxRunes {
		return errors.New("invalid release notes")
	}
	for _, character := range artifact.ReleaseNotes {
		if unicode.IsControl(character) {
			return errors.New("invalid release notes")
		}
	}
	if containsSourceAgentCommandURL(artifact.ReleaseNotes) || containsSourceAgentCommandAbsolutePath(artifact.ReleaseNotes) {
		return errors.New("unsafe release notes")
	}
	if artifact.Size <= 0 || artifact.Size > sourceAgentArtifactMaxBytes {
		return errors.New("invalid artifact size")
	}
	if !isExactLowerHex(artifact.SHA256, sha256.Size*2) {
		return errors.New("invalid artifact sha256")
	}
	if !isSafeSourceAgentArtifactStorageKey(artifact.StorageKey) {
		return errors.New("invalid storage key")
	}
	if artifact.BuildGate != "passed" {
		return errors.New("build gate did not pass")
	}
	return nil
}

func (artifact SourceAgentArtifact) validateTarget(target SourceAgentArtifactTarget) error {
	if target.WorkerType != artifact.WorkerType || target.Platform != artifact.Platform || target.Architecture != artifact.Architecture {
		return ErrSourceAgentArtifactIncompatible
	}
	if !isSourceAgentArtifactVersion(target.CurrentVersion) {
		return ErrSourceAgentArtifactIncompatible
	}
	if artifact.MinimumVersion != "" && compareSourceAgentArtifactVersions(target.CurrentVersion, artifact.MinimumVersion) < 0 {
		return ErrSourceAgentArtifactIncompatible
	}
	if compareSourceAgentArtifactVersions(target.CurrentVersion, artifact.Version) >= 0 {
		return ErrSourceAgentArtifactIncompatible
	}
	return nil
}

func isExactSourceAgentArtifactName(field, value string) bool {
	normalized, err := normalizeSourceAgentName(field, value, sourceAgentRuntimeNameMaxRunes, false)
	return err == nil && normalized != "" && normalized == value
}

func isExactLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func isSourceAgentArtifactVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 31); err != nil {
			return false
		}
	}
	return true
}

func compareSourceAgentArtifactVersions(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < 3; index++ {
		leftValue, _ := strconv.ParseUint(leftParts[index], 10, 31)
		rightValue, _ := strconv.ParseUint(rightParts[index], 10, 31)
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func isSafeSourceAgentArtifactStorageKey(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > sourceAgentArtifactStorageKeyMax ||
		strings.Contains(value, "\\") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || !isExactSourceAgentArtifactName("storage_key", part) {
			return false
		}
	}
	return true
}

func readBoundedRegularSourceAgentArtifact(file *os.File, maximum int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximum {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	return data, nil
}

func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkUniqueJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected trailing token %v", token)
		}
		return err
	}
	return nil
}

func walkUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[name]; exists {
				return errors.New("duplicate object key")
			}
			seen[name] = struct{}{}
			if err := walkUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
