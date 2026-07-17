{{/*
Expand the name of the chart.
*/}}
{{- define "highland.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "highland.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "highland.labels" -}}
helm.sh/chart: {{ include "highland.name" . }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "highland.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "highland.longhornNamespace" -}}
{{- if .Values.embeddedLonghorn.enabled -}}
{{- .Release.Namespace -}}
{{- else -}}
{{- .Values.longhorn.namespace -}}
{{- end -}}
{{- end }}

{{- define "highland.managerUrl" -}}
http://{{ .Values.longhorn.managerService }}.{{ include "highland.longhornNamespace" . }}.svc.cluster.local:{{ .Values.longhorn.managerPort }}
{{- end }}

{{/* providers.longhorn.enabled overrides the legacy longhorn.enabled switch
when explicitly set; null preserves upgrades from Longhorn-only values. */}}
{{- define "highland.longhornEnabled" -}}
{{- if kindIs "bool" .Values.providers.longhorn.enabled -}}
{{- .Values.providers.longhorn.enabled -}}
{{- else -}}
{{- or .Values.longhorn.enabled .Values.embeddedLonghorn.enabled -}}
{{- end -}}
{{- end }}

{{- define "highland.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s" (include "highland.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
