package prometheus

const ConfigTemplate = `
global:
  scrape_interval:     1s
  evaluation_interval: 1s

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: [ 'localhost:{{- .ContainerPort -}}' ]
`

type ConfigTemplateData struct {
	ContainerPort string
}
