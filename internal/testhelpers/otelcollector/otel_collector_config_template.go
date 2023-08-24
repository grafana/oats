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

connectors:
  servicegraph:
    virtual_node_peer_attributes:
      - net.peer.name

extensions:
  health_check:
     endpoint: 0.0.0.0:{{- .HealthCheckPort }}
     path: "/health/status" 

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp, file, servicegraph]
    metrics:
      receivers: [otlp]
      exporters: [prometheusremotewrite]
    metrics/servicegraph:
      receivers: [servicegraph]
      exporters: [prometheusremotewrite]
 
  extensions: [health_check]

`

type ConfigTemplateData struct {
	TempoEndpoint          string
	PrometheusEndpoint     string
	FileExporterOutputPath string
	HealthCheckPort        string
}
