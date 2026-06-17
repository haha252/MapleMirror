package downloadtoken

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const Version = "download.v2"
const ReplicationVersion = "replication.v1"

type Signer struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

type Claims struct {
	TokenVersion          string `json:"token_version"`
	AuthorizationID       string `json:"authorization_id"`
	AssetID               string `json:"asset_id"`
	NodeID                string `json:"node_id"`
	ProjectID             string `json:"project_id,omitempty"`
	System                string `json:"system,omitempty"`
	Architecture          string `json:"architecture,omitempty"`
	ClientPrefix          string `json:"client_prefix"`
	ExpiresAt             string `json:"expires_at"`
	MaxBytes              int64  `json:"max_bytes"`
	TrafficLimitBytes     int64  `json:"traffic_limit_bytes,omitempty"`
	RangeConcurrencyLimit int    `json:"range_concurrency_limit"`
	RequestID             string `json:"request_id"`
}

type ReplicationClaims struct {
	TokenVersion string `json:"token_version"`
	AssetID      string `json:"asset_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	ExpiresAt    string `json:"expires_at"`
	RequestID    string `json:"request_id"`
	TaskID       string `json:"task_id"`
}

func NewSignerFromPrivateFile(path string) (Signer, error) {
	key, err := readPrivate(path)
	if err != nil {
		return Signer{}, err
	}
	return Signer{private: key, public: key.Public().(ed25519.PublicKey)}, nil
}

func NewVerifierFromPublicFile(path string) (Signer, error) {
	key, err := readPublic(path)
	if err != nil {
		return Signer{}, err
	}
	return Signer{public: key}, nil
}

func NewVerifierFromPublicPEM(data []byte) (Signer, error) {
	key, err := parsePublicPEM(data)
	if err != nil {
		return Signer{}, err
	}
	return Signer{public: key}, nil
}

func GenerateKeyFiles(privatePath, publicPath string) error {
	if exists(privatePath) && exists(publicPath) {
		return nil
	}
	if exists(privatePath) || exists(publicPath) {
		return errors.New("下载令牌 Ed25519 密钥文件不完整，请同时保留私钥和公钥或先备份后重新生成")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("生成下载令牌 Ed25519 密钥失败：%w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	if err := writePEM(privatePath, "PRIVATE KEY", privateDER, 0o600); err != nil {
		return err
	}
	return writePEM(publicPath, "PUBLIC KEY", publicDER, 0o644)
}

func (s Signer) Sign(claims Claims) (string, error) {
	if len(s.private) == 0 {
		return "", errors.New("下载令牌私钥未加载")
	}
	claims.TokenVersion = Version
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.private, []byte(payload)))
	return payload + "." + sig, nil
}

func (s Signer) SignReplication(claims ReplicationClaims) (string, error) {
	if len(s.private) == 0 {
		return "", errors.New("复制令牌私钥未加载")
	}
	claims.TokenVersion = ReplicationVersion
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.private, []byte(payload)))
	return payload + "." + sig, nil
}

func (s Signer) Signature(message string) string {
	if len(s.private) == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.private, []byte(message)))
}

func (s Signer) Verify(token string) (Claims, error) {
	var out Claims
	if len(s.public) == 0 {
		return out, errors.New("下载令牌公钥未加载")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return out, errors.New("令牌格式不合法")
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !ed25519.Verify(s.public, []byte(parts[0]), got) {
		return out, errors.New("令牌签名不合法")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	if out.TokenVersion != Version {
		return out, errors.New("令牌版本不支持")
	}
	expires, err := time.Parse(time.RFC3339Nano, out.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return out, errors.New("令牌已过期")
	}
	return out, nil
}

func (s Signer) VerifyReplication(token string) (ReplicationClaims, error) {
	var out ReplicationClaims
	if len(s.public) == 0 {
		return out, errors.New("复制令牌公钥未加载")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return out, errors.New("令牌格式不合法")
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !ed25519.Verify(s.public, []byte(parts[0]), got) {
		return out, errors.New("令牌签名不合法")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	if out.TokenVersion != ReplicationVersion {
		return out, errors.New("令牌版本不支持")
	}
	expires, err := time.Parse(time.RFC3339Nano, out.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return out, errors.New("令牌已过期")
	}
	return out, nil
}
