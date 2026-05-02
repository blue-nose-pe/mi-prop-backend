// Package webhookadapter implementa ports.OTPWebhook contra el endpoint
// Automation Webhooks de HubSpot.
//
// El URL completo es:
//   https://api-na1.hubapi.com/automation/v4/webhook-triggers/{triggerID}/{token}
//
// El P1 lo tenía hardcoded; en P2 viene por env vars (triggerID público
// como ConfigMap, token secreto desde Key Vault).
package webhookadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

const automationBase = "https://api-na1.hubapi.com/automation/v4/webhook-triggers"

type OTP struct {
	url  string
	http *http.Client
}

var _ ports.OTPWebhook = (*OTP)(nil)

func NewOTP(triggerID, token string) *OTP {
	return &OTP{
		url:  fmt.Sprintf("%s/%s/%s", automationBase, triggerID, token),
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Trigger envía {email, otp_token} al webhook. HubSpot Automation responde
// 202 cuando aceptó el trigger. Cualquier otro código → reintento.
func (o *OTP) Trigger(ctx context.Context, email, otp string) error {
	payload, err := json.Marshal(map[string]string{
		"email":     email,
		"otp_token": otp,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrOTPDeliveryFail, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status=%d body=%s", domain.ErrOTPDeliveryFail, resp.StatusCode, string(body))
	}
	return nil
}
