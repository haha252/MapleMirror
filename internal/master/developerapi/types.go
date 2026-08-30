package developerapi

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

const DefaultDailyLimit = 100

var (
	ErrUnauthorized = errors.New("developer API token 无效")
	ErrRateLimited  = errors.New("developer API 今日额度已用完")
)

type Store struct {
	DB         *sql.DB
	Location   *time.Location
	PublicURL  string
	DailyLimit int
}

type APIInfo struct {
	ProjectID       string `json:"project_id"`
	Endpoint        string `json:"endpoint"`
	TokenConfigured bool   `json:"token_configured"`
	TokenPrefix     string `json:"token_prefix,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	RotatedAt       string `json:"rotated_at,omitempty"`
	LastUsedAt      string `json:"last_used_at,omitempty"`
	DailyLimit      int    `json:"daily_limit"`
	UsedToday       int    `json:"used_today"`
	RemainingToday  int    `json:"remaining_today"`
	ResetAt         string `json:"reset_at"`
}

type TokenResult struct {
	APIInfo
	Token string `json:"token"`
}

type Usage struct {
	Limit     int
	Used      int
	Remaining int
	ResetAt   time.Time
}

func NewStore(db *sql.DB, location *time.Location, publicURL string) *Store {
	if location == nil {
		location = time.UTC
	}
	return &Store{
		DB: db, Location: location,
		PublicURL:  strings.TrimRight(strings.TrimSpace(publicURL), "/"),
		DailyLimit: DefaultDailyLimit,
	}
}

func (s *Store) Endpoint(projectID string) string {
	path := "/api/developer/v1/projects/" + projectID + "/sync"
	if s.PublicURL == "" {
		return path
	}
	return s.PublicURL + path
}

func (s *Store) limit() int {
	if s.DailyLimit <= 0 {
		return DefaultDailyLimit
	}
	return s.DailyLimit
}
