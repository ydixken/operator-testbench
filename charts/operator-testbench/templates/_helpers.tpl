{{- define "testbench.fullname" -}}
{{- if contains .Chart.Name .Release.Name }}{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}{{ end }}
{{- end }}

{{- define "testbench.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{/*
The scenario names of the selected pack: the value scenarios for a pack read
from packDir, or else files/scenarios.yaml, which task chart:generate writes
from the embedded packs. A pack that is missing there means the chart and the
image come from different releases.
*/}}
{{- define "testbench.scenarios" -}}
{{- $list := .Values.scenarios | default list }}
{{- if .Values.packDir }}
{{- if not (hasPrefix "/" .Values.packDir) }}{{ fail (printf "packDir %q must be an absolute path" .Values.packDir) }}{{ end }}
{{- if not $list }}{{ fail "scenarios is required with packDir: the scenario names of the pack" }}{{ end }}
{{- range $list }}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" .) }}{{ fail (printf "scenarios: %q is not a DNS label" .) }}{{ end }}
{{- end }}
{{- else }}
{{- if $list }}{{ fail "scenarios needs packDir: the embedded packs bring their own list" }}{{ end }}
{{- $all := .Files.Get "files/scenarios.yaml" | fromYaml }}
{{- $list = get $all .Values.pack }}
{{- if not $list }}{{ fail (printf "pack %q is not in files/scenarios.yaml" .Values.pack) }}{{ end }}
{{- end }}
{{- toYaml $list }}
{{- end }}

{{/*
Every scenario namespace of the run, named the way the engine names them:
testbench-<pack>-<scenario>, with -<N> on Ginkgo process N > 1.
*/}}
{{- define "testbench.namespaces" -}}
{{- $names := list }}
{{- range $proc := untilStep 1 (int (add1 .Values.procs)) 1 }}
{{- range include "testbench.scenarios" $ | fromYamlArray }}
{{- $ns := printf "testbench-%s-%s" $.Values.pack . }}
{{- if gt $proc 1 }}{{ $ns = printf "%s-%d" $ns $proc }}{{ end }}
{{- $names = append $names $ns }}
{{- end }}
{{- end }}
{{- toYaml $names }}
{{- end }}
