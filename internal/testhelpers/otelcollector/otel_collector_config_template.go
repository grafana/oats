package otelcollector

const ConfigTemplate = `
receivers:
  otlp:
    protocols:
      grpc:

exporters:
  otlp:
    endpoint: {{ .TempoEndpoint }}
    tls:
      insecure: true
  file:
    path: {{ .FileExporterOutputPath }} 
  prometheusremotewrite:
    endpoint: "http://{{- .PrometheusEndpoint -}}/api/v1/push"
    tls:
      insecure: true

extensions:
  health_check:
     endpoint: 0.0.0.0:{{- .HealthCheckPort }}
     path: "/health/status" 

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp, file]
    metrics:
      receivers: [otlp]
      exporters: [prometheusremotewrite]

  extensions: [health_check]

`

type ConfigTemplateData struct {
	TempoEndpoint          string
	PrometheusEndpoint     string
	FileExporterOutputPath string
	HealthCheckPort        string
}
