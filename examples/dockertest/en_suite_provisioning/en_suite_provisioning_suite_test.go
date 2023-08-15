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
	var startCtx context.Context = context.Background()

	localEndpoint = observability.NewLocalEndpoint()

	DeferCleanup(func() {
		var  stopCtx context.Context = context.Background()

		if localEndpoint != nil {
			Expect(localEndpoint.Stop(stopCtx)).To(Succeed(), "expected no error stopping the local observability endpoint")
		}
	})

	Expect(localEndpoint.Start(startCtx)).To(Succeed(), "expected no error starting a local observability endpoint")
})

func TestEnSuiteProvisioning(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ginkgo Bootstrap Provisioning Suite")
}
