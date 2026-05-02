{{/*
Helpers reusables para exams-service.
*/}}

{{- define "examsService.name" -}}
exams-service
{{- end -}}

{{- define "examsService.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "examsService.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "examsService.labels" -}}
app.kubernetes.io/name: {{ include "examsService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}

{{- define "examsService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "examsService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "examsService.image" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository .Values.global.imageTag -}}
{{- end -}}

{{- define "examsService.migrateImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.migration.image.repository .Values.global.imageTag -}}
{{- end -}}
