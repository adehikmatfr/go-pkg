// Package fiberparser provides pagination request/response helpers and a
// standard JSON response envelope for github.com/gofiber/fiber/v2 handlers.
package fiberparser

// PaginationRequest is the standard shape for a paginated list endpoint's
// query parameters. T is the filter payload specific to that endpoint.
type PaginationRequest[T any] struct {
	Page    int    `json:"pages" default:"1"`
	Limit   int    `json:"limit" default:"10"`
	SortBy  string `json:"sort_by"`
	OrderBy string `json:"order_by" default:"DESC"`
	Search  string `json:"search"`
	Filter  T      `json:"filter"`
}

// GetOffset returns the zero-based row offset for this page, clamped so a
// non-positive Page or Limit (e.g. an unvalidated zero-value request) never
// produces a negative offset.
func (r *PaginationRequest[T]) GetOffset() int {
	if r.Page <= 1 || r.Limit <= 0 {
		return 0
	}
	return (r.Page - 1) * r.Limit
}

// ToPagination builds the response Pagination block given the number of rows
// returned on this page (length) and the total row count across all pages.
func (r *PaginationRequest[T]) ToPagination(length, total int) Pagination {
	page := r.Page
	if page < 1 {
		page = 1
	}
	limit := r.Limit
	if limit < 1 {
		limit = 1
	}

	from := (page-1)*limit + 1
	to := from + length - 1
	if length <= 0 {
		from, to = 0, 0
	}

	return Pagination{
		CurrentPage: page,
		From:        from,
		To:          to,
		Pages:       (total + limit - 1) / limit,
		Total:       total,
	}
}
