// Package twilio is an sms.SmsSender adapter backed by Twilio's REST API via
// github.com/twilio/twilio-go. The twilio-go types stay entirely internal:
// callers construct a Sender from a Config and use it through the
// sms.SmsSender port.
package twilio

import (
	"context"
	"net/http"
	"strings"

	twiliogo "github.com/twilio/twilio-go"
	tclient "github.com/twilio/twilio-go/client"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"

	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
)

// Config holds the credentials and sender identity for the Twilio adapter.
// AccountSID, AuthToken, and FromNumber are required.
type Config struct {
	// AccountSID is the Twilio Account SID (starts with "AC").
	AccountSID string
	// AuthToken is the Twilio auth token paired with AccountSID.
	AuthToken string
	// FromNumber is the Twilio-owned sender number, ideally E.164.
	FromNumber string
	// HTTPClient overrides the HTTP client used for API calls (custom
	// timeout, proxy, or instrumented transport). Defaults to the Twilio
	// SDK's own client when nil.
	HTTPClient *http.Client
}

func (c Config) validate() error {
	if strings.TrimSpace(c.AccountSID) == "" ||
		strings.TrimSpace(c.AuthToken) == "" ||
		strings.TrimSpace(c.FromNumber) == "" {
		return sms.ErrMissingConfig
	}
	return nil
}

// Sender is an sms.SmsSender backed by Twilio's REST API.
type Sender struct {
	api  *openapi.ApiService
	from string
}

var _ sms.SmsSender = (*Sender)(nil)

// NewClient constructs a Sender from cfg. It returns an error wrapping
// sms.ErrMissingConfig if any required field is blank.
func NewClient(cfg Config) (*Sender, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	baseClient := &tclient.Client{
		Credentials: tclient.NewCredentials(cfg.AccountSID, cfg.AuthToken),
		HTTPClient:  cfg.HTTPClient,
	}
	baseClient.SetAccountSid(cfg.AccountSID)

	restClient := twiliogo.NewRestClientWithParams(twiliogo.ClientParams{
		Username:   cfg.AccountSID,
		Password:   cfg.AuthToken,
		AccountSid: cfg.AccountSID,
		Client:     baseClient,
	})

	return &Sender{api: restClient.Api, from: cfg.FromNumber}, nil
}

// Send implements sms.SmsSender: it validates msg, maps it to Twilio's
// create-message request, and delivers it, returning the Twilio message SID
// on success. A cancellation during the call takes priority over a
// transport error, since it is the more useful signal to the caller.
func (s *Sender) Send(ctx context.Context, msg sms.SmsMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := msg.Validate(); err != nil {
		return "", err
	}

	params := buildCreateMessageParams(s.from, msg)

	resp, err := s.api.CreateMessage(params)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", sms.SendFailure(err, "twilio: create message")
	}
	if resp == nil || resp.Sid == nil || *resp.Sid == "" {
		return "", sms.SendFailure(nil, "twilio: provider returned no message sid")
	}

	return *resp.Sid, nil
}

// buildCreateMessageParams maps a validated domain message to Twilio's
// request parameters. Sending uses an explicit From number, so
// MessagingServiceSid stays unset. IdempotencyKey is not forwarded: the
// v2010 Messages API has no such field, so it stays a caller-side
// de-duplication key.
func buildCreateMessageParams(from string, msg sms.SmsMessage) *openapi.CreateMessageParams {
	params := &openapi.CreateMessageParams{}
	params.SetTo(msg.To)
	params.SetFrom(from)
	params.SetBody(msg.Body)
	return params
}
