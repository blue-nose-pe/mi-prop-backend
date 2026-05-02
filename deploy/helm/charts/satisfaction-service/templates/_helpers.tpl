{{/*
Helpers reusables para satisfaction-service.
*/}}

{{- define "satisfactionService.name" -}}
satisfaction-service
{{- end -}}

{{- define "satisfactionService.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "satisfactionService.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "satisfactionService.labels" -}}
app.kubernetes.io/name: {{ include "satisfactionService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}

{{- define "satisfactionService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "satisfactionService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "satisfactionService.image" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository .Values.global.imageTag -}}
{{- end -}}

{{- define "satisfactionService.migrateImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.migration.image.repository .Values.global.imageTag -}}
{{- end -}}
