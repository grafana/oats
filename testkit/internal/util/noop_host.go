package util

import "go.opentelemetry.io/collector/component"

type NoopHost struct{}

// GetExporters implements component.Host.
func (*NoopHost) GetExporters() map[component.Type]map[component.ID]component.Component { return nil }

// GetExtensions implements component.Host.
func (*NoopHost) GetExtensions() map[component.ID]component.Component { return nil }

// GetFactory implements component.Host.
func (*NoopHost) GetFactory(kind component.Kind, componentType component.Type) component.Factory {
	return nil
}

// ReportFatalError implements component.Host.
func (*NoopHost) ReportFatalError(err error) {}

var _ component.Host = (*NoopHost)(nil)
