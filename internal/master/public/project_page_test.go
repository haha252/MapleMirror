package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectPageRendersOptionalHomepageAndDescription(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE projects SET description = '项目描述正文',
		homepage_url = 'https://project.example.test' WHERE id = 'p1'`)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/p1/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("project page status=%d body=%s", rec.Code, body)
	}
	for _, want := range []string{
		`<title>项目一下载 - 枫源镜像</title>`,
		`<meta name="description" content="项目描述正文 枫源镜像 是一个公益镜像服务，面向 Github Release 设计。我们致力于为所有用户提供，免费、纯净、高速且稳定的下载服务，获取到软件的最新版本。">`,
		`class="site-header"`,
		`class="project-back"`,
		`<path d="M15 5 8 12l7 7">`,
		`href="https://project.example.test"`,
		`项目描述正文`,
		`data-selection-mode="selectors"`,
		`data-selection-mode="file"`,
		`#selection-conditions`,
		`#selection-files`,
		`id="project-file"`,
		`/static/public/project.css?v=`,
		`/static/public/project-responsive.css?v=`,
		`/static/public/project.js?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected project page to contain %q: %s", want, body)
		}
	}
	availabilityAt := strings.Index(body, `id="project-availability"`)
	actionAt := strings.Index(body, `class="project-download__action"`)
	modeAt := strings.LastIndex(body, `class="selection-mode"`)
	if availabilityAt < 0 || actionAt < 0 || modeAt < 0 ||
		availabilityAt >= actionAt || modeAt <= actionAt {
		t.Fatalf("详情页可用状态应移到资产信息，选择方式应放入原操作位置：%s", body)
	}
}

func TestProjectPageUsesServiceDescriptionWhenProjectDescriptionIsEmpty(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/p1/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("project page status=%d body=%s", rec.Code, rec.Body.String())
	}
	want := `<meta name="description" content="枫源镜像 是一个公益镜像服务，面向 Github Release 设计。我们致力于为所有用户提供，免费、纯净、高速且稳定的下载服务，获取到软件的最新版本。">`
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("expected service description, body=%s", rec.Body.String())
	}
}

func TestProjectPageHidesEmptyOptionalSections(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/p1/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("project page status=%d body=%s", rec.Code, body)
	}
	if strings.Contains(body, `class="project-homepage"`) ||
		strings.Contains(body, `class="project-description`) {
		t.Fatalf("empty homepage/description should not render optional components: %s", body)
	}
}

func TestProjectPageDoesNotInterceptDownloadPath(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/p1/v1/a.zip?from=home", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `<title>下载验证 - 枫源镜像</title>`) {
		t.Fatalf("download path should render pow page, code=%d body=%s", rec.Code, body)
	}
	if strings.Contains(body, `class="project-page"`) {
		t.Fatalf("download path should not render project page: %s", body)
	}
}

func TestProjectPageReturnsNotFoundForUnknownProject(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/missing/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project should be 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}
