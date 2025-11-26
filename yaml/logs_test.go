package yaml

import (
	"os"
	"testing"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestAssertLokiResponse(t *testing.T) {
	gomega.RegisterTestingT(t)

	file, err := os.ReadFile("testdata/loki_response.json")
	require.NoError(t, err)
	logs := model.ExpectedSignal{
		Contains: []string{"simulating an error"},
		Attributes: map[string]string{
			"deployment_environment": "staging",
			"exception_message":      "simulating an error",
			"exception_type":         "java.lang.RuntimeException",
			"scope_name":             "org.apache.catalina.core.ContainerBase.[Tomcat].[localhost].[/].[dispatcherServlet]",
			"service_name":           "dice",
			"service_namespace":      "shop",
			"severity_number":        "17",
			"severity_text":          "ERROR",
			"k8s_container_name":     "dice",
			"k8s_namespace_name":     "default",
			"message":                "Servlet.service() for servlet [dispatcherServlet] in context with path [] threw exception [Request processing failed: java.lang.RuntimeException: simulating an error] with root cause",
		},
		AttributeRegexp: map[string]string{
			"thread_name":                 ".*",
			"span_id":                     ".*",
			"trace_id":                    ".*",
			"k8s_pod_name":                "dice-.*-.*",
			"k8s_pod_uid":                 ".*",
			"k8s_container_restart_count": ".*",
			"service_instance_id":         ".*",
			"exception_stacktrace":        ".*",
		},
		NoExtraAttributes: true,
	}
	r := &runner{
		gomegaInst: gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		}),
	}
	AssertLokiResponse(file, logs, r)
}
