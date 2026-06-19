package adminui

import (
	"database/sql"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type clientLimitScopeValue struct {
	Kind string
	Key  string
}

func clientLimitScope(key string) (clientLimitScopeValue, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(key))
	if err != nil {
		return clientLimitScopeValue{}, sql.ErrNoRows
	}
	ones := prefix.Bits()
	addr := prefix.Addr()
	if addr.Is4() {
		switch ones {
		case 32:
			return clientLimitScopeValue{Kind: "ipv4_32", Key: prefix.Masked().String()}, nil
		case 24:
			return clientLimitScopeValue{Kind: "ipv4_24", Key: prefix.Masked().String()}, nil
		}
		return clientLimitScopeValue{}, sql.ErrNoRows
	}
	if ones == 128 {
		return clientLimitScopeValue{Kind: "ipv6_128", Key: prefix.Masked().String()}, nil
	}
	return clientLimitScopeValue{}, sql.ErrNoRows
}

func (s *Server) clearClientLimitScope(r *http.Request, tx *sql.Tx, scope clientLimitScopeValue) error {
	day := statDay(time.Now().UTC(), s.timeLocation)
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM quota_buckets
		WHERE scope_kind = ? AND scope_key = ?`, scope.Kind, scope.Key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM daily_traffic_stats
		WHERE stat_day = ? AND scope_kind = ? AND scope_key = ?`,
		day, scope.Kind, scope.Key); err != nil {
		return err
	}
	return releaseClientLimitReservations(r, tx, day, scope)
}

func releaseClientLimitReservations(r *http.Request, tx *sql.Tx, day string,
	scope clientLimitScopeValue) error {
	_, err := tx.ExecContext(r.Context(), `UPDATE traffic_reservations
		SET status = 'released'
		WHERE scope_day = ? AND status IN ('active', 'reserved')
		AND settled_bytes = 0
		AND ((address_scope_kind = ? AND address_scope_key = ?)
		OR (network_scope_kind = ? AND network_scope_key = ?))`,
		day, scope.Kind, scope.Key, scope.Kind, scope.Key)
	return err
}

func statDay(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02")
}
