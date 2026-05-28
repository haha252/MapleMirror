package public

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

type TokenSigner struct {
	key []byte
}

type DownloadClaims struct {
	TokenVersion          string `json:"token_version"`
	AuthorizationID       string `json:"authorization_id"`
	AssetID               string `json:"asset_id"`
	NodeID                string `json:"node_id"`
	ClientPrefix          string `json:"client_prefix"`
	ExpiresAt             string `json:"expires_at"`
	MaxBytes              int64  `json:"max_bytes"`
	RangeConcurrencyLimit int    `json:"range_concurrency_limit"`
	RequestID             string `json:"request_id"`
}

func NewTokenSigner(db *sql.DB, keyFile string) (TokenSigner, error) {
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return TokenSigner{}, err
		}
		key := []byte(strings.TrimSpace(string(data)))
		if len(key) < 32 {
			return TokenSigner{}, errors.New("下载令牌签名密钥长度不足")
		}
		return TokenSigner{key: key}, nil
	}
	key, err := persistedKey(db)
	if err != nil {
		return TokenSigner{}, err
	}
	return TokenSigner{key: key}, nil
}

func persistedKey(db *sql.DB) ([]byte, error) {
	const name = "download_token_signing_key"
	var value string
	err := db.QueryRow(`SELECT value FROM runtime_kv WHERE key = ?`, name).Scan(&value)
	if err == nil {
		return base64.RawStdEncoding.DecodeString(value)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	value = base64.RawStdEncoding.EncodeToString(key)
	_, err = db.Exec(`INSERT INTO runtime_kv(key, value, updated_at) VALUES (?, ?, ?)`,
		name, value, nowText())
	return key, err
}

func (s TokenSigner) Sign(claims DownloadClaims) (string, error) {
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func (s TokenSigner) Verify(token string) (DownloadClaims, error) {
	var out DownloadClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return out, errors.New("令牌格式不合法")
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(parts[0]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, want) {
		return out, errors.New("令牌签名不合法")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	if out.TokenVersion != "download.v1" {
		return out, errors.New("令牌版本不支持")
	}
	expires, err := time.Parse(time.RFC3339Nano, out.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return out, errors.New("令牌已过期")
	}
	return out, nil
}
