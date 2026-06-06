package adminui

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type userFile struct {
	Users []userRecord `yaml:"users"`
}

type userRecord struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
	Role         string `yaml:"role"`
}

func loadUsers(path, bootstrapEnv string) (map[string]userRecord, error) {
	var file userFile
	if err := readYAMLFile(path, &file); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := bootstrapUserFile(path, bootstrapEnv); err != nil {
			return nil, err
		}
		if err := readYAMLFile(path, &file); err != nil {
			return nil, err
		}
	}
	users := map[string]userRecord{}
	for _, item := range file.Users {
		if item.Username == "" || item.PasswordHash == "" {
			return nil, errors.New("管理面板用户文件包含空用户名或密码哈希")
		}
		if _, exists := users[item.Username]; exists {
			return nil, errors.New("管理面板用户文件包含重复用户名")
		}
		users[item.Username] = item
	}
	if len(users) == 0 {
		if err := writeGeneratedBootstrapUser(path); err != nil {
			return nil, err
		}
		return loadUsers(path, "")
	}
	return users, nil
}

func writeBootstrapUser(path, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirName(path), 0o700); err != nil {
		return err
	}
	body := "users:\n  - username: \"" + username + "\"\n    password_hash: \"" + hash + "\"\n    role: \"owner\"\n"
	return os.WriteFile(path, []byte(body), 0o600)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt, 210000, sha256.Size)
	return "pbkdf2-sha256$210000$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	var out []byte
	var blockIndex uint32 = 1
	for len(out) < keyLen {
		u := prf(password, appendBlockIndex(salt, blockIndex))
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			u = prf(password, u)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
		blockIndex++
	}
	return out[:keyLen]
}

func prf(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func appendBlockIndex(salt []byte, index uint32) []byte {
	out := make([]byte, len(salt)+4)
	copy(out, salt)
	out[len(salt)] = byte(index >> 24)
	out[len(salt)+1] = byte(index >> 16)
	out[len(salt)+2] = byte(index >> 8)
	out[len(salt)+3] = byte(index)
	return out
}

func loadOrCreateSecret(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		secret, decErr := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decErr != nil || len(secret) < 32 {
			return nil, errors.New("管理面板会话密钥文件内容无效")
		}
		return secret, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("生成管理面板会话密钥失败：%w", err)
	}
	if err := os.MkdirAll(dirName(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.RawStdEncoding.EncodeToString(secret)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

func secureToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
