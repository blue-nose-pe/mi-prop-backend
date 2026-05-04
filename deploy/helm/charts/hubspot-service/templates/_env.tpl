{{/* commonEnv define las env vars que comparten server y worker. */}}
{{- define "hubspotService.commonEnv" -}}
- name: GRPC_PORT
  value: "0.0.0.0:{{ .Values.service.grpcPort }}"
- name: WEBHOOK_HTTP_PORT
  value: ":{{ .Values.service.webhookPort }}"
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
# Rate limit distribuido (paquete requestsengine).
# - HUBSPOT_API_TOKENS: CSV con TODAS las API keys disponibles. Más
#   tokens = más rps total (5 keys × 10 rps = 50 rps globales).
# - HUBSPOT_RPS: rps POR token (HubSpot = 10).
# - REDIS_DB_RATELIMIT: DB Redis dedicada al rate limiter, separada
#   de la DB de asynq para no mezclar keyspaces.
- name: HUBSPOT_RPS
  value: "{{ .Values.hubspot.rateLimit.rps }}"
- name: HUBSPOT_ENGINE_WORKERS
  value: "{{ .Values.hubspot.rateLimit.workers }}"
- name: HUBSPOT_ENGINE_QUEUE_SIZE
  value: "{{ .Values.hubspot.rateLimit.queueSize }}"
- name: REDIS_HOST
  value: "{{ .Values.global.redisHost }}"
- name: REDIS_PORT
  value: "{{ .Values.global.redisSslPort }}"
- name: REDIS_ADDR
  value: "{{ .Values.global.redisHost }}:{{ .Values.global.redisSslPort }}"
- name: REDIS_TLS
  value: "{{ .Values.global.redisTls | default "true" }}"
- name: REDIS_DB
  value: "{{ .Values.hubspot.redis.dbAsynq }}"
- name: REDIS_DB_RATELIMIT
  value: "{{ .Values.hubspot.redis.dbRateLimit }}"
- name: USERS_SERVICE_GRPC
  value: "users-service:50051"
- name: HUBSPOT_API_TOKENS
  valueFrom:
    secretKeyRef:
      name: {{ include "hubspotService.fullname" . }}-kv
      key: hubspot-api-tokens
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
- name: JWT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "hubspotService.fullname" . }}-kv
      key: jwt-secret
- name: JWT_ISSUER
  value: "{{ .Values.global.jwtIssuer | default "miproposito.users" }}"
{{- end -}}
