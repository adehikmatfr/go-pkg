package fiberparser

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestSingleResponseCreateResponse(t *testing.T) {
	r := NewSingleResponse[string]()
	r.CreateResponse("payload", "ok", nil)

	if r.GetStatus() != http.StatusOK {
		t.Errorf("GetStatus() = %d, want %d", r.GetStatus(), http.StatusOK)
	}
	if r.Message != "ok" || r.Data != "payload" {
		t.Errorf("response = %+v, want Message=ok Data=payload", r)
	}
	if r.Meta == nil || r.Meta.Timestamp == "" {
		t.Error("CreateResponse() should stamp a default Meta when nil is passed")
	}
}

func TestListResponseCreateResponse(t *testing.T) {
	r := NewListResponse[int]()
	pag := &Pagination{Total: 3}
	r.CreateResponse([]int{1, 2, 3}, "ok", pag, nil)

	if r.GetStatus() != http.StatusOK {
		t.Errorf("GetStatus() = %d, want %d", r.GetStatus(), http.StatusOK)
	}
	if len(r.Data) != 3 {
		t.Errorf("len(Data) = %d, want 3", len(r.Data))
	}
	if r.Pagination != pag {
		t.Error("Pagination not set as given")
	}
}

func TestFail(t *testing.T) {
	resp := Fail("NOT_FOUND", "resource not found", http.StatusNotFound, nil)
	if resp.GetStatus() != http.StatusNotFound {
		t.Errorf("GetStatus() = %d, want %d", resp.GetStatus(), http.StatusNotFound)
	}

	body, ok := resp.GetResponse().(ErrorOnly)
	if !ok {
		t.Fatalf("GetResponse() type = %T, want ErrorOnly", resp.GetResponse())
	}
	if body.Errors.Code != "NOT_FOUND" {
		t.Errorf("Errors.Code = %q, want %q", body.Errors.Code, "NOT_FOUND")
	}
}

func TestValidationFail(t *testing.T) {
	details := []ErrorDetail{{Field: "email", Message: "required"}}
	resp := ValidationFail(details, nil)

	if resp.GetStatus() != http.StatusUnprocessableEntity {
		t.Errorf("GetStatus() = %d, want %d", resp.GetStatus(), http.StatusUnprocessableEntity)
	}
	body := resp.GetResponse().(ErrorOnly)
	if len(body.Errors.Details) != 1 || body.Errors.Details[0].Field != "email" {
		t.Errorf("Errors.Details = %+v, want one detail for field email", body.Errors.Details)
	}
}

func TestMetaOrNowPreservesGivenMeta(t *testing.T) {
	given := &Meta{RequestID: "req-1"}
	got := metaOrNow(given)
	if got != given {
		t.Error("metaOrNow() should return the given Meta unchanged")
	}
}

func TestResponseJSON(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		resp := Fail("BAD", "bad request", http.StatusBadRequest, nil)
		return ResponseJSON(c, resp)
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// Responder is satisfied by ErrorOnly by value and SingleResponse/ListResponse
// by pointer; this compiles only if both remain assignable to the interface,
// which is what lets callers depend on Responder instead of concrete types.
var (
	_ Responder = ErrorOnly{}
	_ Responder = &SingleResponse[string]{}
	_ Responder = &ListResponse[string]{}
)
