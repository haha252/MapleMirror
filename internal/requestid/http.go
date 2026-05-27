package requestid

import (
	"net/http"
	"regexp"
)

var validParent = regexp.MustCompile(`^[A-Za-z0-9._:/+-]{1,128}$`)

func Middleware(next http.Handler, responseHeader, parentHeader string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := New()
		if err != nil {
			http.Error(w, "无法生成请求标识", http.StatusInternalServerError)
			return
		}
		parent := r.Header.Get(parentHeader)
		if !validParent.MatchString(parent) {
			parent = ""
		}
		w.Header().Set(responseHeader, id)
		next.ServeHTTP(w, r.WithContext(WithValues(r.Context(), id, parent)))
	})
}
