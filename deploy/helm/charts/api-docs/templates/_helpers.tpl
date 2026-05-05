{{- define "apiDocs.fullname" -}}
{{- printf "%s-api-docs" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "apiDocs.labels" -}}
app.kubernetes.io/name: api-docs
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "apiDocs.selectorLabels" -}}
app.kubernetes.io/name: api-docs
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
