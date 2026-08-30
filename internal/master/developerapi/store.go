package developerapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) Info(ctx context.Context, projectID string) (APIInfo, error) {
	info := APIInfo{
		ProjectID: projectID, Endpoint: s.Endpoint(projectID),
		DailyLimit: s.limit(),
	}
	var last sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT token_prefix, created_at, rotated_at, last_used_at
		FROM project_developer_tokens WHERE project_id = ?`, projectID).
		Scan(&info.TokenPrefix, &info.CreatedAt, &info.RotatedAt, &last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return APIInfo{}, err
	}
	info.TokenConfigured = err == nil
	if last.Valid {
		info.LastUsedAt = last.String
	}
	usage, err := s.Usage(ctx, projectID, time.Now())
	if err != nil {
		return APIInfo{}, err
	}
	info.UsedToday = usage.Used
	info.RemainingToday = usage.Remaining
	info.ResetAt = usage.ResetAt.Format(time.RFC3339)
	return info, nil
}

func (s *Store) RotateToken(ctx context.Context, projectID string) (TokenResult, error) {
	token, err := newToken()
	if err != nil {
		return TokenResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO project_developer_tokens
		(project_id, token_hash, token_prefix, created_at, rotated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		token_hash = excluded.token_hash,
		token_prefix = excluded.token_prefix,
		rotated_at = excluded.rotated_at,
		last_used_at = NULL`,
		projectID, tokenHash(token), tokenPrefix(token), now, now)
	if err != nil {
		return TokenResult{}, err
	}
	info, err := s.Info(ctx, projectID)
	return TokenResult{APIInfo: info, Token: token}, err
}

func (s *Store) Authenticate(ctx context.Context, token string) (string, string, error) {
	if !strings.HasPrefix(token, "mmdev_") || len(token) < 30 {
		return "", "", ErrUnauthorized
	}
	var projectID, prefix string
	err := s.DB.QueryRowContext(ctx, `SELECT project_id, token_prefix
		FROM project_developer_tokens WHERE token_hash = ?`, tokenHash(token)).
		Scan(&projectID, &prefix)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrUnauthorized
	}
	return projectID, prefix, err
}

func (s *Store) RevokeProject(ctx context.Context, projectID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM project_developer_tokens WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM project_developer_api_daily_usage WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RevokeMissing(ctx context.Context, keep map[string]struct{}) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id FROM project_developer_tokens`)
	if err != nil {
		return err
	}
	var remove []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if _, ok := keep[id]; !ok {
			remove = append(remove, id)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range remove {
		if err := s.RevokeProject(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
