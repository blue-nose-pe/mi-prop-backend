{{- define "gateway.name" -}}gateway{{- end -}}
{{- define "gateway.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "gateway.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "gateway.labels" -}}
app.kubernetes.io/name: {{ include "gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}
{{- define "gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "gateway.image" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository .Values.global.imageTag -}}
{{- end -}}
