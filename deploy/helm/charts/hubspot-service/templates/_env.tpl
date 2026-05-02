{{/* commonEnv define las env vars que comparten server y worker. */}}
{{- define "hubspotService.commonEnv" -}}
- name: GRPC_PORT
  value: "0.0.0.0:{{ .Values.service.grpcPort }}"
- name: WEBHOOK_HTTP_PORT
  value: "{{ .Values.service.webhookPort }}"
- name: HUBSPOT_ENVIRONMENT
  value: "{{ .Values.hubspot.environment }}"
- name: HUBSPOT_CO_KEY_ID
  value: "{{ .Values.hubspot.customObjects.keyId }}"
- name: HUBSPOT_CO_ASESOR_ID
  value: "{{ .Values.hubspot.customObjects.asesorId }}"
- name: HUBSPOT_CO_COLEGIO_ID
  value: "{{ .Values.hubspot.customObjects.colegioId }}"
- name: HUBSPOT_OTP_WEBHOOK_TRIGGER_ID
  value: "{{ .Values.hubspot.otpWebhook.triggerId }}"
- name: HUBSPOT_ASESOR_TEAM_ID
  value: "{{ .Values.hubspot.team.asesorTeamId }}"
- name: HUBSPOT_ASESOR_ROLE_ID
  value: "{{ .Values.hubspot.team.asesorRoleId }}"
- name: REDIS_HOST
  value: "{{ .Values.global.redisHost }}"
- name: REDIS_PORT
  value: "{{ .Values.global.redisSslPort }}"
- name: REDIS_TLS
  value: "true"
- name: USERS_SERVICE_GRPC
  value: "users-service:50051"
- name: HUBSPOT_API_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "hubspotService.fullname" . }}-kv
      key: hubspot-api-token
- name: HUBSPOT_OTP_WEBHOOK_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "hubspotService.fullname" . }}-kv
      key: otp-webhook-token
- name: REDIS_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "hubspotService.fullname" . }}-kv
      key: redis-password
{{- end -}}
