package public

import (
	"html/template"
	"strings"
)

func changelogShellBody(staticURL func(string) string) template.HTML {
	body := `
<section class="changelog-page">
  <div id="changelog-filter-backdrop" class="catalog-filter-backdrop" hidden></div>
  <div class="download-layout">
    <div class="catalog-left-rail">
      <aside id="changelog-filters" class="catalog-filters" data-i18n-aria-label="changelog.filterAria" aria-label="更新日志筛选器">
        <div class="catalog-filters__mobile-head">
          <h2 data-i18n="catalog.filter">筛选器</h2>
          <div class="catalog-filters__mobile-actions">
            <button id="changelog-filter-clear" type="button" disabled>
              <svg aria-hidden="true"><use href="__ICONS__#broom"></use></svg>
              <span>取消全部</span>
            </button>
            <button id="changelog-filter-close" type="button" data-i18n-aria-label="catalog.close" aria-label="关闭筛选器">
              <svg aria-hidden="true"><use href="__ICONS__#close"></use></svg>
            </button>
          </div>
        </div>
        <div class="catalog-filter-groups">
          <details class="catalog-filter-group" open>
            <summary>
              <span data-i18n="changelog.filterLevel">日志等级</span>
              <svg aria-hidden="true"><use href="__ICONS__#chevron-down"></use></svg>
            </summary>
            <div class="catalog-filter-options">
              <label class="catalog-filter-option"><input type="radio"
                name="changelog-minimum-level" value="info" checked><span>展示全部</span></label>
              <label class="catalog-filter-option"><input type="radio"
                name="changelog-minimum-level" value="notice"><span>公告及以上</span></label>
              <label class="catalog-filter-option"><input type="radio"
                name="changelog-minimum-level" value="warn"><span>警告及以上</span></label>
              <label class="catalog-filter-option"><input type="radio"
                name="changelog-minimum-level" value="critical"><span>事故</span></label>
            </div>
          </details>
        </div>
      </aside>
    </div>
    <div class="changelog-content">
      <div class="catalog-mobile-toolbar">
        <label class="catalog-search" for="changelog-search-mobile">
          <svg aria-hidden="true"><use href="__ICONS__#search"></use></svg>
          <input id="changelog-search-mobile" type="search" autocomplete="off"
            maxlength="100" data-i18n-placeholder="search.changelog" placeholder="搜索更新标题或描述">
        </label>
        <button id="changelog-filter-button" class="catalog-filter-button" type="button"
          data-i18n-aria-label="catalog.filterButton" aria-label="打开筛选器" aria-controls="changelog-filters" aria-expanded="false">
          <svg aria-hidden="true"><use href="__ICONS__#filter"></use></svg>
          <span data-i18n="catalog.filter">筛选器</span><span id="changelog-filter-count"
            class="catalog-filter-count" hidden></span>
        </button>
      </div>
      <div id="changelog-timeline" class="changelog-timeline" role="list"></div>
      <div id="changelog-load-more" class="changelog-load-more">
        <button type="button" class="button-link" hidden>加载更多</button>
      </div>
      <div id="changelog-status" class="changelog-status muted"
        role="status" aria-live="polite" data-i18n="changelog.loading">正在加载更新记录...</div>
    </div>
  </div>
</section>`
	return template.HTML(strings.ReplaceAll(body, "__ICONS__", esc(staticURL("icons.svg"))))
}
