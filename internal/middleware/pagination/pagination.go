package pagination

import (
	"fmt"
	"net/http"
	"strconv"
)

type PageParams struct {
	Page     int
	PageSize int
	Offset   int
}

type PaginatedResponse struct {
	Count    int64   `json:"count"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Results  any     `json:"results"`
}

// ParsePageParams extracts pagination parameters from request
func ParsePageParams(r *http.Request) PageParams {
	page := 1
	pageSize := 10

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}

	return PageParams{
		Page:     page,
		PageSize: pageSize,
		Offset:   (page - 1) * pageSize,
	}
}

// BuildPaginatedResponse creates DRF-style response
func BuildPaginatedResponse(r *http.Request, totalCount int64, results any, params PageParams) PaginatedResponse {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.Path)

	totalPages := int((totalCount + int64(params.PageSize) - 1) / int64(params.PageSize))

	var next *string
	var previous *string

	if params.Page < totalPages {
		nextQuery := r.URL.Query()
		nextQuery.Set("page", strconv.Itoa(params.Page+1))
		nextQuery.Set("page_size", strconv.Itoa(params.PageSize))
		nextURL := fmt.Sprintf("%s?%s", baseURL, nextQuery.Encode())
		next = &nextURL
	}

	if params.Page > 1 {
		prevQuery := r.URL.Query()
		prevQuery.Set("page", strconv.Itoa(params.Page-1))
		prevQuery.Set("page_size", strconv.Itoa(params.PageSize))
		prevURL := fmt.Sprintf("%s?%s", baseURL, prevQuery.Encode())
		previous = &prevURL
	}

	return PaginatedResponse{
		Count:    totalCount,
		Next:     next,
		Previous: previous,
		Results:  results,
	}
}
