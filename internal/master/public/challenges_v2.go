package public

import (
	"net/http"
)

type powTelemetryInput struct {
	SolveElapsedMS      int64   `json:"solve_elapsed_ms"`
	Platform            string  `json:"platform"`
	HardwareConcurrency int     `json:"hardware_concurrency"`
	DeviceMemoryGiB     float64 `json:"device_memory_gib"`
}

func (s Server) webV2Challenge(w http.ResponseWriter, r *http.Request) {
	s.v2Challenge(w, r, "web")
}

func (s Server) apiV2Challenge(w http.ResponseWriter, r *http.Request) {
	s.v2Challenge(w, r, "api")
}

func (s Server) v2Challenge(w http.ResponseWriter, r *http.Request, source string) {
	if s.rejectBlockedDownload(w, r, "", source+"_v2_challenge") {
		return
	}
	assetID, prefix, ok := s.challengeRequest(w, r)
	if !ok {
		return
	}
	level, _, rejected := s.challengeAbuse(w, r, abuseScope(source, "v2"), prefix)
	if rejected {
		return
	}
	challenge, err := s.Store.CreateVDFChallenge(r.Context(), source, assetID, prefix, level, s.VDFTTL)
	if err != nil {
		s.writeChallengeCreateError(w, r, err)
		return
	}
	writeOK(w, r, http.StatusCreated, "挑战已创建", map[string]any{
		"challenge_id": challenge.ID, "asset_id": challenge.AssetID,
		"algorithm": challenge.Algorithm, "modulus_id": challenge.ModulusID,
		"modulus": challenge.ModulusEncoded, "base": challenge.BaseEncoded,
		"iterations": challenge.Iterations, "encoding": vdfEncoding, "expires_at": challenge.ExpiresAt,
	})
}

func (s Server) webV2Authorize(w http.ResponseWriter, r *http.Request) {
	s.v2Authorize(w, r, "web")
}

func (s Server) apiV2Authorize(w http.ResponseWriter, r *http.Request) {
	s.v2Authorize(w, r, "api")
}

func (s Server) v2Authorize(w http.ResponseWriter, r *http.Request, source string) {
	if s.rejectBlockedDownload(w, r, "", source+"_v2_authorization") {
		return
	}
	var in struct {
		ChallengeID string             `json:"challenge_id"`
		AssetID     string             `json:"asset_id"`
		Solution    string             `json:"solution"`
		Telemetry   *powTelemetryInput `json:"telemetry"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	s.authorize(w, r, challengeSubmit{SourceKind: source, ProtocolVersion: "v2",
		Algorithm: vdfAlgorithm, ChallengeID: in.ChallengeID, AssetID: in.AssetID,
		Solution: in.Solution, Telemetry: in.Telemetry})
}
