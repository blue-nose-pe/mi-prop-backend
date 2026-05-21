// Resend adapter: envia el OTP directo via Resend HTTP API. Evita el
// path HubSpot Workflow (filtrado por hs_marketable_status). Cuando se
// le inyecta un hubspot_service client, dispara en goroutine separada
// una sync best-effort del contacto a HubSpot para que el CRM no quede
// desactualizado — fallo de esa sync NO afecta entrega del OTP.
package otpsender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"time"

	"users_service/internal/core/ports"

	hubspotpb "hubspot_service/proto/gen"

	"google.golang.org/grpc/metadata"
)

// Resend implementa ports.OTPSender enviando el correo via api.resend.com.
type Resend struct {
	apiKey   string
	from     string
	replyTo  string
	http     *http.Client
	hubspot  hubspotpb.HubspotServiceClient // opcional; si !=nil, sync best-effort
}

var _ ports.OTPSender = (*Resend)(nil)

// NewResend construye el adapter. `from` debe ser un remitente verificado
// en Resend (en sandbox: "Mi Proposito UCSP <onboarding@resend.dev>").
// `replyTo` puede ser "" si no se quiere setear. `hubspotCli` opcional.
func NewResend(apiKey, from, replyTo string, hubspotCli hubspotpb.HubspotServiceClient) *Resend {
	return &Resend{
		apiKey:  apiKey,
		from:    from,
		replyTo: replyTo,
		http:    &http.Client{Timeout: 15 * time.Second},
		hubspot: hubspotCli,
	}
}

func (r *Resend) Send(ctx context.Context, email, plainOTP string) error {
	return r.send(ctx, email, plainOTP, "")
}

func (r *Resend) SendWithContact(ctx context.Context, in ports.OTPSendInput) error {
	displayName := firstNonEmpty(in.FirstName, in.Email)
	if err := r.send(ctx, in.Email, in.PlainOTP, displayName); err != nil {
		return err
	}
	// Best-effort: hidrata el contacto en HubSpot con datos completos para
	// que el CRM siga sirviendo de reportes — fallo aca NO bloquea login.
	if r.hubspot != nil {
		go r.syncHubspotBestEffort(ctx, in)
	}
	return nil
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

type resendResponse struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
}

func (r *Resend) send(ctx context.Context, email, plainOTP, displayName string) error {
	if r.apiKey == "" {
		return fmt.Errorf("resend api key is empty")
	}
	if email == "" || plainOTP == "" {
		return fmt.Errorf("resend: email or otp empty")
	}
	body := resendRequest{
		From:    r.from,
		To:      []string{email},
		Subject: "Tu codigo de verificacion - Mi Proposito UCSP",
		HTML:    buildOTPHTML(displayName, plainOTP),
		ReplyTo: r.replyTo,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("resend marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("resend req: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("resend http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend status=%d body=%s", resp.StatusCode, string(raw))
	}
	var parsed resendResponse
	_ = json.Unmarshal(raw, &parsed)
	log.Printf("[otpsender:resend] sent email_id=%s to=%s", parsed.ID, email)
	return nil
}

func (r *Resend) syncHubspotBestEffort(parent context.Context, in ports.OTPSendInput) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Reenvia x-correlation-id si esta presente.
	if md, ok := metadata.FromIncomingContext(parent); ok {
		if v := md.Get("x-correlation-id"); len(v) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-correlation-id", v[0])
		}
	}
	_, err := r.hubspot.SyncStudentContact(ctx, &hubspotpb.SyncStudentContactRequest{
		Email:          in.Email,
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		DocumentNumber: in.DocumentNumber,
		Phone:          in.Phone,
		SchoolIntId:    in.SchoolIntID,
		SchoolRecordId: in.SchoolRecordID,
	})
	if err != nil {
		log.Printf("[otpsender:resend] hubspot sync best-effort failed email=%s err=%v", in.Email, err)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// buildOTPHTML genera el cuerpo HTML del correo. Template autocontenido
// (sin assets externos) para evitar problemas de bloqueo de imagenes en
// clientes de email. Disenado mobile-first.
func buildOTPHTML(displayName, otp string) string {
	greeting := "Hola,"
	if displayName != "" {
		greeting = fmt.Sprintf("Hola %s,", html.EscapeString(displayName))
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
        <tr><td style="font-size:15px;line-height:22px;padding-bottom:24px;">Usa este codigo para confirmar tu correo y entrar al simulacro. Es valido por <strong>10 minutos</strong>.</td></tr>
        <tr><td align="center" style="padding:8px 0 24px 0;">
          <div style="display:inline-block;font-family:'Courier New',monospace;font-size:36px;font-weight:700;letter-spacing:8px;background:#f0f4ff;color:#0d3a8a;border-radius:8px;padding:16px 24px;">` + html.EscapeString(otp) + `</div>
        </td></tr>
        <tr><td style="font-size:13px;line-height:20px;color:#666;padding-top:8px;">Si no solicitaste este codigo puedes ignorar el mensaje — nadie podra entrar sin el.</td></tr>
        <tr><td style="font-size:12px;line-height:18px;color:#999;padding-top:24px;border-top:1px solid #eee;margin-top:24px;">Universidad Catolica San Pablo - Programa Mi Proposito</td></tr>
      </table>
    </td></tr>
  </table>
</body></html>`
}
