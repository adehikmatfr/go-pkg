package fiberparser

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ErrorDetail describes one field-level validation failure.
type ErrorDetail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ErrorResponse is the standard error body shape.
type ErrorResponse struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details,omitempty"`
}

// Meta carries request-scoped metadata attached to every response.
type Meta struct {
	RequestID  string `json:"request_id"`
	Timestamp  string `json:"timestamp"`
	TraceID    string `json:"trace_id,omitempty"`
	Cached     bool   `json:"cached,omitempty"`
	CustomFlag string `json:"custom_flag,omitempty"`
}

// Pagination describes a page of results within a larger result set.
type Pagination struct {
	CurrentPage int `json:"current_page"`
	From        int `json:"from"`
	To          int `json:"to"`
	Pages       int `json:"pages"`
	Total       int `json:"total"`
}

// Responder is anything ResponseJSON can render: an HTTP status plus a body.
type Responder interface {
	GetStatus() int
	GetResponse() interface{}
}

// SingleResponse envelopes a single-object success response.
type SingleResponse[T any] struct {
	Status  int            `json:"-"`
	Message string         `json:"message"`
	Data    T              `json:"data,omitempty"`
	Errors  *ErrorResponse `json:"errors,omitempty"`
	Meta    *Meta          `json:"meta,omitempty"`
}

func NewSingleResponse[T any]() *SingleResponse[T] {
	return &SingleResponse[T]{}
}

func (r *SingleResponse[T]) GetStatus() int           { return r.Status }
func (r *SingleResponse[T]) GetResponse() interface{} { return r }

func (r *SingleResponse[T]) CreateResponse(data T, message string, meta *Meta) {
	r.Status = http.StatusOK
	r.Message = message
	r.Data = data
	r.Meta = metaOrNow(meta)
}

// ListResponse envelopes a paginated list success response.
type ListResponse[T any] struct {
	Status     int            `json:"-"`
	Message    string         `json:"message"`
	Data       []T            `json:"data,omitempty"`
	Errors     *ErrorResponse `json:"errors,omitempty"`
	Pagination *Pagination    `json:"pagination,omitempty"`
	Meta       *Meta          `json:"meta,omitempty"`
}

func NewListResponse[T any]() *ListResponse[T] {
	return &ListResponse[T]{}
}

func (r *ListResponse[T]) GetStatus() int           { return r.Status }
func (r *ListResponse[T]) GetResponse() interface{} { return r }

func (r *ListResponse[T]) CreateResponse(data []T, message string, pag *Pagination, meta *Meta) {
	r.Status = http.StatusOK
	r.Message = message
	r.Data = data
	r.Pagination = pag
	r.Meta = metaOrNow(meta)
}

// ErrorOnly envelopes an error response with no data payload.
type ErrorOnly struct {
	Status int           `json:"-"`
	Errors ErrorResponse `json:"errors"`
	Meta   *Meta         `json:"meta,omitempty"`
}

func (e ErrorOnly) GetStatus() int           { return e.Status }
func (e ErrorOnly) GetResponse() interface{} { return e }

// Fail builds a generic error Responder.
func Fail(code, msg string, httpStatus int, meta *Meta) Responder {
	return ErrorOnly{
		Status: httpStatus,
		Errors: ErrorResponse{Code: code, Message: msg},
		Meta:   metaOrNow(meta),
	}
}

// ValidationFail builds a 422 error Responder with per-field validation details.
func ValidationFail(details []ErrorDetail, meta *Meta) Responder {
	return ErrorOnly{
		Status: http.StatusUnprocessableEntity,
		Errors: ErrorResponse{
			Code:    "VALIDATION_ERROR",
			Message: "Validation failed",
			Details: details,
		},
		Meta: metaOrNow(meta),
	}
}

// metaOrNow returns m if non-nil, otherwise a Meta stamped with the current time.
func metaOrNow(m *Meta) *Meta {
	if m != nil {
		return m
	}
	return &Meta{Timestamp: time.Now().UTC().Format(time.RFC3339)}
}

// ResponseJSON writes resp's status code and JSON body to c.
func ResponseJSON(c *fiber.Ctx, resp Responder) error {
	c.Response().SetStatusCode(resp.GetStatus())
	return c.JSON(resp.GetResponse())
}
