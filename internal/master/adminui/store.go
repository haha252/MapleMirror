package adminui

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

type loginStore struct {
	db          *sql.DB
	secret      []byte
	window      time.Duration
	limit       int
	banDuration time.Duration
	sessionTTL  time.Duration
	memory      *loginMemory
}

type blockStatus struct {
	Blocked   bool
	MaskedIP  string
	ExpiresAt string
}

func (s loginStore) ipKey(ip string) string {
	return tokenHash("admin-ip:" + string(s.secret) + ":" + ip)
}

func (s loginStore) sessionKey(token string) string {
	return tokenHash("admin-session:" + string(s.secret) + ":" + token)
}

func (s loginStore) blocked(ctx context.Context, ip string) (blockStatus, error) {
	now := nowText()
	key := s.ipKey(ip)
	if s.memory != nil {
		if status, ok := s.memory.blocked(key, now); ok {
			return status, nil
		}
	}
	var masked, expires string
	err := s.db.QueryRowContext(ctx, `SELECT masked_ip, expires_at
		FROM admin_ip_blocks WHERE ip_key = ? AND expires_at > ?`, key, now).
		Scan(&masked, &expires)
	if err == sql.ErrNoRows {
		return blockStatus{}, nil
	}
	if err != nil {
		return blockStatus{}, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE admin_ip_blocks SET
		attempts_after_block = attempts_after_block + 1,
		last_attempt_at = ?, updated_at = ? WHERE ip_key = ?`, now, now, key)
	return blockStatus{Blocked: true, MaskedIP: masked, ExpiresAt: expires}, nil
}

func (s loginStore) recordFailure(ctx context.Context, ip string) error {
	if s.memory != nil {
		s.memory.recordFailure(s.ipKey(ip), maskIP(ip), ip, s.window, s.limit, s.banDuration)
		return nil
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	key := s.ipKey(ip)
	masked := maskIP(ip)
	var count int
	var windowStarted string
	err := s.db.QueryRowContext(ctx, `SELECT failed_count, window_started_at
		FROM admin_login_failures WHERE ip_key = ?`, key).Scan(&count, &windowStarted)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	reset := err == sql.ErrNoRows
	if !reset {
		started, parseErr := time.Parse(time.RFC3339Nano, windowStarted)
		reset = parseErr != nil || now.Sub(started) > s.window
	}
	if reset {
		count = 1
		windowStarted = nowText
	} else {
		count++
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_login_failures
		(ip_key, masked_ip, failed_count, window_started_at, last_failed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(ip_key) DO UPDATE SET masked_ip = excluded.masked_ip,
		failed_count = excluded.failed_count, window_started_at = excluded.window_started_at,
		last_failed_at = excluded.last_failed_at, updated_at = excluded.updated_at`,
		key, masked, count, windowStarted, nowText, nowText)
	if err != nil || count < s.limit {
		return err
	}
	expires := now.Add(s.banDuration).Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_ip_blocks
		(ip_key, masked_ip, display_ip, reason, blocked_at, expires_at, last_attempt_at, updated_at)
		VALUES (?, ?, ?, 'admin_login_failed', ?, ?, ?, ?)
		ON CONFLICT(ip_key) DO UPDATE SET reason = excluded.reason,
		masked_ip = excluded.masked_ip, display_ip = excluded.display_ip,
		blocked_at = excluded.blocked_at, expires_at = excluded.expires_at,
		last_attempt_at = excluded.last_attempt_at, updated_at = excluded.updated_at`,
		key, masked, ip, nowText, expires, nowText, nowText)
	return err
}

func (s loginStore) clearFailures(ctx context.Context, ip string) {
	if s.memory != nil {
		s.memory.clearFailures(s.ipKey(ip))
		return
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM admin_login_failures WHERE ip_key = ?`, s.ipKey(ip))
}

func (s loginStore) createSession(ctx context.Context, username, ip string) (string, string, error) {
	token, err := secureToken()
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	expires := now.Add(s.sessionTTL).Format(time.RFC3339Nano)
	if s.memory != nil {
		s.memory.createSession(s.sessionKey(token), username, s.ipKey(ip), expires)
		return token, expires, nil
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_web_sessions
		(id, username, ip_key, created_at, expires_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)`, s.sessionKey(token), username, s.ipKey(ip),
		now.Format(time.RFC3339Nano), expires, now.Format(time.RFC3339Nano))
	return token, expires, err
}

func (s loginStore) verifySession(ctx context.Context, token, ip string) (string, bool, error) {
	if token == "" {
		return "", false, nil
	}
	now := nowText()
	if s.memory != nil {
		return s.memory.verifySession(s.sessionKey(token), s.ipKey(ip), now)
	}
	var username, ipKey string
	err := s.db.QueryRowContext(ctx, `SELECT username, ip_key FROM admin_web_sessions
		WHERE id = ? AND expires_at > ?`, s.sessionKey(token), now).Scan(&username, &ipKey)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if ipKey != s.ipKey(ip) {
		return "", false, errors.New("管理面板会话来源不匹配")
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE admin_web_sessions SET last_seen_at = ? WHERE id = ?`,
		now, s.sessionKey(token))
	return username, true, nil
}

func (s loginStore) deleteSession(ctx context.Context, token string) {
	if token != "" {
		if s.memory != nil {
			s.memory.deleteSession(s.sessionKey(token))
			return
		}
		_, _ = s.db.ExecContext(ctx, `DELETE FROM admin_web_sessions WHERE id = ?`, s.sessionKey(token))
	}
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func maskIP(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return strings.Join([]string{
			byteString(v4[0]), byteString(v4[1]), byteString(v4[2]), "*",
		}, ".")
	}
	return ip.String()[:min(len(ip.String()), 10)] + "*"
}

func byteString(value byte) string {
	return strconv.Itoa(int(value))
}
