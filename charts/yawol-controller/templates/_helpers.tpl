{{- define "deploymentversion" -}}
apps/v1
{{- end -}}

{{- define "logFlags" }}
- -zap-stacktrace-level={{ .Values.logging.stacktraceLevel }}
- -zap-log-level={{ .Values.logging.level }}
- -zap-encoder={{ .Values.logging.encoding }}
{{- end }}
