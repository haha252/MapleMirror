package public

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/master/powtelemetry"
)

func TestVDFSequentialAndTrapdoorVector(t *testing.T) {
	modulus, lambda, base := big.NewInt(77), big.NewInt(30), big.NewInt(5)
	sequential := sequentialVDFSolution(base, modulus, 8)
	fast := fastVDFSolution(base, modulus, lambda, 8)
	if sequential.Cmp(big.NewInt(16)) != 0 || fast.Cmp(sequential) != 0 {
		t.Fatalf("vector mismatch sequential=%s fast=%s", sequential, fast)
	}
}

func TestVDFEncodingAndSolutionBoundaries(t *testing.T) {
	encoded, err := encodeVDFInteger(big.NewInt(16))
	if err != nil || len(encoded) != 512 {
		t.Fatalf("encoding=%q err=%v", encoded, err)
	}
	value, bytes, err := decodeVDFInteger(encoded)
	if err != nil || value.Cmp(big.NewInt(16)) != 0 || len(bytes) != vdfByteSize {
		t.Fatal("round trip failed")
	}
	for _, invalid := range []string{encoded + "=", encoded[:511], "+" + encoded[1:], "/" + encoded[1:]} {
		if _, _, err := decodeVDFInteger(invalid); err == nil {
			t.Fatalf("invalid encoding accepted: %q", invalid[:1])
		}
	}
	digest := sha256.Sum256(bytes)
	challenge := Challenge{ProtocolVersion: "v2", Algorithm: vdfAlgorithm,
		Modulus: big.NewInt(77), SolutionDigest: digest}
	if !validVDFSolution(challenge, encoded) {
		t.Fatal("valid solution rejected")
	}
	outOfRange, _ := encodeVDFInteger(big.NewInt(77))
	if validVDFSolution(challenge, outOfRange) {
		t.Fatal("solution equal to modulus accepted")
	}
	challenge.SolutionDigest[0] ^= 1
	if validVDFSolution(challenge, encoded) {
		t.Fatal("wrong digest accepted")
	}
}

func TestVDFChallengeKeepsOldPublicModulusAfterRotation(t *testing.T) {
	encoded, _ := encodeVDFInteger(big.NewInt(16))
	_, bytes, _ := decodeVDFInteger(encoded)
	challenge := Challenge{ProtocolVersion: "v2", Algorithm: vdfAlgorithm,
		Modulus: big.NewInt(77), SolutionDigest: sha256.Sum256(bytes)}
	service := &vdfService{keys: &vdfKeyManager{currentKey: &vdfKeyMaterial{modulus: big.NewInt(91)}}}
	service.keys.currentKey = &vdfKeyMaterial{modulus: big.NewInt(143)}
	if !validVDFSolution(challenge, encoded) {
		t.Fatal("rotation invalidated an open challenge")
	}
}

func TestVDFChallengeCreationAndSharedGuards(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 1,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100})
	if _, err := store.CreateChallenge(context.Background(), "api_pow", "asset-1",
		"192.0.2.1/32", 4, time.Minute, "req"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateVDFChallenge(context.Background(), "web", "asset-1",
		"192.0.2.1/32", abuseLevelNormal, time.Minute); !errors.Is(err, errChallengeQuota) {
		t.Fatalf("V1/V2 should share bucket: %v", err)
	}

	store = testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100})
	for index := 0; index < 4; index++ {
		if _, err := store.CreateChallenge(context.Background(), "api_pow", "asset-1",
			"198.51.100.1/32", 4, time.Minute, "req"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateVDFChallenge(context.Background(), "web", "asset-1",
		"198.51.100.1/32", abuseLevelNormal, time.Minute); !errors.Is(err, errChallengeOutstanding) {
		t.Fatalf("shared outstanding guard missing: %v", err)
	}
	store = testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 1})
	if _, err := store.CreateChallenge(context.Background(), "api_pow", "asset-1",
		"203.0.113.1/32", 4, time.Minute, "req"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateChallenge(context.Background(), "api_pow", "asset-1",
		"203.0.113.2/32", 4, time.Minute, "req"); !errors.Is(err, errChallengeCapacity) {
		t.Fatalf("global capacity error=%v", err)
	}
}

