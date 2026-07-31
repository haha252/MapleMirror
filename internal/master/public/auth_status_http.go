package public

import (
	"net/http"
	"strings"

	"mirror-server/internal/downloadtoken"
)

func (s Server) authorization(w http.ResponseWriter, r *http.Request) {
	s.authorizationByID(w, r, "/api/public/v1/authorizations/")
}

func (s Server) authorizationV2(w http.ResponseWriter, r *http.Request) {
	s.authorizationByID(w, r, "/api/public/v2/authorizations/")
}

func (s Server) authorizationByID(w http.ResponseWriter, r *http.Request, pathPrefix string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, pathPrefix)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "授权不存在")
		return
	}
	auth, err := s.Store.Authorization(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "授权不存在")
		return
	}
	token := authorizationBearer(r)
	claims, verifyErr := s.Signer.Verify(token)
	legacyOK := verifyErr == nil && claims.AuthorizationID == id &&
		claims.AssetID == auth.AssetID && claims.NodeID == auth.NodeID &&
		claims.ClientPrefix == auth.ClientPrefixKey && claims.ClientPrefix == s.clientPrefix(r)
	opaqueOK := auth.TokenHash != "" && auth.TokenHash == downloadtoken.OpaqueHash(token) &&
		auth.ClientPrefixKey == s.clientPrefix(r)
	if !legacyOK && !opaqueOK {
		writeError(w, r, http.StatusUnauthorized, "DOWNLOAD_TOKEN_INVALID", "下载令牌无效")
		return
	}
	sent, first, _ := s.Store.AuthorizationBytes(r.Context(), id)
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{
		"authorization_id": auth.AuthorizationID, "asset_id": auth.AssetID,
		"node_id": auth.NodeName, "state": auth.State, "expires_at": auth.ExpiresAt,
		"bytes_accounting_enabled": true, "sent_bytes": sent, "first_transfer_at": first,
	})
}

func authorizationBearer(r *http.Request) string {
	const prefix = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return ""
}
