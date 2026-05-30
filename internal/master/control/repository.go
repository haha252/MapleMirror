package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

type Repository struct{ DB *sql.DB }

type PairingCode struct {
	ID        string
	Code      string
	ExpiresAt time.Time
}

type Enrollment struct {
	ID           string
	PublicName   string
	Fingerprint  string
	Status       string
	ExpiresAt    time.Time
	RequestID    string
	Capabilities string
}

func (r Repository) CreatePairing(ctx context.Context, ttl time.Duration, requestID string) (PairingCode, error) {
	now := time.Now().UTC()
	id, err := newID()
	if err != nil {
		return PairingCode{}, err
	}
	code, err := secureToken(32)
	if err != nil {
		return PairingCode{}, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO pairing_codes
		(id, code_hash, code_hint, expires_at, created_by_request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, hashCode(code), codeHint(code), now.Add(ttl).Format(time.RFC3339Nano),
		requestID, now.Format(time.RFC3339Nano))
	if err != nil {
		return PairingCode{}, fmt.Errorf("创建配对码失败：%w", err)
	}
	return PairingCode{ID: id, Code: code, ExpiresAt: now.Add(ttl)}, nil
}

func (r Repository) NodeIDByFingerprint(ctx context.Context, fingerprint string) (string, error) {
	var nodeID string
	err := r.DB.QueryRowContext(ctx, `SELECT node_id FROM node_certificates
		WHERE fingerprint = ? ORDER BY created_at DESC LIMIT 1`, fingerprint).Scan(&nodeID)
	if err != nil {
		return "", err
	}
	return nodeID, nil
}

func (r Repository) RevokePairing(ctx context.Context, id, requestID string) error {
	result, err := r.DB.ExecContext(ctx, `UPDATE pairing_codes
		SET revoked_at = ?, consumed_by_request_id = COALESCE(consumed_by_request_id, ?)
		WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), requestID, id)
	if err != nil {
		return fmt.Errorf("撤销配对码失败：%w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r Repository) CreateEnrollment(ctx context.Context, code, name, csr, fp, caps, requestID string, ttl time.Duration) (Enrollment, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Enrollment{}, err
	}
	defer tx.Rollback()
	pairingID, err := consumePairing(ctx, tx, code, requestID)
	if err != nil {
		return Enrollment{}, err
	}
	id, err := newID()
	if err != nil {
		return Enrollment{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	_, err = tx.ExecContext(ctx, `INSERT INTO node_enrollment_requests
		(id, pairing_code_id, public_name, csr_pem, public_key_fingerprint, status,
		request_id, capabilities_json, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
		id, pairingID, name, csr, fp, requestID, caps,
		expires.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Enrollment{}, fmt.Errorf("创建登记请求失败：%w", err)
	}
	return Enrollment{ID: id, PublicName: name, Fingerprint: fp, Status: "pending",
		ExpiresAt: expires, RequestID: requestID, Capabilities: caps}, tx.Commit()
}

func consumePairing(ctx context.Context, tx *sql.Tx, code, requestID string) (string, error) {
	var id, expires string
	err := tx.QueryRowContext(ctx, `SELECT id, expires_at FROM pairing_codes
		WHERE code_hash = ? AND used_at IS NULL AND revoked_at IS NULL`, hashCode(code)).Scan(&id, &expires)
	if err != nil {
		return "", fmt.Errorf("配对码无效或已使用")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !time.Now().UTC().Before(expiresAt) {
		return "", fmt.Errorf("配对码已过期")
	}
	_, err = tx.ExecContext(ctx, `UPDATE pairing_codes SET used_at = ?,
		consumed_by_request_id = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), requestID, id)
	return id, err
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func codeHint(code string) string {
	if len(code) <= 6 {
		return "***"
	}
	return code[:3] + "..." + code[len(code)-3:]
}

func newID() (string, error) { return secureToken(18) }

func secureToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("生成安全随机值失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
