package yaml

import (
	"encoding/json"
	"log/slog"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
)

type LokiQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func AssertLoki(r *runner, l model.ExpectedLogs) {
	b, err := r.endpoint.SearchLoki(l.LogQL)
	r.LogQueryResult("logQL query %v response %v err=%v\n", l.LogQL, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())
	AssertLokiResponse(b, l.Signal, r)
}

func AssertLokiResponse(b []byte, l model.ExpectedSignal, r *runner) {
	g := r.gomegaInst
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0), "expected loki response to be non-empty")

	response := LokiQueryResponse{}
	err := json.Unmarshal(b, &response)
	if err != nil {
		slog.Info("error unmarshalling loki", "response", string(b))
	}
	g.Expect(err).ToNot(gomega.HaveOccurred())

	g.Expect(response.Status).To(gomega.Equal("success"))
	streams := response.Data.Result
	g.Expect(len(streams)).Should(gomega.BeNumerically(">", 0), "expected loki streams to be non-empty")

	line := ""
	atts := map[string]string{}
	if len(streams) > 0 && len(streams[0].Values) > 0 {
		line = streams[0].Values[0][1]
		atts = streams[0].Stream
	}
	assertSignal(g, l, len(streams), line, atts)
}
