package public

import (
	"net/http"
	"strings"
)

type downloadProjectView struct {
	ProjectSummary
	Assets []AssetSummary
}

func (s Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		http.Error(w, "项目列表读取失败", http.StatusInternalServerError)
		return
	}
	views := make([]downloadProjectView, 0, len(projects))
	for _, p := range projects {
		view := downloadProjectView{ProjectSummary: p}
		assets, err := s.Store.Assets(r.Context(), p.ProjectID)
		if err != nil {
			http.Error(w, "资产列表读取失败", http.StatusInternalServerError)
			return
		}
		view.Assets = assets
		views = append(views, view)
	}
	renderPage(w, "下载", buildDownloadPageBody(views))
}

func buildDownloadPageBody(projects []downloadProjectView) string {
	var b strings.Builder
	b.WriteString(`<p class="muted">选择可下载文件后点击“下载”，系统会先执行 ALTCHA 验证，再签发绑定单节点的短时下载令牌。</p>`)
	b.WriteString(`<div id="download-status" class="status muted">请选择一个文件开始下载。</div>`)
	for _, project := range projects {
		b.WriteString(`<section class="project-card">`)
		b.WriteString(`<div class="project-head"><div><h2>`)
		b.WriteString(esc(project.DisplayName))
		b.WriteString(`</h2><p class="muted">`)
		b.WriteString(esc(project.Repository))
		b.WriteString(`</p></div><div>`)
		if project.Available {
			b.WriteString(`<span class="ok">可下载</span>`)
		} else {
			b.WriteString(`<span class="warn">暂不可下载</span>`)
			b.WriteString(renderDetail("原因", project.UnavailableReason))
		}
		b.WriteString(`</div></div>`)
		if len(project.Assets) == 0 {
			b.WriteString(`<p class="muted">暂无可展示文件。</p></section>`)
			continue
		}
		b.WriteString(`<table class="asset-table"><tr><th>版本</th><th>架构</th><th>文件</th><th>大小</th><th>状态</th><th>操作</th></tr>`)
		for _, asset := range project.Assets {
			b.WriteString(`<tr><td>`)
			b.WriteString(esc(asset.Version))
			b.WriteString(`</td><td>`)
			b.WriteString(esc(asset.Architecture))
			b.WriteString(`</td><td>`)
			b.WriteString(esc(asset.FileName))
			b.WriteString(`</td><td>`)
			b.WriteString(bytesText(asset.SizeBytes))
			b.WriteString(`</td><td>`)
			if asset.Available {
				b.WriteString(`<span class="ok">可下载</span>`)
			} else {
				b.WriteString(`<span class="warn">暂不可下载</span>`)
				b.WriteString(renderDetail("原因", asset.UnavailableReason))
			}
			b.WriteString(`</td><td>`)
			if asset.Available {
				b.WriteString(`<button type="button" class="download-btn" data-asset-id="`)
				b.WriteString(esc(asset.AssetID))
				b.WriteString(`" data-asset-name="`)
				b.WriteString(esc(asset.FileName))
				b.WriteString(`" onclick="startDownload(this)">下载</button>`)
			} else {
				b.WriteString(`<span class="muted">无可用入口</span>`)
			}
			b.WriteString(`</td></tr>`)
		}
		b.WriteString(`</table></section>`)
	}
	b.WriteString(`<script>
(function () {
  const statusBox = document.getElementById("download-status");
  const encoder = new TextEncoder();

  function setStatus(message, level) {
    statusBox.textContent = message;
    statusBox.className = "status " + (level || "muted");
  }

  function hasLeadingZeroBits(bytes, bits) {
    for (const byte of bytes) {
      if (bits <= 0) return true;
      if (bits >= 8) {
        if (byte !== 0) return false;
        bits -= 8;
        continue;
      }
      return (byte >> (8 - bits)) === 0;
    }
    return bits <= 0;
  }

  async function solveChallenge(challenge, difficulty) {
    for (let i = 0; ; i++) {
      const data = encoder.encode(challenge + ":" + i);
      const digest = await crypto.subtle.digest("SHA-256", data);
      if (hasLeadingZeroBits(new Uint8Array(digest), difficulty)) {
        return i;
      }
      if ((i & 1023) === 0) {
        await new Promise((resolve) => setTimeout(resolve, 0));
      }
    }
  }

  async function postJSON(url, payload) {
    const resp = await fetch(url, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload)
    });
    const text = await resp.text();
    let body = null;
    try {
      body = JSON.parse(text);
    } catch (err) {
      body = {message: text};
    }
    if (!resp.ok) {
      throw new Error((body && body.message) ? body.message : "请求失败");
    }
    return body;
  }

  window.startDownload = async function (button) {
    if (!window.crypto || !window.crypto.subtle) {
      setStatus("当前浏览器不支持下载验证所需的加密能力。", "warn");
      return;
    }
    const assetId = button.getAttribute("data-asset-id");
    const assetName = button.getAttribute("data-asset-name") || assetId;
    const oldText = button.textContent;
    button.disabled = true;
    button.textContent = "验证中...";
    try {
      setStatus("正在创建下载挑战...", "muted");
      const challengeResp = await postJSON("/api/public/v1/web/challenges", {asset_id: assetId});
      const challengeData = challengeResp.data || {};
      const altcha = challengeData.altcha || {};
      const difficulty = challengeData.difficulty || 10;
      if (!challengeData.challenge_id || !altcha.challenge) {
        throw new Error("挑战数据缺失");
      }
      setStatus("正在计算验证答案...", "muted");
      const number = await solveChallenge(altcha.challenge, difficulty);
      setStatus("正在领取下载授权...", "muted");
      const authResp = await postJSON("/api/public/v1/web/authorizations", {
        challenge_id: challengeData.challenge_id,
        asset_id: assetId,
        altcha_payload: {number: number}
      });
      const authData = authResp.data || {};
      if (!authData.download_url || !authData.download_token) {
        throw new Error("授权数据缺失");
      }
      setStatus("授权已签发，正在开始下载 " + assetName + "。", "ok");
      window.location.href = authData.download_url + "?token=" + encodeURIComponent(authData.download_token);
    } catch (err) {
      setStatus(err && err.message ? err.message : "下载失败", "warn");
    } finally {
      button.disabled = false;
      button.textContent = oldText;
    }
  };
	})();
</script>`)
	return b.String()
}
