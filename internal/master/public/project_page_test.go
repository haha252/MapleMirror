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
		`id="project-file-browser"`,
		`data-folder-icon=`,
		`class="file-browser__level file-browser__level--versions"`,
		`class="file-browser__level file-browser__level--files" hidden`,
		`class="file-browser__back"`,
		`class="file-browser__versions" role="group"`,
		`class="file-browser__files" role="group"`,
		`/static/public/project.css?v=`,
		`/static/public/project-responsive.css?v=`,
		`/static/public/project.js?v=`,
		`/static/public/download-file-browser.js?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected project page to contain %q: %s", want, body)
		}
	}
	modeAt := strings.Index(body, `class="selection-mode project-selection-mode"`)
	downloadAt := strings.Index(body, `class="project-download"`)
	availabilityAt := strings.Index(body, `id="project-availability"`)
	buttonAt := strings.Index(body, `id="project-download-button"`)
	if modeAt < 0 || downloadAt < 0 || availabilityAt < 0 || buttonAt < 0 ||
		modeAt >= downloadAt || availabilityAt >= buttonAt {
		t.Fatalf("详情页选择方式应位于下载选择区外侧，资产信息应位于下载按钮之前：%s", body)
	}
	if strings.Contains(body, `class="project-download__action"`) {
		t.Fatalf("详情页下载按钮不应继续套用多余操作容器：%s", body)
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
