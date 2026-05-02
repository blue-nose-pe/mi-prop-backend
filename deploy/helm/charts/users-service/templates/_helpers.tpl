{{/*
Helpers reusables para users-service.
*/}}

{{- define "usersService.name" -}}
users-service
{{- end -}}

{{- define "usersService.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "usersService.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "usersService.labels" -}}
app.kubernetes.io/name: {{ include "usersService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}

{{- define "usersService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "usersService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "usersService.image" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository .Values.global.imageTag -}}
{{- end -}}

{{- define "usersService.migrateImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.migration.image.repository .Values.global.imageTag -}}
{{- end -}}
