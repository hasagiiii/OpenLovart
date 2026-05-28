// Package templates renders transactional email bodies. Each template
// returns the (subject, html, text) triple ready to feed into
// email.Sender.Send. The plain-text variant is intentionally exhaustive
// (mirrors the HTML's information) so non-HTML clients see the same content.
package templates

import (
	"bytes"
	"fmt"
	"text/template"
)

// VerifyData is the binding for verify.html / verify.txt below.
type VerifyData struct {
	// Link is the fully-qualified URL the user clicks to verify their
	// address (e.g. EMAIL_VERIFY_BASE_URL + "?token=...").
	Link string
}

const verifySubject = "Verify your OpenLovart email"

const verifyHTML = `<!doctype html>
<html><body style="font-family: -apple-system,Segoe UI,Helvetica,Arial,sans-serif;line-height:1.5;color:#111">
<p>Welcome to OpenLovart!</p>
<p>Please confirm this is your email address by clicking the link below:</p>
<p><a href="{{ .Link }}" style="display:inline-block;padding:10px 18px;background:#111;color:#fff;border-radius:6px;text-decoration:none">Verify email</a></p>
<p>Or copy and paste this URL into your browser:<br><code>{{ .Link }}</code></p>
<p>If this wasn&rsquo;t you, you can safely ignore this email — your account will not be activated.</p>
</body></html>`

const verifyText = `Welcome to OpenLovart!

Please confirm this is your email address by visiting:

{{ .Link }}

If this wasn't you, ignore this email — your account will not be activated.
`

// RenderVerify produces the (subject, html, text) triple for the
// verify-email message.
func RenderVerify(data VerifyData) (subject, html, text string, err error) {
	html, err = render("verify.html", verifyHTML, data)
	if err != nil {
		return "", "", "", fmt.Errorf("templates: verify html: %w", err)
	}
	text, err = render("verify.txt", verifyText, data)
	if err != nil {
		return "", "", "", fmt.Errorf("templates: verify text: %w", err)
	}
	return verifySubject, html, text, nil
}

// render is a small generic helper: parse src as a text/template, execute
// against data, return the rendered string.
func render(name, src string, data any) (string, error) {
	tpl, err := template.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
