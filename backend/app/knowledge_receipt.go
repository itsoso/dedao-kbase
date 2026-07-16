package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *KnowledgeCatalogStore) SaveDeliveryReceipt(input DeliveryReceipt, now func() time.Time) (DeliveryReceipt, error) {
	if s == nil || s.db == nil {
		return DeliveryReceipt{}, fmt.Errorf("knowledge catalog store is required")
	}
	if now == nil {
		now = time.Now
	}
	if input.SchemaVersion == "" {
		input.SchemaVersion = DeliveryReceiptSchemaVersion
	}
	if input.ReceivedAt == "" {
		input.ReceivedAt = now().UTC().Format(time.RFC3339Nano)
	}
	payloadHash, err := deliveryReceiptPayloadHash(input)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if input.ReceiptID == "" {
		input.ReceiptID = stableKnowledgeID("receipt", input.Consumer, input.ReleaseID, input.IdempotencyKey)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return DeliveryReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing DeliveryReceipt
	var existingHash string
	row := tx.QueryRow(`SELECT receipt_id, schema_version, consumer, release_id, idempotency_key, disposition, imported_fingerprint, received_at, payload_hash
		FROM knowledge_delivery_receipts WHERE idempotency_key = ?`, input.IdempotencyKey)
	if err := row.Scan(&existing.ReceiptID, &existing.SchemaVersion, &existing.Consumer, &existing.ReleaseID, &existing.IdempotencyKey, &existing.Disposition, &existing.ImportedFingerprint, &existing.ReceivedAt, &existingHash); err != nil && err != sql.ErrNoRows {
		return DeliveryReceipt{}, err
	} else if err == nil {
		if existingHash != payloadHash {
			return DeliveryReceipt{}, fmt.Errorf("idempotency payload conflict")
		}
		if err := tx.Commit(); err != nil {
			return DeliveryReceipt{}, err
		}
		return existing, nil
	}
	if _, err := tx.Exec(`INSERT INTO knowledge_delivery_receipts (
		receipt_id, schema_version, consumer, release_id, idempotency_key, disposition, imported_fingerprint, received_at, payload_hash
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ReceiptID, input.SchemaVersion, input.Consumer, input.ReleaseID, input.IdempotencyKey, input.Disposition, input.ImportedFingerprint, input.ReceivedAt, payloadHash,
	); err != nil {
		return DeliveryReceipt{}, err
	}
	return input, tx.Commit()
}

func deliveryReceiptPayloadHash(receipt DeliveryReceipt) (string, error) {
	seed := struct {
		SchemaVersion       string `json:"schema_version"`
		Consumer            string `json:"consumer"`
		ReleaseID           string `json:"release_id"`
		IdempotencyKey      string `json:"idempotency_key"`
		Disposition         string `json:"disposition"`
		ImportedFingerprint string `json:"imported_fingerprint,omitempty"`
	}{
		SchemaVersion:       strings.TrimSpace(receipt.SchemaVersion),
		Consumer:            strings.TrimSpace(receipt.Consumer),
		ReleaseID:           strings.TrimSpace(receipt.ReleaseID),
		IdempotencyKey:      strings.TrimSpace(receipt.IdempotencyKey),
		Disposition:         strings.TrimSpace(receipt.Disposition),
		ImportedFingerprint: strings.TrimSpace(receipt.ImportedFingerprint),
	}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
