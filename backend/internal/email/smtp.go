package email

import (
	"context"
	"fmt"

	"gopkg.in/gomail.v2"

	"github.com/jiantaoli/openlovart/backend/internal/config"
)

// SMTPSender dispatches mail via gomail.v2. Config is captured at
// construction time; the SMTPSender itself is safe for concurrent use
// because every Send opens its own dialer connection.
type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

// NewSMTPSender constructs an SMTPSender from cfg. Returns an error when
// required fields are missing (host, port, from). When username is empty
// the dialer skips AUTH (matches the Mailpit dev case).
func NewSMTPSender(cfg *config.Config) (*SMTPSender, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("email: SMTP_HOST is required")
	}
	if cfg.SMTPPort <= 0 {
		return nil, fmt.Errorf("email: SMTP_PORT must be > 0")
	}
	if cfg.SMTPFrom == "" {
		return nil, fmt.Errorf("email: SMTP_FROM is required")
	}
	return &SMTPSender{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		from:     cfg.SMTPFrom,
	}, nil
}

// Send composes a multipart/alternative message containing the supplied
// HTML and text bodies, then dials the SMTP server and delivers it.
//
// We honor ctx by checking for cancellation before any blocking call;
// gomail itself does not accept a context, so we cannot interrupt an
// in-flight DialAndSend, but a long-cancelled ctx will short-circuit before
// we open the connection.
func (s *SMTPSender) Send(ctx context.Context, to, subject, html, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := gomail.NewMessage()
	m.SetHeader("From", s.from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", text)
	m.AddAlternative("text/html", html)

	dialer := gomail.NewDialer(s.host, s.port, s.username, s.password)
	// Mailpit (and many internal relays) accept plaintext on a non-TLS port;
	// gomail upgrades to STARTTLS automatically when the server advertises
	// it, so we leave TLS handling to the dialer's defaults.

	if err := dialer.DialAndSend(m); err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	return nil
}
