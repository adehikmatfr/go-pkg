package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// ErrEmptyResponseBody is returned by Response.Consume when the response has
// no body to decode.
var ErrEmptyResponseBody = errors.New("response body is empty")

// DefaultRest performs the actual HTTP round-trip for a Request.
type DefaultRest interface {
	MakeRequest(r Request) *Result
}

// DefaultRestClient is the default DefaultRest implementation.
type DefaultRestClient struct {
	Client  *http.Client
	Timeout int
}

// MakeRequest performs the request described by r. A successful call returns
// a *Result with Response populated and Error nil; any failure (building the
// request, performing it, reading the body) is captured in Result.Error
// instead of being returned directly, so callers always get a non-nil Result
// to inspect via Consume.
func (c *DefaultRestClient) MakeRequest(r Request) *Result {
	result := &Result{}
	req, err := http.NewRequest(r.Method, r.URL, r.Body)
	if err != nil {
		result.Error = err
		return result
	}

	ctx := context.Background()
	if r.Context != nil {
		ctx = r.Context
	}

	host := "svc"
	if uri, parseErr := url.Parse(r.URL); parseErr == nil {
		host = uri.Hostname()
	}

	tr := otel.Tracer("pkg/client")
	ctx, span := tr.Start(ctx, r.Method+" "+r.URL, trace.WithAttributes(semconv.PeerServiceKey.String(host)))
	defer span.End()

	timeout := c.Timeout
	if r.Timeout != 0 {
		timeout = r.Timeout
	}
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(timeout))
		defer cancel()
	}
	req = req.WithContext(ctx)

	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}

	if r.OnRequestStart != nil {
		if err := r.OnRequestStart(req); err != nil {
			result.Error = err
			return result
		}
	}

	httpClient := c.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		result.Error = err
		if r.OnRequestFinished != nil {
			_ = r.OnRequestFinished(result)
		}
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	buff := new(bytes.Buffer)
	if _, err := io.Copy(buff, resp.Body); err != nil {
		span.RecordError(err)
		result.Error = err
		return result
	}

	result.Response.StatusCode = resp.StatusCode
	result.Response.Body = buff
	result.Header = resp.Header

	if r.OnRequestFinished != nil {
		if err := r.OnRequestFinished(result); err != nil {
			result.Error = err
			return result
		}
	}

	result.Request = req
	return result
}

// Result is the outcome of a single request attempt.
type Result struct {
	Response Response
	Request  *http.Request
	Header   http.Header
	Error    error
}

// Consume decodes the JSON response body into v. If the request itself
// failed, that error is returned. If the response status isn't 2xx, an
// *ErrorStatusNotOK is returned instead of decoding.
func (r *Result) Consume(v interface{}) error {
	if r.Error != nil {
		return r.Error
	}
	if r.Response.StatusCode < 200 || r.Response.StatusCode > 299 {
		log.Error().Int("status_code", r.Response.StatusCode).Msg("rest client: request did not return a 2xx status")
		return &ErrorStatusNotOK{Response: r.Response}
	}
	return r.Response.Consume(v)
}

// Response holds the raw status code and body of an HTTP response.
type Response struct {
	StatusCode int
	Body       *bytes.Buffer
}

