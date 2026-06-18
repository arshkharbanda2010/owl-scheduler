{{- /*
  Common labels and name helpers
*/}}
{{- define "k8s-scheduler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "k8s-scheduler.fullname" -}}
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

{{- define "k8s-scheduler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "k8s-scheduler.labels" -}}
helm.sh/chart: {{ include "k8s-scheduler.chart" . }}
app.kubernetes.io/name: {{ include "k8s-scheduler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: owl-scheduler
{{- end }}

{{- define "k8s-scheduler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8s-scheduler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
