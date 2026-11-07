{{- define "pre.name" -}}{{ .Release.Name }}-pod-restart-exporter{{- end }}
{{- define "pre.labels" -}}
app.kubernetes.io/name: pod-restart-exporter
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- define "pre.selector" -}}
app.kubernetes.io/name: pod-restart-exporter
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
