package public

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mirror-server/internal/master/powtelemetry"
)

type PoWTelemetryWriter interface {
	Write(powtelemetry.Record) error
	Close() error
}

type telemetryWarningLimiter struct {
	mu   sync.Mutex
	last time.Time
}

func (s *Server) SetPoWTelemetry(writer PoWTelemetryWriter) {
	s.PowTelemetry = writer
	s.TelemetryWarnings = &telemetryWarningLimiter{}
}

func (s Server) writePoWTelemetry(r *http.Request, challenge Challenge,
	authorizationID string, input *powTelemetryInput) {
	if s.PowTelemetry == nil || challenge.SourceKind != "web" || challenge.ProtocolVersion != "v2" {
		return
	}
	now := time.Now().UTC()
	record := powtelemetry.Record{
		RequestID: requestID(r), AuthorizationID: authorizationID, ChallengeID: challenge.ID,
		AssetID: challenge.AssetID, AssetSizeBytes: challenge.AssetSizeBytes,
		SourceKind: challenge.SourceKind, ProtocolVersion: challenge.ProtocolVersion,
		PoWAlgorithm: challenge.Algorithm, PoWIterations: challenge.Iterations,
		PoWMultiplier: challenge.Multiplier, ChallengeAgeMS: challengeAgeMilliseconds(challenge, now),
		UserAgent: truncateTelemetryString(r.UserAgent(), 512),
	}
	if input != nil {
		if input.SolveElapsedMS > 0 && input.SolveElapsedMS <= int64((24*time.Hour)/time.Millisecond) {
			value := input.SolveElapsedMS
			record.SolveElapsedMS = &value
		}
		record.Platform = truncateTelemetryString(input.Platform, 128)
		if input.HardwareConcurrency > 0 && input.HardwareConcurrency <= 1024 {
			value := input.HardwareConcurrency
			record.HardwareConcurrency = &value
		}
		if input.DeviceMemoryGiB > 0 && input.DeviceMemoryGiB <= 1024 {
			value := input.DeviceMemoryGiB
			record.DeviceMemoryGiB = &value
		}
	}
	if err := s.PowTelemetry.Write(record); err != nil {
		s.warnPoWTelemetry(r.Context(), err)
	}
}

func truncateTelemetryString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s Server) warnPoWTelemetry(ctx context.Context, err error) {
	if s.Logger == nil {
		return
	}
	limiter := s.TelemetryWarnings
	if limiter == nil {
		s.Logger.Warn(ctx, "PoW 遥测写入失败，不影响授权", slog.String("error", err.Error()))
		return
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := time.Now()
	if !limiter.last.IsZero() && now.Sub(limiter.last) < time.Minute {
		return
	}
	limiter.last = now
	s.Logger.Warn(ctx, "PoW 遥测写入失败，不影响授权", slog.String("error", err.Error()))
}