// Consume decodes the body as JSON into v.
func (resp *Response) Consume(v interface{}) error {
	if resp.Body == nil {
		return ErrEmptyResponseBody
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// ErrorStatusNotOK indicates the server responded with a non-2xx status.
type ErrorStatusNotOK struct {
	Response
}

func (e *ErrorStatusNotOK) Error() string {
	body := ""
	if e.Body != nil {
		body = e.Body.String()
	}
	return fmt.Sprintf("rest client: response status %d, body: %s", e.StatusCode, body)
}

// Request describes a single HTTP call to make.
type Request struct {
	URL               string
	Method            string
	Body              io.Reader
	Headers           map[string]string
	Context           context.Context
	Timeout           int // seconds; 0 means no per-request timeout.
	OnRequestStart    OnRequestStartFunc
	OnRequestFinished OnRequestFinishedFunc
}

// OnRequestStartFunc is invoked just before the request is sent, e.g. to sign it.
type OnRequestStartFunc func(*http.Request) error

// OnRequestFinishedFunc is invoked after a response (or transport error) is received.
type OnRequestFinishedFunc func(*Result) error

// RequestBuilder builds a Request fluently, then executes it via Execute.
type RequestBuilder struct {
	request       Request
	client        DefaultRest
	retryStrategy RetryStrategy
}

// Do starts building a request for method and url using a fresh http.Client
// with no retry.
func Do(method, url string) *RequestBuilder {
	return &RequestBuilder{
		request: Request{
			URL:     url,
			Method:  method,
			Headers: map[string]string{},
		},
		retryStrategy: &NoRetry{},
		client:        &DefaultRestClient{Client: &http.Client{}},
	}
}

func Post(url string) *RequestBuilder   { return Do(http.MethodPost, url) }
func Get(url string) *RequestBuilder    { return Do(http.MethodGet, url) }
func Put(url string) *RequestBuilder    { return Do(http.MethodPut, url) }
func Patch(url string) *RequestBuilder  { return Do(http.MethodPatch, url) }
func Delete(url string) *RequestBuilder { return Do(http.MethodDelete, url) }

func (rb *RequestBuilder) WithBody(body io.Reader) *RequestBuilder {
	rb.request.Body = body
	return rb
}

func (rb *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder {
	rb.request.Context = ctx
	return rb
}

// WithClient overrides the DefaultRest used to perform the request (e.g. for
// tests, or to route through a client configured with a proxy). A nil or
// zero-value client is ignored.
func (rb *RequestBuilder) WithClient(c DefaultRest) *RequestBuilder {
	if c != nil && !reflect.ValueOf(c).IsZero() {
		rb.client = c
	}
	return rb
}

func (rb *RequestBuilder) WithRetryStrategy(rs RetryStrategy) *RequestBuilder {
	rb.retryStrategy = rs
	return rb
}

// WithTimeout sets the request timeout in seconds.
func (rb *RequestBuilder) WithTimeout(timeout int) *RequestBuilder {
	rb.request.Timeout = timeout
	return rb
}

func (rb *RequestBuilder) AddHeader(key, value string) *RequestBuilder {
	rb.request.Headers[key] = value
	return rb
}

func (rb *RequestBuilder) AddHeaders(headers map[string]string) *RequestBuilder {
	for k, v := range headers {
		rb.request.Headers[k] = v
	}
	return rb
}

func (rb *RequestBuilder) OnRequestStart(f OnRequestStartFunc) *RequestBuilder {
	rb.request.OnRequestStart = f
	return rb
}

func (rb *RequestBuilder) OnRequestFinished(f OnRequestFinishedFunc) *RequestBuilder {
	rb.request.OnRequestFinished = f
	return rb
}

// Execute performs the request, applying the configured retry strategy.
func (rb *RequestBuilder) Execute() *Result {
	return rb.retryStrategy.DoRequest(rb.client, rb.request)
}

// SetBasicAuth sets the Authorization header using HTTP Basic auth.
func (rb *RequestBuilder) SetBasicAuth(username, password string) *RequestBuilder {
	token := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	rb.request.Headers["Authorization"] = "Basic " + token
	return rb
}

// Rest is a fluent client bound to a base URL and default timeout.
type Rest interface {
	Delete(url string) *RequestBuilder
	Do(method string, url string) *RequestBuilder
	Get(url string) *RequestBuilder
	OnRequestFinished(fn OnRequestFinishedFunc) *APIRequestClient
	OnRequestStart(fn OnRequestStartFunc) *APIRequestClient
	Patch(url string) *RequestBuilder
	Post(url string) *RequestBuilder
	Put(url string) *RequestBuilder
	UseProxy(rawURL string) *APIRequestClient
}

// APIRequestClient is the default Rest implementation: every call is
// prefixed with baseURL and given the client's default timeout.
type APIRequestClient struct {
	name                string
	baseURL             string
	timeout             int
	httpClient          *http.Client
	onRequestStartFn    OnRequestStartFunc
	onRequestFinishedFn OnRequestFinishedFunc
}

// NewAPIRequestClient builds a Rest client for a named upstream service.
func NewAPIRequestClient(name string, baseURL string, timeout int) Rest {
	return &APIRequestClient{
		name:    name,
		baseURL: baseURL,
		timeout: timeout,
	}
}

// UseProxy routes every request made through this client via the given proxy
// URL, using a dedicated *http.Transport scoped to this client instance —
// unlike setting the HTTP_PROXY environment variable, this doesn't affect
// unrelated clients or goroutines and takes effect immediately. An invalid
// rawURL is logged and leaves the client unproxied.
func (arc *APIRequestClient) UseProxy(rawURL string) *APIRequestClient {
	proxyURL, err := url.Parse(rawURL)
	if err != nil {
		log.Error().Err(err).Str("proxy_url", rawURL).Msg("rest client: invalid proxy url, ignoring")
		return arc
	}

	arc.httpClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	return arc
}

func (arc *APIRequestClient) Do(method, path string) *RequestBuilder {
	rb := Do(method, arc.baseURL+path).WithTimeout(arc.timeout)
	if arc.httpClient != nil {
		rb.WithClient(&DefaultRestClient{Client: arc.httpClient})
	}
	if arc.onRequestStartFn != nil {
		rb.OnRequestStart(arc.onRequestStartFn)
	}
	if arc.onRequestFinishedFn != nil {
		rb.OnRequestFinished(arc.onRequestFinishedFn)
	}
	return rb
}

func (arc *APIRequestClient) OnRequestStart(fn OnRequestStartFunc) *APIRequestClient {
	arc.onRequestStartFn = fn
	return arc
}

func (arc *APIRequestClient) OnRequestFinished(fn OnRequestFinishedFunc) *APIRequestClient {
	arc.onRequestFinishedFn = fn
	return arc
}

func (arc *APIRequestClient) Post(path string) *RequestBuilder { return arc.Do(http.MethodPost, path) }
func (arc *APIRequestClient) Get(path string) *RequestBuilder  { return arc.Do(http.MethodGet, path) }
func (arc *APIRequestClient) Put(path string) *RequestBuilder  { return arc.Do(http.MethodPut, path) }
func (arc *APIRequestClient) Patch(path string) *RequestBuilder {
	return arc.Do(http.MethodPatch, path)
}
func (arc *APIRequestClient) Delete(path string) *RequestBuilder {
	return arc.Do(http.MethodDelete, path)
}

// RetryStrategy decides whether/how to retry a failed request.
type RetryStrategy interface {
	DoRequest(c DefaultRest, r Request) *Result
}

// DelayFunc computes the delay before retry attempt n.
type DelayFunc func(n uint) time.Duration

// RetryConfig accumulates retry bookkeeping (errors seen, buffered body for replay).
type RetryConfig struct {
	NumRetry  uint
	DelayType DelayFunc
	errorLog  []error
	bodyBytes []byte
}

// NoRetry performs the request exactly once.
type NoRetry struct{}

func (nr *NoRetry) DoRequest(c DefaultRest, r Request) *Result {
	return c.MakeRequest(r)
}

// RetryIfTimeout retries only on a client-side timeout, up to NumRetry times,
// with no delay between attempts.
type RetryIfTimeout struct {
	NoRetry
	RetryConfig
}

func (rt *RetryIfTimeout) DoRequest(c DefaultRest, r Request) *Result {
	return rt.makeRequest(rt.NumRetry, c, r)
}

func (rt *RetryIfTimeout) makeRequest(attempt uint, c DefaultRest, r Request) *Result {
	resp := rt.NoRetry.DoRequest(c, r)
	if resp.Error != nil && attempt > 0 {
		var netErr net.Error
		if errors.As(resp.Error, &netErr) && netErr.Timeout() {
			return rt.makeRequest(attempt-1, c, r)
		}
	}
	return resp
}

// RetryAllErrors retries on any error (including non-timeout ones), with
// exponential backoff between attempts, replaying the original request body.
type RetryAllErrors struct {
	NoRetry
	RetryConfig
}

// NewRetryAllErrors returns a RetryAllErrors with sensible defaults: 3
// retries, 100ms base exponential backoff.
func NewRetryAllErrors() *RetryAllErrors {
	return &RetryAllErrors{
		RetryConfig: RetryConfig{
			NumRetry:  3,
			DelayType: BackOffDelay(100 * time.Millisecond),
		},
	}
}

func (rt *RetryAllErrors) DoRequest(c DefaultRest, r Request) *Result {
	return rt.makeRequest(rt.NumRetry, c, r)
}

func (rt *RetryAllErrors) makeRequest(attempt uint, c DefaultRest, r Request) *Result {
	if r.Body != nil && rt.bodyBytes == nil {
		var err error
		rt.bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return &Result{Error: err}
		}
	}
	if rt.bodyBytes != nil {
		r.Body = bytes.NewReader(rt.bodyBytes)
	}

	resp := rt.NoRetry.DoRequest(c, r)
	if resp.Error != nil && attempt > 0 {
		rt.errorLog = append(rt.errorLog, resp.Error)
		time.Sleep(rt.DelayType(attempt))
		return rt.makeRequest(attempt-1, c, r)
	}
	return resp
}

// BackOffDelay returns a DelayFunc computing exponential backoff starting at delay.
func BackOffDelay(delay time.Duration) DelayFunc {
	return func(attempt uint) time.Duration {
		if attempt == 0 {
			return delay
		}
		return delay * (1 << (attempt - 1))
	}
}

// RetryError formats the accumulated errors from a RetryConfig for logging.
func RetryError(config *RetryConfig) string {
	lines := make([]string, 0, len(config.errorLog))
	for i, err := range config.errorLog {
		if err != nil {
			lines = append(lines, fmt.Sprintf("#%d: %s", i+1, err.Error()))
		}
	}
	return fmt.Sprintf("fail:\n%s", strings.Join(lines, "\n"))
}
