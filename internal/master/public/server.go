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
	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
)

type Server struct {
	Store            Store
	Signer           downloadtoken.Signer
	ALTCHATTL        time.Duration
	ALTCHADifficulty int
	APITTL           time.Duration
	TokenTTL         time.Duration
	APIZeroBits      int
	TrustedCIDRs     []string
	Logger           *logging.Logger
	WebAssets        *webAssets
	ProjectAssets    map[string]projectAssetConfig
	ProjectsPath     string
	NoticesPath      string
	Notices          []config.PublicNotice
	NoticeStore      *noticeStore
	PageViews        *pageViewTracker
	Blocklist        *blocklistPolicy
}

func New(db *sql.DB, signer downloadtoken.Signer, altchaTTL, apiTTL, tokenTTL time.Duration,
	altchaDifficulty, apiBits int, quota config.Quota, loc *time.Location, trusted []string,
	projects config.Projects, projectsPath, noticesPath string, notices []config.PublicNotice,
	runtime *mastercontrol.RuntimeStore,
	logger *logging.Logger, publicProbeNetworkFailures int) (Server, error) {
	assets, err := loadDefaultWebAssets()
	if err != nil {
		return Server{}, err
	}
	challenges := newChallengeMemory()
	challenges.startCleanup(minDuration(altchaTTL, apiTTL, time.Minute))
	blocklist := newBlocklistPolicy(quota, logger)
	blocklist.start()
	return Server{
		Store: Store{DB: db, Quota: newQuotaPolicy(quota), Location: loc,
			Challenges: challenges, MaxBytes: newMaxBytesPolicy(quota),
			RangeLimit: quota.RangeConcurrencyLimit, Runtime: runtime,
			PublicProbeNetworkFailures: publicProbeNetworkFailures},
		Signer:           signer,
		ALTCHATTL:        altchaTTL,
		ALTCHADifficulty: altchaDifficulty,
		APITTL:           apiTTL,
		TokenTTL:         tokenTTL,
		APIZeroBits:      apiBits,
		TrustedCIDRs:     trusted,
		Logger:           logger,
		WebAssets:        assets,
		ProjectAssets:    projectAssetMap(projects),
		ProjectsPath:     projectsPath,
		NoticesPath:      noticesPath,
		Notices:          clonePublicNotices(notices),
		NoticeStore:      newNoticeStore(notices),
		PageViews:        newPageViewTracker(),
		Blocklist:        blocklist,
	}, nil
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
			IconPath:                 filepath.Clean(item.ResolvedIconPath),
			ArchitectureMatchEnabled: item.ArchitectureMatchEnabled,
			SystemMatchEnabled:       item.SystemMatchEnabled,
		}
		if strings.TrimSpace(item.ResolvedIconPath) == "" {
			projectAssets[item.ID] = projectAssetConfig{
				ArchitectureMatchEnabled: item.ArchitectureMatchEnabled,
				SystemMatchEnabled:       item.SystemMatchEnabled,
			}
		}
	}
	return projectAssets
}

func minDuration(values ...time.Duration) time.Duration {
	out := time.Duration(0)
	for _, value := range values {
		if value > 0 && (out == 0 || value < out) {
			out = value
		}
	}
	return out
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.WebAssets != nil && s.WebAssets.staticFS != nil {
		mux.Handle("/static/public/", noCache(http.StripPrefix("/static/public/", http.FileServer(http.FS(s.WebAssets.staticFS)))))
	} else if s.WebAssets != nil {
		mux.Handle("/static/public/", noCache(http.StripPrefix("/static/public/", http.FileServer(http.Dir(s.WebAssets.staticDir)))))
	} else if assets, err := loadDefaultWebAssets(); err == nil {
		mux.Handle("/static/public/", noCache(http.StripPrefix("/static/public/", http.FileServer(http.FS(assets.staticFS)))))
	}
	mux.HandleFunc("/static/project-icons/", s.projectIcon)
	mux.HandleFunc("/downloads/", s.downloadMisrouted)
	mux.HandleFunc("/favicon.ico", s.favicon)
	mux.HandleFunc("/stats", s.statsPage)
	mux.HandleFunc("/about", s.aboutPage)
	mux.HandleFunc("/api-docs", s.apiDocsPage)
	mux.HandleFunc("/download/", s.downloadPowPage)
	mux.Handle("/api/public/v1/stats", statsJSONCompression(http.HandlerFunc(s.statsAPI)))
	mux.Handle("/api/public/v1/stats/details", statsJSONCompression(http.HandlerFunc(s.statsDetailsAPI)))
	mux.HandleFunc("/api/public/v1/projects", s.projects)
	mux.HandleFunc("/api/public/v1/projects/", s.projectAssets)
	mux.HandleFunc("/api/public/v1/web/challenges", s.webChallenge)
	mux.HandleFunc("/api/public/v1/web/authorizations", s.webAuthorize)
	mux.HandleFunc("/api/public/v1/api/challenges", s.apiChallenge)
	mux.HandleFunc("/api/public/v1/api/authorizations", s.apiAuthorize)
	mux.HandleFunc("/api/public/v1/authorizations/", s.authorization)
	mux.HandleFunc("/", s.downloadPage)
	return mux
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func noContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s Server) projects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "项目查询失败")
		return
	}
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{"projects": projects})
}

func (s Server) projectAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	prefix := "/api/public/v1/projects/"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	projectID, ok := strings.CutSuffix(path, "/assets")
	if !ok || projectID == "" || strings.Contains(projectID, "/") {
		writeError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "项目资产不存在")
		return
	}
	assets, err := s.Store.Assets(r.Context(), projectID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "资产查询失败")
		return
	}
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{"assets": assets})
}
