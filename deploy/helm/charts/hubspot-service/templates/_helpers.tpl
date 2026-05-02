{{- define "hubspotService.name" -}}hubspot-service{{- end -}}
{{- define "hubspotService.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "hubspotService.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "hubspotService.labels" -}}
app.kubernetes.io/name: {{ include "hubspotService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: miproposito
{{- end -}}
{{- define "hubspotService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hubspotService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "hubspotService.serverImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository .Values.global.imageTag -}}
{{- end -}}
{{- define "hubspotService.workerImage" -}}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.workerRepository .Values.global.imageTag -}}
{{- end -}}
