{{- define "redisServer.name" -}}
redis-server
{{- end }}

{{- define "redisServer.fullname" -}}
redis-server
{{- end }}

{{- define "redisServer.labels" -}}
app.kubernetes.io/name: {{ include "redisServer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: cache
{{- end }}

{{- define "redisServer.selectorLabels" -}}
app.kubernetes.io/name: {{ include "redisServer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
