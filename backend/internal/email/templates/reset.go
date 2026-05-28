package templates

import "fmt"

// ResetData binds reset.html / reset.txt below.
type ResetData struct {
	// Link is the fully-qualified URL the user clicks to choose a new
	// password (e.g. PASSWORD_RESET_BASE_URL + "?token=...").
	Link string
}

const resetSubject = "Reset your OpenLovart password"

const resetHTML = `<!doctype html>
<html><body style="font-family: -apple-system,Segoe UI,Helvetica,Arial,sans-serif;line-height:1.5;color:#111">
<p>We received a request to reset the password on your OpenLovart account.</p>
<p>Click the link below to choose a new password. The link expires in 30 minutes.</p>
<p><a href="{{ .Link }}" style="display:inline-block;padding:10px 18px;background:#111;color:#fff;border-radius:6px;text-decoration:none">Reset password</a></p>
<p>Or copy and paste this URL into your browser:<br><code>{{ .Link }}</code></p>
<p>If this wasn&rsquo;t you, you can safely ignore this email — your password will not change.</p>
</body></html>`

const resetText = `We received a request to reset the password on your OpenLovart account.

Visit this link to choose a new password (expires in 30 minutes):

{{ .Link }}

If this wasn't you, ignore this email — your password will not change.
`

// RenderReset produces the (subject, html, text) triple for the
// password-reset message.
func RenderReset(data ResetData) (subject, html, text string, err error) {
	html, err = render("reset.html", resetHTML, data)
	if err != nil {
		return "", "", "", fmt.Errorf("templates: reset html: %w", err)
	}
	text, err = render("reset.txt", resetText, data)
	if err != nil {
		return "", "", "", fmt.Errorf("templates: reset text: %w", err)
	}
	return resetSubject, html, text, nil
}
