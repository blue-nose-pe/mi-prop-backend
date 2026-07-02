// Helpers compartidos para armar el correo de OTP (cuerpo HTML + utilidades).
// Los usa el adapter SMTP (Gmail). Antes vivían en resend.go junto al adapter
// Resend; al retirar Resend (código muerto, envío definitivo es por Gmail) se
// movieron aquí para que sigan disponibles sin el proveedor de terceros.
package otpsender

import (
	"fmt"
	"html"
)

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// buildOTPHTML genera el cuerpo HTML del correo. Template autocontenido
// (sin assets externos) para evitar problemas de bloqueo de imagenes en
// clientes de email. Disenado mobile-first.
func buildOTPHTML(displayName, otp, magicLinkURL string) string {
	greeting := "Hola,"
	if displayName != "" {
		greeting = fmt.Sprintf("Hola %s,", html.EscapeString(displayName))
	}
	// Intro: con magic link (masivo) el mensaje destaca el boton; sin link
	// (login normal) destaca el codigo.
	intro := "Usa este codigo para confirmar tu correo y entrar al simulacro. Es valido por <strong>10 minutos</strong>."
	if magicLinkURL != "" {
		intro = "Ya estas inscrito al Examen Simulacro UCSP. Haz clic en el boton para ingresar a tu examen (valido por <strong>10 minutos</strong>). Si el boton no funciona, usa el codigo de abajo."
	}
	// Boton de acceso directo (magic link) — el href ya viene armado por el
	// command con el OTP embebido, asi que el alumno entra sin escribir nada.
	button := ""
	if magicLinkURL != "" {
		button = `<tr><td align="center" style="padding:8px 0 20px 0;">
          <a href="` + html.EscapeString(magicLinkURL) + `" style="display:inline-block;background:#0046b8;color:#ffffff;text-decoration:none;font-size:16px;font-weight:700;border-radius:8px;padding:14px 32px;">Ingresar a mi examen</a>
        </td></tr>`
	}
	return `<!doctype html>
<html lang="es">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#f6f7fb;font-family:Helvetica,Arial,sans-serif;color:#1a1a1a;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="background:#f6f7fb;padding:24px 0;">
    <tr><td align="center">
      <table role="presentation" width="480" cellspacing="0" cellpadding="0" border="0" style="background:#ffffff;border-radius:12px;padding:32px;max-width:480px;">
        <tr><td style="font-size:18px;font-weight:600;color:#0d3a8a;padding-bottom:8px;">Mi Proposito UCSP</td></tr>
        <tr><td style="font-size:15px;line-height:22px;padding-bottom:16px;">` + greeting + `</td></tr>
        <tr><td style="font-size:15px;line-height:22px;padding-bottom:24px;">` + intro + `</td></tr>
        ` + button + `
        <tr><td align="center" style="padding:8px 0 24px 0;">
          <div style="display:inline-block;font-family:'Courier New',monospace;font-size:36px;font-weight:700;letter-spacing:8px;background:#f0f4ff;color:#0d3a8a;border-radius:8px;padding:16px 24px;">` + html.EscapeString(otp) + `</div>
        </td></tr>
        <tr><td style="font-size:13px;line-height:20px;color:#666;padding-top:8px;">Si no solicitaste este mensaje puedes ignorarlo — nadie podra entrar sin el.</td></tr>
        <tr><td style="font-size:12px;line-height:18px;color:#999;padding-top:24px;border-top:1px solid #eee;margin-top:24px;">Universidad Catolica San Pablo - Programa Mi Proposito</td></tr>
      </table>
    </td></tr>
  </table>
</body></html>`
}
