// Package fcm is a push.PushSender adapter backed by Firebase Cloud
// Messaging via firebase.google.com/go/v4. The Firebase SDK types stay
// entirely internal: callers construct a Sender from a Config and use it
// through the push.PushSender port.
//
// The PushMessage -> *messaging.Message translation is isolated in the
// pure, exported BuildMessage, so the mapping is table-testable without any
// network or credentials.
package fcm

import (
	"context"
	"fmt"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"

	"github.com/adehikmatfr/go-pkg/v2/notification/push"
)

// Config identifies the Firebase project the Sender talks to.
type Config struct {
	// ProjectID is the Firebase/GCP project ID. Required.
	ProjectID string
}

// Sender is a push.PushSender backed by Firebase Cloud Messaging.
type Sender struct {
	client *messaging.Client
}

var _ push.PushSender = (*Sender)(nil)

// NewClient builds a Sender for cfg.ProjectID, applying opts to the
// underlying Google API client (for example option.WithCredentialsFile for
// production, or option.WithEndpoint plus option.WithoutAuthentication to
// point at a fake server in tests). It returns an error wrapping
// push.ErrInvalidConfig if ProjectID is blank.
func NewClient(ctx context.Context, cfg Config, opts ...option.ClientOption) (push.PushSender, error) {
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return nil, fmt.Errorf("%w: ProjectID must not be empty", push.ErrInvalidConfig)
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: cfg.ProjectID}, opts...)
	if err != nil {
		return nil, fmt.Errorf("fcm: init firebase app: %w", err)
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("fcm: init messaging client: %w", err)
	}
	return &Sender{client: client}, nil
}

// Send implements push.PushSender: it validates msg, maps it to an FCM
// message, and delivers it, returning the FCM message ID on success. A
// provider failure is classified into a push sentinel error via
// classifyError.
func (s *Sender) Send(ctx context.Context, msg push.PushMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := msg.Validate(); err != nil {
		return "", err
	}
	fcmMsg, err := BuildMessage(msg)
	if err != nil {
		return "", err
	}
	id, err := s.client.Send(ctx, fcmMsg)
	if err != nil {
		return "", classifyError(err)
	}
	return id, nil
}

// collapseKeyData is the data-map key the idempotency key is echoed under,
// so the client app can dedup locally in addition to the platform collapse
// key/header.
const collapseKeyData = "idempotency_key"

// BuildMessage translates msg into a Firebase *messaging.Message. It is
// side-effect-free and re-validates msg, so it is safe to call standalone.
//
// Token/Topic map to the FCM recipient fields; Title/Body become a
// Notification, omitted when both are empty for a data-only message; Data
// is copied rather than aliased. Priority maps to android "high"/"normal"
// and apns-priority "10"/"5". IdempotencyKey becomes the Android collapse
// key, the apns-collapse-id header, and a data entry.
func BuildMessage(msg push.PushMessage) (*messaging.Message, error) {
	if err := msg.Validate(); err != nil {
		return nil, err
	}

	out := &messaging.Message{
		Token: msg.Token, //nolint:staticcheck // Token is still the FCM v1 API's device-registration-token field; push.PushMessage models that concept as Token, not the newer Fid.
		Topic: msg.Topic,
	}

	if msg.Title != "" || msg.Body != "" {
		out.Notification = &messaging.Notification{
			Title: msg.Title,
			Body:  msg.Body,
		}
	}

	if len(msg.Data) > 0 || msg.IdempotencyKey != "" {
		data := make(map[string]string, len(msg.Data)+1)
		for k, v := range msg.Data {
			data[k] = v
		}
		if msg.IdempotencyKey != "" {
			data[collapseKeyData] = msg.IdempotencyKey
		}
		out.Data = data
	}

	out.Android = buildAndroid(msg)
	out.APNS = buildAPNS(msg)

	return out, nil
}

// buildAndroid maps priority and idempotency onto an AndroidConfig.
func buildAndroid(msg push.PushMessage) *messaging.AndroidConfig {
	cfg := &messaging.AndroidConfig{Priority: androidPriority(msg.Priority)}
	if msg.IdempotencyKey != "" {
		cfg.CollapseKey = msg.IdempotencyKey
	}
	return cfg
}

// buildAPNS maps priority and idempotency onto an APNSConfig via headers.
func buildAPNS(msg push.PushMessage) *messaging.APNSConfig {
	headers := map[string]string{"apns-priority": apnsPriority(msg.Priority)}
	if msg.IdempotencyKey != "" {
		headers["apns-collapse-id"] = msg.IdempotencyKey
	}
	return &messaging.APNSConfig{Headers: headers}
}

// androidPriority maps a push.Priority to FCM's android priority string.
func androidPriority(p push.Priority) string {
	if p == push.PriorityHigh {
		return "high"
	}
	return "normal"
}

// apnsPriority maps a push.Priority to the apns-priority header value ("10"
// = send immediately, "5" = power-considerate).
func apnsPriority(p push.Priority) string {
	if p == push.PriorityHigh {
		return "10"
	}
	return "5"
}

// classifyError translates a firebase messaging error into a push sentinel
// error, preserving the original in the chain via %w so errors.Is(err,
// cause) also holds.
func classifyError(err error) error {
	switch {
	case messaging.IsInvalidArgument(err):
		return fmt.Errorf("%w: %w", push.ErrInvalidToken, err)
	case messaging.IsUnregistered(err):
		return fmt.Errorf("%w: %w", push.ErrUnregistered, err)
	case messaging.IsUnavailable(err), messaging.IsInternal(err):
		return fmt.Errorf("%w: %w", push.ErrUnavailable, err)
	default:
		return fmt.Errorf("fcm: send: %w", err)
	}
}
