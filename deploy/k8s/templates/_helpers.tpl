{{- define "sluice.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sluice.labels" -}}
app.kubernetes.io/name: {{ include "sluice.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "sluice.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sluice.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
