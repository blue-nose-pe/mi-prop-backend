{{/*
Helpers reusables para keys-service.
*/}}

{{- define "keysService.name" -}}
keys-service
{{- end -}}

{{- define "keysService.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "keysService.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "keysService.labels" -}}
app.kubernetes.io/name: {{ include "keysService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}

{{- define "keysService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "keysService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "keysService.image" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository (.Values.image.tag | default .Values.global.imageTag) -}}
{{- end -}}

{{- define "keysService.migrateImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.migration.image.repository (.Values.image.tag | default .Values.global.imageTag) -}}
{{- end -}}
