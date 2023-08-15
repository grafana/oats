package en_suite_provisioning_test

import (
	"context"
	"testing"

	"github.com/grafana/oats/internal/testhelpers/observability"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var localEndpoint *observability.LocalEndpoint

var _ = BeforeSuite(func() {
	var ctx context.Context = context.Background()
	var startErr error

	localEndpoint = observability.NewLocalEndpoint()

	DeferCleanup(func() {
		var ctx context.Context = context.Background()
		var stopErr error

		if localEndpoint != nil {
			stopErr = localEndpoint.Stop(ctx)
			Expect(stopErr).ToNot(HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
	})

	startErr = localEndpoint.Start(ctx)
	Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
})

func TestEnSuiteProvisioning(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ginkgo Bootstrap Provisioning Suite")
}
