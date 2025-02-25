{{- define "prometheus-s3-exporter.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name }}
{{- end }}