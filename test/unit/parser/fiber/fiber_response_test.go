package fiberparser_test

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"

	fiberparser "github.com/adehikmatfr/go-pkg/v2/parser/fiber"
)

func TestSingleResponseCreateResponse(t *testing.T) {
	r := fiberparser.NewSingleResponse[string]()
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
	if r.GetResponse() != r {
		t.Error("GetResponse() should return the receiver itself")
	}
}

func TestSingleResponseCreateResponsePreservesGivenMeta(t *testing.T) {
	given := &fiberparser.Meta{RequestID: "req-1"}
	r := fiberparser.NewSingleResponse[string]()
	r.CreateResponse("payload", "ok", given)

	if r.Meta != given {
		t.Error("CreateResponse() should keep the given Meta unchanged instead of stamping a new one")
	}
}

func TestListResponseCreateResponse(t *testing.T) {
	r := fiberparser.NewListResponse[int]()
	pag := &fiberparser.Pagination{Total: 3}
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
	if r.GetResponse() != r {
		t.Error("GetResponse() should return the receiver itself")
	}
}

func TestFail(t *testing.T) {
	resp := fiberparser.Fail("NOT_FOUND", "resource not found", http.StatusNotFound, nil)
	if resp.GetStatus() != http.StatusNotFound {
		t.Errorf("GetStatus() = %d, want %d", resp.GetStatus(), http.StatusNotFound)
	}

	body, ok := resp.GetResponse().(fiberparser.ErrorOnly)
	if !ok {
		t.Fatalf("GetResponse() type = %T, want ErrorOnly", resp.GetResponse())
	}
	if body.Errors.Code != "NOT_FOUND" {
		t.Errorf("Errors.Code = %q, want %q", body.Errors.Code, "NOT_FOUND")
	}
}

func TestValidationFail(t *testing.T) {
	details := []fiberparser.ErrorDetail{{Field: "email", Message: "required"}}
	resp := fiberparser.ValidationFail(details, nil)

	if resp.GetStatus() != http.StatusUnprocessableEntity {
		t.Errorf("GetStatus() = %d, want %d", resp.GetStatus(), http.StatusUnprocessableEntity)
	}
	body := resp.GetResponse().(fiberparser.ErrorOnly)
	if len(body.Errors.Details) != 1 || body.Errors.Details[0].Field != "email" {
		t.Errorf("Errors.Details = %+v, want one detail for field email", body.Errors.Details)
	}
}

func TestResponseJSON(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		resp := fiberparser.Fail("BAD", "bad request", http.StatusBadRequest, nil)
		return fiberparser.ResponseJSON(c, resp)
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
	_ fiberparser.Responder = fiberparser.ErrorOnly{}
	_ fiberparser.Responder = &fiberparser.SingleResponse[string]{}
	_ fiberparser.Responder = &fiberparser.ListResponse[string]{}
)
