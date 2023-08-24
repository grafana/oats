package mimir_test

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/grafana/oats/internal/testhelpers/common"
	"github.com/grafana/oats/internal/testhelpers/mimir"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LocalMimirEndpoint", func() {
  It("starts a Mimir endpoint locally", func() {
    ctx := context.Background()

		sharedNetworkUUID, err := uuid.NewRandom()
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a UUID for a shared container network")

		sharedNetworkName := fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

		_, err = common.ContainerNetwork(sharedNetworkName)
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a shared container network")

		defer common.DestroyContainerNetwork(sharedNetworkName)

		endpoint, err := mimir.NewLocalEndpoint(ctx, sharedNetworkName)
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Prometheus endpoint")

		endpointURL, err := endpoint.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "expected no error starting a local Prometheus endpoint")

		defer endpoint.Stop(ctx)

		resp, err := http.Get(fmt.Sprintf("http://%s/-/healthy", endpointURL.HostEndpoint))
		Expect(err).ToNot(HaveOccurred(), "expected no error getting the status of the local Tempo endpoint")

		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusOK), "expected 200 OX from the local Prometheus endpoint")

		respBytes, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred(), "expected no error reading a Tempo status response")

		Expect(string(respBytes)).To(ContainSubstring("Prometheus Server is Healthy."), "expected a healthy response from Promtheus")

  })
})
