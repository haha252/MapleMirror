package public

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/geoip"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/statbuffer"
)

type Server struct {
	Store                        Store
	Signer                       downloadtoken.Signer
	VDFTTL                       time.Duration
	APITTL                       time.Duration
	APIV1Enabled                 *bool
	TokenLifetime                TokenLifetime
	TrustedCIDRs                 []string
	Version                      string
	Logger                       *logging.Logger
	WebAssets                    *webAssets
	ProjectAssets                map[string]projectAssetConfig
	ProjectsPath                 string
	NoticesPath                  string
	Notices                      []config.PublicNotice
	NoticeStore                  *noticeStore
	PageViews                    *pageViewTracker
	Blocklist                    *blocklistPolicy
	ClientBlocks                 *clientBlockManager
	AbuseTracker                 *abuseTracker
	BlocklistExport              *blocklistExportCache
	ResourceLimiter              *publicResourceLimiter
	StatsCache                   *statsCache
	WebVerifications             *webVerificationTokenStore
	CatalogIndex                 *catalogIndex
	CatalogCache                 *catalogResultCache
	changelog                    *changelogStore
	VDFKeys                      *vdfKeyManager
	vdfPolicyReloader            *vdfPolicyReloader
	PowTelemetry                 PoWTelemetryWriter
	TelemetryWarnings            *telemetryWarningLimiter
	CatalogBatchRows             int
	CatalogPrefetchRemainingRows *int
}
type TokenLifetime struct {
	FirstConnectionTimeout time.Duration
	IdleTimeout            time.Duration
	MaxDuration            time.Duration
}
func New(db *sql.DB, signer downloadtoken.Signer, vdfTTL, apiTTL time.Duration,
	tokenLifetime TokenLifetime,
	powSizeTiers []config.PoWSizeTier, vdfSizeTiers []config.VDFSizeTier, vdfConfig config.VDF,
	apiV1Enabled bool, quota config.Quota, loc *time.Location, trusted []string,
	projects config.Projects, filters config.Filters,
	projectsPath, filtersPath, noticesPath, changelogPath string, notices []config.PublicNotice,
	vdfConfigPath string,
	runtime *mastercontrol.RuntimeStore, version string,
	regionClassifier geoip.Classifier,
	logger *logging.Logger, publicProbeNetworkFailures int,
	catalogBatchRows, catalogPrefetchRemainingRows int,
	archive *accountingarchive.Writer,
	statsBuffer *statbuffer.Buffer) (Server, error) {
	powDifficulty, err := newPoWSizePolicy(
		powSizeTiers, quota.AbuseControl.Challenge.MaxBits)
	if err != nil {
		return Server{}, err
	}
	vdfPolicy, err := newVDFPolicy(vdfSizeTiers, vdfConfig)
	if err != nil {
		return Server{}, err
	}
	rotation, err := time.ParseDuration(vdfConfig.KeyRotationInterval)
	if err != nil {
		return Server{}, err
	}
	keys, err := newVDFKeyManager(rotation, logger, nil)
	if err != nil {
		return Server{}, err
	}
	assets, err := loadDefaultWebAssets()
	if err != nil {
		keys.Close()
		return Server{}, err
	}
	challenges := newChallengeMemory(quota.ChallengeLimits)
	challenges.startCleanup(minDuration(vdfTTL, apiTTL, time.Minute))
	vdf := &vdfService{keys: keys, policy: vdfPolicy,
		semaphore: make(chan struct{}, vdfConfig.MaxParallelCreations)}
	blocklist := newBlocklistPolicy(quota, logger)
	blocklist.start()
	abuseTracker := newAbuseTracker(quota.AbuseControl)
	abuseTracker.start()
	clientBlocks := newClientBlockManager(db, quota.AbuseControl, logger)
	clientBlocks.start()
	webVerifications := newWebVerificationTokenStore(webVerificationTokenCapacity, webVerificationTokenTTL)
	webVerifications.startCleanup()
	catalogCache := newCatalogResultCache(filters.CacheBytes)
	catalogIndex := newCatalogIndex(projects, filters, projectsPath, filtersPath, catalogCache, logger)
	server := Server{
		Store: Store{DB: db, Quota: newQuotaPolicy(quota), Location: loc,
			Challenges: challenges, PoWDifficulty: powDifficulty,
			VDF:        vdf,
			MaxBytes:   newMaxBytesPolicy(quota),
			RangeLimit: quota.RangeConcurrencyLimit, Runtime: runtime,
			RegionClassifier:           regionClassifier,
			PublicProbeNetworkFailures: publicProbeNetworkFailures,
			Archive:                    archive,
			Logger:                     logger,
			StatsBuffer:                statsBuffer},
		Signer:                       signer,
		VDFTTL:                       vdfTTL,
		APITTL:                       apiTTL,
		APIV1Enabled:                 &apiV1Enabled,
		TokenLifetime:                tokenLifetime,
		TrustedCIDRs:                 trusted,
		Version:                      version,
		Logger:                       logger,
		WebAssets:                    assets,
		ProjectAssets:                projectAssetMap(projects),
		ProjectsPath:                 projectsPath,
		NoticesPath:                  noticesPath,
		Notices:                      clonePublicNotices(notices),
		NoticeStore:                  newNoticeStore(notices),
		PageViews:                    newPageViewTracker(),
		Blocklist:                    blocklist,
		ClientBlocks:                 clientBlocks,
		AbuseTracker:                 abuseTracker,
		BlocklistExport:              newBlocklistExportCache(time.Minute),
		ResourceLimiter:              newPublicResourceLimiter(quota),
		StatsCache:                   &statsCache{},
		WebVerifications:             webVerifications,
		CatalogIndex:                 catalogIndex,
		CatalogCache:                 catalogCache,
		changelog:                    newChangelogStore(changelogPath, logger),
		VDFKeys:                      keys,
		CatalogBatchRows:             catalogBatchRows,
		CatalogPrefetchRemainingRows: &catalogPrefetchRemainingRows,
	}
	server.vdfPolicyReloader = newVDFPolicyReloader(vdf, vdfConfigPath, vdfConfig, logger)
	return server, nil
}

