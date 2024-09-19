package yaml

import (
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestAssertLokiResponse(t *testing.T) {
	file, err := os.ReadFile("testdata/loki_response.json")
	require.NoError(t, err)
	logs := ExpectedLogs{
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
			"thread_name":          ".*",
			"span_id":              ".*",
			"trace_id":             ".*",
			"k8s_pod_name":         "dice-.*-.*",
			"k8s_pod_uid":          ".*",
			"exception_stacktrace": ".*",
		},
	}
	AssertLokiResponse(gomega.NewGomega(func(message string, callerSkip ...int) {
		t.Error(message)
	}), file, logs, nil)
}

//contains:
//  - 'simulating an error'
//attributes:
//  deployment_environment: staging
//  exception_message: "simulating an error"
//  exception_type: "java.lang.RuntimeException"
//  scope_name: "org.apache.catalina.core.ContainerBase.[Tomcat].[localhost].[/].[dispatcherServlet]"
//  service_name: dice
//  service_namespace: shop
//  severity_number: 17
//  severity_text: ERROR
//  k8s_container_name: dice
//  k8s_namespace_name: shop
//  message: "Servlet.service() for servlet [dispatcherServlet] in context with path [] threw exception [Request processing failed: java.lang.RuntimeException: simulating an error] with root cause"
//attribute-regexp:
//  thread_name: ".*"
//  span_id: ".*"
//  trace_id: ".*"
//  k8s_pod_name: dice-.*-.*
//  k8s_pod_uid: ".*"
//  exception_stacktrace: ".*" # TODO: transform to sanitized stacktrace
