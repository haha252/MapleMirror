package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalizedPublicPagesHaveStableSSRSEO(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	handler := srv.Handler()

	cases := []struct {
		path    string
		zhTitle string
		enTitle string
	}{
		{"/", "枫源镜像 - GitHub Release 软件版本与文件下载服务", englishPublicPageMeta["/"].BrowserTitle},
		{"/stats", "枫源镜像节点状态与下载数据统计 - 访问、流量与 SLA", englishPublicPageMeta["/stats"].BrowserTitle},
		{"/api-docs", "枫源镜像公共下载 API 文档 - 项目、文件与自动下载接口", englishPublicPageMeta["/api-docs"].BrowserTitle},
		{"/changelog", "枫源镜像更新日志 - 版本发布、功能改进与服务维护", englishPublicPageMeta["/changelog"].BrowserTitle},
		{"/about", "关于枫源镜像 - 公益镜像服务、开源代码与赞助支持", englishPublicPageMeta["/about"].BrowserTitle},
	}

	for _, item := range cases {
		t.Run("zh_"+item.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, item.path, nil)
			req.Header.Set("Accept-Language", "en-US,en;q=0.9")
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, body)
			}
			for _, want := range []string{
				"<html lang=\"zh-CN\"",
				"<title>" + item.zhTitle + "</title>",
				"<link rel=\"canonical\" href=\"https://fyhub.cn" + item.path + "\">",
				"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"https://fyhub.cn" + item.path + "\">",
				"<link rel=\"alternate\" hreflang=\"en\" href=\"https://fyhub.cn" + localizedPublicPath(publicLocaleEN, item.path) + "\">",
				"<link rel=\"alternate\" hreflang=\"x-default\" href=\"https://fyhub.cn" + item.path + "\">",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("Chinese page missing %q: %s", want, body)
				}
			}
		})

		t.Run("en_"+item.path, func(t *testing.T) {
			enPath := localizedPublicPath(publicLocaleEN, item.path)
			req := httptest.NewRequest(http.MethodGet, enPath, nil)
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, body)
			}
			for _, want := range []string{
				"<html lang=\"en\"",
				"<title>" + item.enTitle + "</title>",
				"<link rel=\"canonical\" href=\"https://fyhub.cn" + enPath + "\">",
				"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"https://fyhub.cn" + item.path + "\">",
				"<link rel=\"alternate\" hreflang=\"en\" href=\"https://fyhub.cn" + enPath + "\">",
				"<link rel=\"alternate\" hreflang=\"x-default\" href=\"https://fyhub.cn" + item.path + "\">",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("English page missing %q: %s", want, body)
				}
			}
		})
	}
}

func TestLocalizedProjectPageAndEnglishRootRedirect(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	handler := srv.Handler()

	redirect := httptest.NewRecorder()
	handler.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/en", nil))
	if redirect.Code != http.StatusPermanentRedirect || redirect.Header().Get("Location") != "/en/" {
		t.Fatalf("/en redirect status=%d location=%q", redirect.Code, redirect.Header().Get("Location"))
	}

	zhReq := httptest.NewRequest(http.MethodGet, "/p1/", nil)
	zhReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
	zh := httptest.NewRecorder()
	handler.ServeHTTP(zh, zhReq)
	if zh.Code != http.StatusOK ||
		!strings.Contains(zh.Body.String(), "<html lang=\"zh-CN\"") ||
		!strings.Contains(zh.Body.String(), "<title>项目一 版本与文件下载 - 枫源镜像</title>") {
		t.Fatalf("Chinese project page must remain Chinese under English browser headers: %d %s", zh.Code, zh.Body.String())
	}

	en := httptest.NewRecorder()
	handler.ServeHTTP(en, httptest.NewRequest(http.MethodGet, "/en/p1/", nil))
	if en.Code != http.StatusOK {
		t.Fatalf("English project status=%d body=%s", en.Code, en.Body.String())
	}
	for _, want := range []string{
		"<html lang=\"en\"",
		"<title>项目一 Releases and File Downloads - Maple Mirror</title>",
		"<link rel=\"canonical\" href=\"https://fyhub.cn/en/p1/\">",
		"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"https://fyhub.cn/p1/\">",
		"<link rel=\"alternate\" hreflang=\"en\" href=\"https://fyhub.cn/en/p1/\">",
	} {
		if !strings.Contains(en.Body.String(), want) {
			t.Fatalf("English project page missing %q: %s", want, en.Body.String())
		}
	}
}

func TestEnglishPageNavigationStaysUnderEnglishPrefix(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/en/", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"href=\"/en/\"",
		"href=\"/en/stats\"",
		"href=\"/en/api-docs\"",
		"href=\"/en/changelog\"",
		"href=\"/en/about\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("English navigation missing %q: %s", want, body)
		}
	}
}