func TestVDFBusyReleasesOutstandingReservation(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 1, MaxOutstandingTotal: 10})
	store.VDF.semaphore <- struct{}{}
	_, err := store.CreateVDFChallenge(context.Background(), "web", "asset-1",
		"192.0.2.2/32", abuseLevelNormal, time.Minute)
	if !errors.Is(err, errVDFBusy) {
		t.Fatalf("busy error=%v", err)
	}
	<-store.VDF.semaphore
	if _, err := store.CreateVDFChallenge(context.Background(), "web", "asset-1",
		"192.0.2.2/32", abuseLevelNormal, time.Minute); err != nil {
		t.Fatalf("busy path leaked outstanding reservation: %v", err)
	}
}

type failingTelemetryWriter struct{ calls int }

func (w *failingTelemetryWriter) Write(powtelemetry.Record) error {
	w.calls++
	return errors.New("disk full")
}
func (w *failingTelemetryWriter) Close() error { return nil }

type captureTelemetryWriter struct{ record powtelemetry.Record }

func (w *captureTelemetryWriter) Write(record powtelemetry.Record) error {
	w.record = record
	return nil
}
func (w *captureTelemetryWriter) Close() error { return nil }

func TestTelemetrySanitizesClientFieldsAndTrustsChallenge(t *testing.T) {
	writer := &captureTelemetryWriter{}
	server := Server{PowTelemetry: writer}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("User-Agent", strings.Repeat("u", 700))
	challenge := Challenge{ID: "c", AssetID: "a", AssetSizeBytes: 12, SourceKind: "web",
		ProtocolVersion: "v2", Algorithm: vdfAlgorithm, Iterations: 96, Multiplier: 4,
		CreatedAt: time.Now().Add(-time.Second).Format(time.RFC3339Nano)}
	server.writePoWTelemetry(req, challenge, "auth", &powTelemetryInput{SolveElapsedMS: -1,
		Platform: strings.Repeat("设", 200), HardwareConcurrency: 5000, DeviceMemoryGiB: 5000})
	if writer.record.PoWIterations != 96 || writer.record.PoWMultiplier != 4 ||
		writer.record.SolveElapsedMS != nil || writer.record.HardwareConcurrency != nil ||
		writer.record.DeviceMemoryGiB != nil || len(writer.record.UserAgent) != 512 ||
		len([]rune(writer.record.Platform)) > 128 {
		t.Fatalf("sanitized record=%+v", writer.record)
	}
}

func TestTelemetryFailureDoesNotFailV2Authorization(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100})
	challenge, err := store.CreateVDFChallenge(context.Background(), "web", "asset-1",
		"192.0.2.3/32", abuseLevelNormal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	base, _, _ := decodeVDFInteger(challenge.BaseEncoded)
	solution, _ := encodeVDFInteger(sequentialVDFSolution(base, challenge.Modulus, challenge.Iterations))
	writer := &failingTelemetryWriter{}
	server := Server{Store: store, TokenLifetime: testTokenLifetime(time.Minute), PowTelemetry: writer}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v2/web/authorizations", nil)
	req.RemoteAddr = "192.0.2.3:1234"
	rec := httptest.NewRecorder()
	delivered := deliverNextAuthorization(t, db, "node-1")
	server.authorize(rec, req, challengeSubmit{SourceKind: "web", ProtocolVersion: "v2",
		Algorithm: vdfAlgorithm, ChallengeID: challenge.ID, AssetID: challenge.AssetID,
		Solution: solution, Telemetry: &powTelemetryInput{SolveElapsedMS: 10}})
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated || writer.calls != 1 {
		t.Fatalf("telemetry failure changed authorization: status=%d calls=%d body=%s", rec.Code, writer.calls, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "disk full") {
		t.Fatal("telemetry error leaked to response")
	}
}

func testVDFStore(t *testing.T, db *sql.DB, limits config.ChallengeLimits) Store {
	t.Helper()
	modulus, lambda := big.NewInt(3233), big.NewInt(780)
	encoded, _ := encodeVDFInteger(modulus)
	id, _ := vdfModulusID(modulus)
	keys := &vdfKeyManager{currentKey: &vdfKeyMaterial{modulus: modulus, lambda: lambda,
		encoded: encoded, modulusID: id, createdAt: time.Now().UTC()}}
	return Store{DB: db, Challenges: newChallengeMemory(limits), VDF: &vdfService{
		keys: keys, policy: vdfPolicy{tiers: []vdfSizeTier{{minBytes: 0, iterations: 8}},
			elevatedMultiplier: 2, severeMultiplier: 4, maxIterations: 100}, semaphore: make(chan struct{}, 1)}}
}
