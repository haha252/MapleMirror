package requestid

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type contextKey string

const (
	requestKey contextKey = "request_id"
	parentKey  contextKey = "parent_request_id"
)

func New() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("生成请求 ID 失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func WithValues(ctx context.Context, id, parent string) context.Context {
	ctx = context.WithValue(ctx, requestKey, id)
	if parent != "" {
		ctx = context.WithValue(ctx, parentKey, parent)
	}
	return ctx
}

func FromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestKey).(string)
	return value
}

func ParentFromContext(ctx context.Context) string {
	value, _ := ctx.Value(parentKey).(string)
	return value
}
