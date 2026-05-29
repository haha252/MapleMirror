package downloadtoken

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEd25519TokenRoundTripAndTamperReject(t *testing.T) {
	dir := t.TempDir()
	privatePath := dir + "/token.key"
	publicPath := dir + "/token.pub"
	if err := GenerateKeyFiles(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	signer, err := NewSignerFromPrivateFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifierFromPublicFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(Claims{AuthorizationID: "auth", AssetID: "asset",
		NodeID: "node", ClientPrefix: "192.0.2.1/32",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Verify(token)
	if err != nil || claims.TokenVersion != Version {
		t.Fatalf("Ed25519 令牌验证失败：claims=%+v err=%v", claims, err)
	}
	if _, err := verifier.Verify(token + "x"); err == nil {
		t.Fatal("篡改后的令牌应被拒绝")
	}
	if _, err := verifier.Sign(Claims{}); err == nil {
		t.Fatal("仅公钥加载器不得签发令牌")
	}
}

func TestEd25519RejectsOldTokenVersion(t *testing.T) {
	dir := t.TempDir()
	privatePath := dir + "/token.key"
	publicPath := dir + "/token.pub"
	if err := GenerateKeyFiles(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	signer, _ := NewSignerFromPrivateFile(privatePath)
	verifier, _ := NewVerifierFromPublicFile(publicPath)
	body, err := json.Marshal(Claims{TokenVersion: "download.v1",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	token := payload + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(signer.private, []byte(payload)))
	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("旧 download.v1 令牌应被拒绝")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatal("令牌格式异常")
	}
	if _, err := verifier.Verify(parts[0] + ".bad"); err == nil {
		t.Fatal("错误签名应被拒绝")
	}
}
