package fiberparser

import "testing"

func TestGetOffset(t *testing.T) {
	cases := []struct {
		name string
		req  PaginationRequest[any]
		want int
	}{
		{"page 1", PaginationRequest[any]{Page: 1, Limit: 10}, 0},
		{"page 2", PaginationRequest[any]{Page: 2, Limit: 10}, 10},
		{"page 3 limit 20", PaginationRequest[any]{Page: 3, Limit: 20}, 40},
		{"zero page defaults to no offset", PaginationRequest[any]{Page: 0, Limit: 10}, 0},
		{"zero limit defaults to no offset", PaginationRequest[any]{Page: 2, Limit: 0}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.GetOffset(); got != tc.want {
				t.Errorf("GetOffset() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestToPagination(t *testing.T) {
	req := PaginationRequest[any]{Page: 2, Limit: 10}
	pag := req.ToPagination(10, 25)

	if pag.CurrentPage != 2 {
		t.Errorf("CurrentPage = %d, want 2", pag.CurrentPage)
	}
	if pag.From != 11 {
		t.Errorf("From = %d, want 11", pag.From)
	}
	if pag.To != 20 {
		t.Errorf("To = %d, want 20", pag.To)
	}
	if pag.Pages != 3 {
		t.Errorf("Pages = %d, want 3", pag.Pages)
	}
	if pag.Total != 25 {
		t.Errorf("Total = %d, want 25", pag.Total)
	}
}

func TestToPaginationEmptyPage(t *testing.T) {
	req := PaginationRequest[any]{Page: 1, Limit: 10}
	pag := req.ToPagination(0, 0)

	if pag.From != 0 || pag.To != 0 {
		t.Errorf("From/To on empty page = %d/%d, want 0/0", pag.From, pag.To)
	}
}