type noticeView struct {
	Level   string
	Message string
}

type noticeStore struct {
	mu      sync.RWMutex
	notices []config.PublicNotice
}

func newNoticeStore(notices []config.PublicNotice) *noticeStore {
	return &noticeStore{notices: clonePublicNotices(notices)}
}

func (s *noticeStore) get() []config.PublicNotice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePublicNotices(s.notices)
}

func (s *noticeStore) set(notices []config.PublicNotice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notices = clonePublicNotices(notices)
}

func (s Server) currentNotices() []noticeView {
	notices := clonePublicNotices(s.Notices)
	if s.NoticeStore != nil {
		notices = s.NoticeStore.get()
	}
	if strings.TrimSpace(s.NoticesPath) != "" {
		loaded, err := config.LoadNoticesOnly(s.NoticesPath)
		if err != nil {
			if s.Logger != nil {
				s.Logger.Warn(context.Background(), "公告配置热重载失败，沿用上一次有效配置",
					slog.String("error", err.Error()))
			}
		} else {
			notices = loaded
			if s.NoticeStore != nil {
				s.NoticeStore.set(loaded)
			}
		}
	}
	views := make([]noticeView, 0, len(notices))
	for _, notice := range notices {
		level := strings.TrimSpace(notice.Level)
		message := strings.TrimSpace(notice.Message)
		if level == "" || message == "" {
			continue
		}
		views = append(views, noticeView{Level: level, Message: message})
	}
	return views
}

func clonePublicNotices(notices []config.PublicNotice) []config.PublicNotice {
	if len(notices) == 0 {
		return nil
	}
	out := make([]config.PublicNotice, len(notices))
	copy(out, notices)
	return out
}

func projectAssetMap(projects config.Projects) map[string]projectAssetConfig {
	projectAssets := map[string]projectAssetConfig{}
	for _, item := range projects.Projects {
		projectAssets[item.ID] = projectAssetConfig{
			IconPath:                    filepath.Clean(item.ResolvedIconPath),
			ArchitectureSelectorEnabled: item.ArchitectureSelectorEnabled(),
			SystemSelectorEnabled:       item.SystemSelectorEnabled(),
			DefaultSelectionMode:        item.NormalizedDefaultSelectionMode(),
		}
		if strings.TrimSpace(item.ResolvedIconPath) == "" {
			projectAssets[item.ID] = projectAssetConfig{
				ArchitectureSelectorEnabled: item.ArchitectureSelectorEnabled(),
				SystemSelectorEnabled:       item.SystemSelectorEnabled(),
				DefaultSelectionMode:        item.NormalizedDefaultSelectionMode(),
			}
		}
	}
	return projectAssets
}

func (s Server) staticURL(name string) string {
	assets, err := s.assets()
	if err != nil {
		return staticURLFunc(nil)(name)
	}
	return staticURLFunc(assets.staticManifest)(name)
}

func noContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
