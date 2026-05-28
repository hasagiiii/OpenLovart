// Package email defines the Sender interface used by the auth service to
// dispatch transactional mail (verify-email, password-reset). The interface
// keeps the auth service decoupled from any specific transport so tests can
// use an in-memory fake and production can wire SMTP (smtp.go) or another
// provider in the future.
package email

import "context"

// Sender is the minimal contract the auth service depends on.
//
// Implementations:
//   - smtp.go        — gomail-backed SMTP, used in dev (Mailpit) and prod.
//
// Both `html` and `text` are the rendered bodies; the implementation should
// emit a multipart/alternative message containing both.
type Sender interface {
	Send(ctx context.Context, to, subject, html, text string) error
}
