{{/*
Helpers reusables para analytics-service.
*/}}

{{- define "analyticsService.name" -}}
analytics-service
{{- end -}}

{{- define "analyticsService.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "analyticsService.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "analyticsService.labels" -}}
app.kubernetes.io/name: {{ include "analyticsService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}

{{- define "analyticsService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "analyticsService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "analyticsService.image" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository (.Values.image.tag | default .Values.global.imageTag) -}}
{{- end -}}

{{- define "analyticsService.migrateImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.migration.image.repository (.Values.image.tag | default .Values.global.imageTag) -}}
{{- end -}}
