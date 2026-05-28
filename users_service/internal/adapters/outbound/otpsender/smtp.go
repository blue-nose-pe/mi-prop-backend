// SMTP adapter: envia el OTP via SMTP estandar (PLAIN auth + STARTTLS).
// Pensado para SMTP de Gmail / Outlook / cualquier provider con SMTP AUTH.
//
// Gmail config:
//   SMTP_HOST=smtp.gmail.com
//   SMTP_PORT=587
//   SMTP_USERNAME=tu-email@gmail.com
//   SMTP_PASSWORD=<App Password de 16 chars de https://myaccount.google.com/apppasswords>
//   SMTP_FROM="Mi Proposito UCSP <tu-email@gmail.com>"
//
// Gmail re-escribe FROM al email autenticado si difiere, asi que el alias
// solo controla el display name. Si necesitan otro FROM, deben verificar
// dominio en Resend (path correcto a futuro).
package otpsender

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	hubspotpb "hubspot_service/proto/gen"
	"users_service/internal/core/ports"

	"google.golang.org/grpc/metadata"
)

// SMTP implementa ports.OTPSender enviando el correo via SMTP AUTH PLAIN
// sobre STARTTLS (puerto 587).
type SMTP struct {
	host     string
	port     int
	username string
	password string
	from     string
	hubspot  hubspotpb.HubspotServiceClient // opcional; sync best-effort post-send
}

var _ ports.OTPSender = (*SMTP)(nil)

func NewSMTP(host string, port int, username, password, from string, hubspotCli hubspotpb.HubspotServiceClient) *SMTP {
	return &SMTP{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		hubspot:  hubspotCli,
	}
}

func (s *SMTP) Send(ctx context.Context, email, plainOTP string) error {
	return s.send(ctx, email, plainOTP, "")
}

func (s *SMTP) SendWithContact(ctx context.Context, in ports.OTPSendInput) error {
	displayName := firstNonEmpty(in.FirstName, in.Email)
	if err := s.send(ctx, in.Email, in.PlainOTP, displayName); err != nil {
		return err
	}
	if s.hubspot != nil {
		go s.syncHubspotBestEffort(ctx, in)
	}
	return nil
}

func (s *SMTP) send(ctx context.Context, email, plainOTP, displayName string) error {
	if s.host == "" || s.username == "" || s.password == "" || s.from == "" {
		return fmt.Errorf("smtp: missing host/username/password/from")
	}
	if email == "" || plainOTP == "" {
		return fmt.Errorf("smtp: email or otp empty")
	}

	subject := "Tu codigo de verificacion - Mi Proposito UCSP"
	body := buildOTPHTML(displayName, plainOTP)
	msg := buildMIME(s.from, email, subject, body)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.username, s.password, s.host)

	// Dial con timeout respetando ctx — net/smtp.SendMail no acepta context
	// asi que armamos el cliente a mano para poder cancelar en caso de
	// que el ctx parent se cierre durante el handshake.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}
	d := net.Dialer{Deadline: deadline}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if err := client.Hello("miproposito"); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	// STARTTLS obligatorio para Gmail/Outlook (rechazan auth en clear).
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: s.host}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	// Gmail acepta el FROM del MAIL FROM solo si coincide con el username
	// auth. Si difiere, lo re-escribe — no nos importa: el header From: del
	// MIME es el que ve el usuario, y ahi va el display name.
	if err := client.Mail(extractAddr(s.from)); err != nil {
		return fmt.Errorf("smtp mail-from: %w", err)
	}
	if err := client.Rcpt(email); err != nil {
		return fmt.Errorf("smtp rcpt-to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data-close: %w", err)
	}
	if err := client.Quit(); err != nil {
		// QUIT failing al final es benigno — el correo ya se entrego al servidor.
		log.Printf("[otpsender:smtp] quit warning: %v", err)
	}
	log.Printf("[otpsender:smtp] sent to=%s", email)
	return nil
}

func (s *SMTP) syncHubspotBestEffort(parent context.Context, in ports.OTPSendInput) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if md, ok := metadata.FromIncomingContext(parent); ok {
		if v := md.Get("x-correlation-id"); len(v) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-correlation-id", v[0])
		}
	}
	_, err := s.hubspot.SyncStudentContact(ctx, &hubspotpb.SyncStudentContactRequest{
		Email:          in.Email,
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		DocumentNumber: in.DocumentNumber,
		Phone:          in.Phone,
		SchoolIntId:    in.SchoolIntID,
		SchoolRecordId: in.SchoolRecordID,
	})
	if err != nil {
		log.Printf("[otpsender:smtp] hubspot sync best-effort failed email=%s err=%v", in.Email, err)
	}
}

// buildMIME arma un correo MIME-multipart minimo con HTML inline.
// Headers obligatorios para entrega: From, To, Subject, MIME-Version,
// Content-Type. Sin Date Gmail acepta igual (lo sello el receptor), pero
// lo incluimos por higiene.
func buildMIME(from, to, subject, html string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(html)
	return sb.String()
}

// extractAddr saca el email "limpio" de un FROM tipo "Name <email@host>".
// Gmail rechaza MAIL FROM con angle brackets sin sanitizar.
func extractAddr(from string) string {
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			return from[i+1 : i+j]
		}
	}
	return strings.TrimSpace(from)
}

// buildOTPHTML vive en resend.go (mismo package) — lo reusamos para que
// el template del correo sea identico via Resend o via SMTP.
