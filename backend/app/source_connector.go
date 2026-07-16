package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	SourceLicenseScopePersonalUse  = "personal_use"
	SourceLicenseScopeLicensedUse  = "licensed_use"
	SourceLicenseScopePublicDomain = "public_domain"
)

type SourceConnector interface {
	Name() string
	Capabilities() SourceConnectorCapabilities
	Fetch(context.Context, SourceFetchRequest) (SourceFetchPage, error)
}

type SourceConnectorCapabilities struct {
	Name                string   `json:"name"`
	SupportedOperations []string `json:"supported_operations"`
	SupportsCheckpoint  bool     `json:"supports_checkpoint"`
	SupportsBackfill    bool     `json:"supports_backfill"`
	DefaultLicenseScope string   `json:"default_license_scope"`
}

type SourceFetchRequest struct {
	SourceType       string            `json:"source_type"`
	SourceAccountKey string            `json:"source_account_key"`
	Operation        string            `json:"operation"`
	Limit            int               `json:"limit,omitempty"`
	Checkpoint       SourceCheckpoint  `json:"checkpoint,omitempty"`
	Options          map[string]string `json:"options,omitempty"`
}

type SourceFetchPage struct {
	Checkpoint SourceCheckpoint           `json:"checkpoint"`
	Documents  []SourceDocumentEnvelope   `json:"documents"`
	Failures   []SourceAdapterItemFailure `json:"failures,omitempty"`
}

type SourceCheckpoint struct {
	Cursor    string `json:"cursor,omitempty"`
	Sequence  int64  `json:"sequence,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type SourceDocumentEnvelope struct {
	IdempotencyKey   string            `json:"idempotency_key"`
	SourceType       string            `json:"source_type"`
	SourceAccountKey string            `json:"source_account_key"`
	SourceAccount    string            `json:"source_account,omitempty"`
	SourceItemKey    string            `json:"source_item_key"`
	Title            string            `json:"title"`
	Author           string            `json:"author,omitempty"`
	SourceURL        string            `json:"source_url"`
	PublishedAt      string            `json:"published_at,omitempty"`
	Content          string            `json:"content"`
	ContentFormat    string            `json:"content_format"`
	LicenseScope     string            `json:"license_scope"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

func NormalizeSourceConnectorCapabilities(input SourceConnectorCapabilities) (SourceConnectorCapabilities, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.DefaultLicenseScope = normalizeSourceLicenseScope(input.DefaultLicenseScope)
	if input.Name == "" {
		return input, fmt.Errorf("source connector name is required")
	}
	if input.DefaultLicenseScope == "" {
		input.DefaultLicenseScope = SourceLicenseScopePersonalUse
	}
	if !validSourceLicenseScope(input.DefaultLicenseScope) {
		return input, fmt.Errorf("unsupported source license scope %q", input.DefaultLicenseScope)
	}
	seen := make(map[string]bool)
	operations := make([]string, 0, len(input.SupportedOperations))
	for _, operation := range input.SupportedOperations {
		operation = strings.TrimSpace(operation)
		if operation == "" || seen[operation] {
			continue
		}
		seen[operation] = true
		operations = append(operations, operation)
	}
	if len(operations) == 0 {
		return input, fmt.Errorf("source connector operations are required")
	}
	input.SupportedOperations = operations
	return input, nil
}

func ValidateSourceCheckpointAdvance(previous, next SourceCheckpoint) error {
	if next.Sequence < previous.Sequence {
		return fmt.Errorf("source checkpoint sequence regressed from %d to %d", previous.Sequence, next.Sequence)
	}
	if next.Sequence == previous.Sequence && strings.TrimSpace(next.Cursor) != strings.TrimSpace(previous.Cursor) {
		return fmt.Errorf("source checkpoint cursor changed without sequence advance")
	}
	return nil
}

func NormalizeSourceDocumentEnvelope(input SourceDocumentEnvelope) (SourceDocumentEnvelope, string, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.SourceAccountKey = strings.TrimSpace(input.SourceAccountKey)
	input.SourceAccount = strings.TrimSpace(input.SourceAccount)
	input.SourceItemKey = strings.TrimSpace(input.SourceItemKey)
	input.Title = strings.TrimSpace(input.Title)
	input.Author = strings.TrimSpace(input.Author)
	input.PublishedAt = strings.TrimSpace(input.PublishedAt)
	input.ContentFormat = strings.ToLower(strings.TrimSpace(input.ContentFormat))
	input.LicenseScope = normalizeSourceLicenseScope(input.LicenseScope)
	if input.SourceAccount == "" {
		input.SourceAccount = input.SourceAccountKey
	}
	if input.ContentFormat == "" {
		input.ContentFormat = "markdown"
	}
	if input.LicenseScope == "" {
		input.LicenseScope = SourceLicenseScopePersonalUse
	}
	if input.IdempotencyKey == "" || input.SourceType == "" || input.SourceAccountKey == "" || input.SourceItemKey == "" || input.Title == "" {
		return input, "", fmt.Errorf("idempotency_key, source_type, source_account_key, source_item_key, and title are required")
	}
	if input.ContentFormat != "markdown" {
		return input, "", fmt.Errorf("unsupported content_format %q", input.ContentFormat)
	}
	if !validSourceLicenseScope(input.LicenseScope) {
		return input, "", fmt.Errorf("unsupported source license scope %q", input.LicenseScope)
	}
	canonicalURL, err := canonicalSourceArticleURL(input.SourceURL)
	if err != nil {
		return input, "", err
	}
	input.SourceURL = canonicalURL
	input.Content = normalizeSourceArticleContent(input.Content)
	if strings.TrimSpace(input.Content) == "" {
		return input, "", fmt.Errorf("source document content is required")
	}
	input.Metadata = normalizeSourceConnectorMetadata(input.Metadata)
	sum := sha256.Sum256([]byte(input.Content))
	return input, hex.EncodeToString(sum[:]), nil
}

func SourceArticleEnvelopeFromDocument(document SourceDocumentEnvelope) (SourceArticleEnvelope, string, error) {
	normalized, contentHash, err := NormalizeSourceDocumentEnvelope(document)
	if err != nil {
		return SourceArticleEnvelope{}, "", err
	}
	metadata := make(map[string]string, len(normalized.Metadata)+1)
	for key, value := range normalized.Metadata {
		metadata[key] = value
	}
	metadata["license_scope"] = normalized.LicenseScope
	return SourceArticleEnvelope{
		IdempotencyKey:  normalized.IdempotencyKey,
		SourceType:      normalized.SourceType,
		SourceAccountID: normalized.SourceAccountKey,
		SourceAccount:   normalized.SourceAccount,
		SourceItemID:    normalized.SourceItemKey,
		Title:           normalized.Title,
		Author:          normalized.Author,
		SourceURL:       normalized.SourceURL,
		PublishedAt:     normalized.PublishedAt,
		Content:         normalized.Content,
		ContentFormat:   normalized.ContentFormat,
		Metadata:        metadata,
	}, contentHash, nil
}

func normalizeSourceLicenseScope(scope string) string {
	return strings.ToLower(strings.TrimSpace(scope))
}

func validSourceLicenseScope(scope string) bool {
	switch normalizeSourceLicenseScope(scope) {
	case SourceLicenseScopePersonalUse, SourceLicenseScopeLicensedUse, SourceLicenseScopePublicDomain:
		return true
	default:
		return false
	}
}

func normalizeSourceConnectorMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	normalized := make(map[string]string)
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		normalized[key] = value
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
