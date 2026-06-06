package adminui

import (
	"net/http"
	"strconv"
)

type pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

func paginationFrom(r *http.Request, fallback int) pagination {
	page := positiveInt(r.URL.Query().Get("page"), 1)
	size := positiveInt(r.URL.Query().Get("page_size"), fallback)
	if size > 100 {
		size = 100
	}
	return pagination{Page: page, PageSize: size}
}

func positiveInt(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func (p pagination) offset() int {
	return (p.Page - 1) * p.PageSize
}
