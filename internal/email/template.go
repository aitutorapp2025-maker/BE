package email

import (
	"fmt"
	"strings"
)

// Wrap renders body HTML inside the branded Vaha AI email layout (landing
// theme: forest green + gold on parchment). [heading] is the big title shown at
// the top of the card; [bodyHTML] is trusted HTML for the message body.
func Wrap(heading, bodyHTML string) string {
	const (
		primary   = "#123F36"
		secondary = "#E8A33D"
		bg        = "#FAF6EC"
		surface   = "#FFFFFF"
		text      = "#16302B"
		muted     = "#5E6B63"
		border    = "#EAE2D2"
	)

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:%[3]s;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:%[3]s;padding:24px 12px;font-family:Arial,Helvetica,sans-serif;">
    <tr><td align="center">
      <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:560px;background:%[4]s;border:1px solid %[7]s;border-radius:16px;overflow:hidden;">
        <!-- header -->
        <tr><td style="background:%[1]s;padding:22px 28px;">
          <span style="color:#ffffff;font-size:20px;font-weight:800;letter-spacing:.2px;">Vaha&nbsp;AI</span>
          <span style="color:%[2]s;font-size:20px;font-weight:800;">.</span>
        </td></tr>
        <!-- body -->
        <tr><td style="padding:28px;">
          <h1 style="margin:0 0 14px;color:%[1]s;font-size:22px;line-height:1.25;">%[5]s</h1>
          <div style="color:%[6]s;font-size:15px;line-height:1.6;">%[8]s</div>
        </td></tr>
        <!-- footer -->
        <tr><td style="padding:18px 28px;border-top:1px solid %[7]s;">
          <div style="color:%[9]s;font-size:12px;line-height:1.5;">
            You're receiving this email from Vaha AI — your child's personal AI tutor.<br>
            &copy; Vaha AI. All rights reserved.
          </div>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`,
		primary,   // 1
		secondary, // 2
		bg,        // 3
		surface,   // 4
		heading,   // 5
		text,      // 6
		border,    // 7
		bodyHTML,  // 8
		muted,     // 9
	)
}

// Escape HTML-escapes a plain string for safe interpolation into a template.
func Escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
