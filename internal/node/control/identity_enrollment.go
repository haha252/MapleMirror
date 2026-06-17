package control

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func (s IdentityStore) SavePendingKey(keyPEM []byte) error {
	_, err := s.DB.Exec(`INSERT INTO node_enrollment_state
		(id, private_key_pem, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET private_key_pem = excluded.private_key_pem,
		updated_at = excluded.updated_at`,
		string(keyPEM), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s IdentityStore) SavePairingCode(code string) error {
	_, err := s.DB.Exec(`INSERT INTO node_enrollment_state
		(id, pairing_code, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET pairing_code = excluded.pairing_code,
		updated_at = excluded.updated_at`,
		code, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s IdentityStore) PairingCode(fallbackPath string) (string, error) {
	var code string
	err := s.DB.QueryRow(`SELECT COALESCE(pairing_code, '')
		FROM node_enrollment_state WHERE id = 1`).Scan(&code)
	if err == nil && code != "" {
		return code, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	data, readErr := os.ReadFile(fallbackPath)
	if readErr != nil {
		return "", fmt.Errorf("读取配对码失败：%w", readErr)
	}
	code = string(data)
	if saveErr := s.SavePairingCode(code); saveErr != nil {
		return "", saveErr
	}
	return code, nil
}

func (s IdentityStore) EnrollmentID(fallbackPath string) (string, error) {
	var id string
	err := s.DB.QueryRow(`SELECT COALESCE(enrollment_id, '')
		FROM node_enrollment_state WHERE id = 1`).Scan(&id)
	if err == nil && id != "" {
		return id, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	data, readErr := os.ReadFile(fallbackPath)
	if readErr != nil {
		return "", readErr
	}
	var c Credential
	if err := json.Unmarshal(data, &c); err != nil {
		return "", err
	}
	if c.EnrollmentID == "" {
		return "", sql.ErrNoRows
	}
	return c.EnrollmentID, s.SaveEnrollmentID(c.EnrollmentID)
}

func (s IdentityStore) SaveEnrollmentID(id string) error {
	_, err := s.DB.Exec(`INSERT INTO node_enrollment_state
		(id, enrollment_id, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET enrollment_id = excluded.enrollment_id,
		updated_at = excluded.updated_at`,
		id, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s IdentityStore) ClearEnrollmentState() error {
	_, err := s.DB.Exec(`DELETE FROM node_enrollment_state WHERE id = 1`)
	return err
}

func (s IdentityStore) pendingPrivateKey() ([]byte, error) {
	var keyPEM string
	err := s.DB.QueryRow(`SELECT COALESCE(private_key_pem, '')
		FROM node_enrollment_state WHERE id = 1`).Scan(&keyPEM)
	if err == nil && keyPEM != "" {
		return []byte(keyPEM), nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	data, readErr := os.ReadFile(s.KeyFile)
	if readErr != nil {
		return nil, fmt.Errorf("节点私钥缺失，无法保存身份：%w", readErr)
	}
	return data, nil
}
