package downloadtoken

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ReplicationClaims struct {
	TokenVersion string `json:"token_version"`
	AssetID      string `json:"asset_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	ExpiresAt    string `json:"expires_at"`
	RequestID    string `json:"request_id"`
	TaskID       string `json:"task_id"`
	RangeStart   int64  `json:"range_start,omitempty"`
	RangeEnd     int64  `json:"range_end,omitempty"`
}

type SwarmClaims struct {
	TokenVersion string `json:"token_version"`
	AssetID      string `json:"asset_id"`
	ManifestID   string `json:"manifest_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	ExpiresAt    string `json:"expires_at"`
	Scope        string `json:"scope"`
	AssetSize    int64  `json:"asset_size"`
	PieceSize    int64  `json:"piece_size"`
	PieceCount   int    `json:"piece_count"`
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

func (s Signer) SignSwarm(claims SwarmClaims) (string, error) {
	if len(s.private) == 0 {
		return "", errors.New("Swarm 令牌私钥未加载")
	}
	claims.TokenVersion = SwarmVersion
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.private, []byte(payload)))
	return payload + "." + sig, nil
}

func (s Signer) VerifySwarm(token string) (SwarmClaims, error) {
	var out SwarmClaims
	if len(s.public) == 0 {
		return out, errors.New("Swarm 令牌公钥未加载")
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
	if out.TokenVersion != SwarmVersion || out.Scope != "swarm_piece_read" {
		return out, errors.New("令牌版本或 scope 不支持")
	}
	expires, err := time.Parse(time.RFC3339Nano, out.ExpiresAt)
	if err != nil || !time.Now().UTC().Before(expires) {
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
