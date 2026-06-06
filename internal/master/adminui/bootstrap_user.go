package adminui

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"os"
)

const credentialAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

var bootstrapCredentialConsole io.Writer = os.Stdout

func bootstrapUserFile(path, bootstrapEnv string) error {
	if bootstrapEnv != "" {
		password := os.Getenv(bootstrapEnv)
		if password != "" {
			return writeBootstrapUser(path, "admin", password)
		}
	}
	return writeGeneratedBootstrapUser(path)
}

func writeGeneratedBootstrapUser(path string) error {
	username, err := randomCredential(8)
	if err != nil {
		return err
	}
	password, err := randomCredential(20)
	if err != nil {
		return err
	}
	if err := writeBootstrapUser(path, username, password); err != nil {
		return err
	}
	printGeneratedBootstrapUser(path, username, password)
	return nil
}

func randomCredential(length int) (string, error) {
	out := make([]byte, length)
	max := big.NewInt(int64(len(credentialAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = credentialAlphabet[n.Int64()]
	}
	return string(out), nil
}

func printGeneratedBootstrapUser(path, username, password string) {
	_, _ = fmt.Fprintf(bootstrapCredentialConsole, "管理面板未找到管理员用户，已自动生成初始账号。\n")
	_, _ = fmt.Fprintf(bootstrapCredentialConsole, "用户文件：%s\n", path)
	_, _ = fmt.Fprintf(bootstrapCredentialConsole, "用户名：%s\n", username)
	_, _ = fmt.Fprintf(bootstrapCredentialConsole, "密码：%s\n", password)
	_, _ = fmt.Fprintf(bootstrapCredentialConsole, "请立即保存该密码；明文只会在本次启动控制台输出一次。\n")
}
