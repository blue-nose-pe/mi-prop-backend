{{/*
Nombre canónico del chart (usado por todos los recursos).
*/}}
{{- define "mssqlServer.name" -}}
mssql-server
{{- end }}

{{- define "mssqlServer.fullname" -}}
mssql-server
{{- end }}

{{- define "mssqlServer.labels" -}}
app.kubernetes.io/name: {{ include "mssqlServer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: database
{{- end }}

{{- define "mssqlServer.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mssqlServer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
