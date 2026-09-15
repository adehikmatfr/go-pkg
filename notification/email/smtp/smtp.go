// Package smtp is an email.EmailSender adapter backed by an SMTP server via
// github.com/wneessen/go-mail. The go-mail types stay entirely internal:
// callers construct a Sender from a Config and use it through the
// email.EmailSender port.
package smtp

import (
	"context"
	"fmt"
	"strings"

	mail "github.com/wneessen/go-mail"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
)

// TLSMode describes the transport security policy for the SMTP connection.
type TLSMode int

const (
	// TLSMandatory requires STARTTLS; the connection fails if the server
	// does not support it. This is the default and the recommended setting.
	TLSMandatory TLSMode = iota
	// TLSOpportunistic attempts STARTTLS but falls back to plaintext if the
	// server does not offer it. Use only for trusted local relays.
	TLSOpportunistic
	// TLSNone disables STARTTLS entirely (plaintext). Intended for local
	// development relays.
	TLSNone
	// TLSImplicit uses implicit TLS (SMTPS) for the whole connection,
	// typically on port 465.
	TLSImplicit
)

// AuthMode selects the SMTP authentication mechanism.
type AuthMode int

const (
	// AuthNone performs no SMTP authentication.
	AuthNone AuthMode = iota
	// AuthPlain uses the PLAIN SASL mechanism.
	AuthPlain
	// AuthLogin uses the LOGIN SASL mechanism.
	AuthLogin
	// AuthCRAMMD5 uses the CRAM-MD5 SASL mechanism.
	AuthCRAMMD5
)

// Config configures a Sender. Password is held only for the sender's
// lifetime and is never logged by this package.
type Config struct {
	// Host is the SMTP server hostname. Required.
	Host string
	// Port is the SMTP server port. If zero, go-mail selects a default for
	// the TLS mode (587 for STARTTLS, 465 for implicit TLS).
	Port int
	// Username and Password are the SMTP credentials. Required when Auth is
	// not AuthNone.
	Username string
	Password string
	// Auth selects the authentication mechanism. Defaults to AuthNone.
	Auth AuthMode
	// TLS selects the transport security policy. Defaults to TLSMandatory.
	TLS TLSMode
	// DefaultFrom is used as the sender for any EmailMessage that does not
	// set its own From.
	DefaultFrom email.Address
}

// Sender is an email.EmailSender backed by an SMTP server.
type Sender struct {
	cfg    Config
	client *mail.Client
}

var _ email.EmailSender = (*Sender)(nil)

// NewClient constructs a Sender from cfg. It validates the configuration and
// pre-builds the go-mail client eagerly, returning an error wrapping
// email.ErrInvalidConfig on failure, so misconfiguration fails at
// construction rather than at first send.
func NewClient(cfg Config) (*Sender, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("%w: empty SMTP host", email.ErrInvalidConfig)
	}
	if cfg.Auth != AuthNone && (cfg.Username == "" || cfg.Password == "") {
		return nil, fmt.Errorf("%w: auth enabled but username/password missing", email.ErrInvalidConfig)
	}
	if cfg.DefaultFrom.Email != "" {
		if err := cfg.DefaultFrom.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid DefaultFrom: %w", email.ErrInvalidConfig, err)
		}
	}

	client, err := newMailClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Sender{cfg: cfg, client: client}, nil
}

// newMailClient assembles the internal go-mail client from cfg, kept
// separate so construction can be validated without dialing.
func newMailClient(cfg Config) (*mail.Client, error) {
	// go-mail defaults the HELO/EHLO greeting to the OS hostname, which is
	// not guaranteed to be a valid domain/address literal; pin it so the
	// greeting is always accepted regardless of the host's own hostname.
	opts := []mail.Option{mail.WithHELO("localhost")}
	if cfg.Port > 0 {
		opts = append(opts, mail.WithPort(cfg.Port))
	}

	switch cfg.TLS {
	case TLSMandatory:
		opts = append(opts, mail.WithTLSPortPolicy(mail.TLSMandatory))
	case TLSOpportunistic:
		opts = append(opts, mail.WithTLSPortPolicy(mail.TLSOpportunistic))
	case TLSNone:
		opts = append(opts, mail.WithTLSPortPolicy(mail.NoTLS))
	case TLSImplicit:
		opts = append(opts, mail.WithSSLPort(false))
	default:
		return nil, fmt.Errorf("%w: unknown TLS mode %d", email.ErrInvalidConfig, cfg.TLS)
	}

	switch cfg.Auth {
	case AuthNone:
		// no auth options
	case AuthPlain:
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthPlain))
	case AuthLogin:
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthLogin))
	case AuthCRAMMD5:
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthCramMD5))
	default:
		return nil, fmt.Errorf("%w: unknown auth mode %d", email.ErrInvalidConfig, cfg.Auth)
	}
	if cfg.Auth != AuthNone {
		opts = append(opts, mail.WithUsername(cfg.Username), mail.WithPassword(cfg.Password))
	}

	client, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: building SMTP client: %w", email.ErrInvalidConfig, err)
	}
	return client, nil
}

// Send implements email.EmailSender: it validates msg, builds the MIME
// message and dials the configured server. Validation failures wrap
// email.ErrInvalidMessage; transport failures wrap email.ErrSendFailed.
func (s *Sender) Send(ctx context.Context, msg email.EmailMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	gomsg, err := s.buildMessage(msg)
	if err != nil {
		return err
	}
	if err := s.client.DialAndSendWithContext(ctx, gomsg); err != nil {
		return fmt.Errorf("%w: %w", email.ErrSendFailed, err)
	}
	return nil
}

// buildMessage validates msg and translates it into an internal go-mail
// *Msg. It is the single place go-mail message types are constructed.
func (s *Sender) buildMessage(msg email.EmailMessage) (*mail.Msg, error) {
	if err := msg.Validate(); err != nil {
		return nil, err
	}

	from := msg.From
	if from.Email == "" {
		from = s.cfg.DefaultFrom
	}
	if from.Email == "" {
		return nil, fmt.Errorf("%w: no From on message and no DefaultFrom configured", email.ErrInvalidMessage)
	}

	m := mail.NewMsg()
	if err := m.From(from.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid from address: %w", email.ErrInvalidMessage, err)
	}

	tos := make([]string, 0, len(msg.To))
	for _, to := range msg.To {
		tos = append(tos, to.String())
	}
	if err := m.To(tos...); err != nil {
		return nil, fmt.Errorf("%w: invalid recipient address: %w", email.ErrInvalidMessage, err)
	}

	m.Subject(msg.Subject)
	// Use the idempotency key as the Message-ID so retries are de-duplicable
	// by the receiving infrastructure.
	m.SetMessageIDWithValue(msg.IdempotencyKey)

	for k, v := range msg.Headers {
		if email.IsReservedHeader(k) {
			continue
		}
		m.SetGenHeader(mail.Header(k), v)
	}

	html := strings.TrimSpace(msg.HTMLBody)
	text := strings.TrimSpace(msg.TextBody)
	switch {
	case html != "" && text != "":
		// multipart/alternative orders parts least to most preferred (RFC
		// 2046 §5.1.4); a client renders the last alternative it
		// understands, so text/plain goes first and text/html last.
		m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
		m.AddAlternativeString(mail.TypeTextHTML, msg.HTMLBody)
	case html != "":
		m.SetBodyString(mail.TypeTextHTML, msg.HTMLBody)
	default:
		m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
	}

	return m, nil
}
