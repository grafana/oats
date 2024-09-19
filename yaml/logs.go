package yaml

import (
	"encoding/json"
	"fmt"
	. "github.com/onsi/gomega"
	"io"
)

type QueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func AssertLoki(r *runner, l ExpectedLogs) {
	b, err := r.endpoint.SearchLoki(l.LogQL)
	r.queryLogger.LogQueryResult("logQL query %v response %v err=%v\n", l.LogQL, string(b), err)
	g := r.gomega
	g.Expect(err).ToNot(HaveOccurred())
	AssertLokiResponse(g, b, l, r.queryLogger.Logger)
}

func AssertLokiResponse(g Gomega, b []byte, l ExpectedLogs, logger io.WriteCloser) {
	g.Expect(len(b)).Should(BeNumerically(">", 0), "expected loki response to be non-empty")

	response := QueryResponse{}
	err := json.Unmarshal(b, &response)
	if err != nil {
		_, _ = fmt.Fprintf(logger, "error unmarshalling loki response: %s\n", string(b))
	}
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(response.Status).To(Equal("success"))
	streams := response.Data.Result
	g.Expect(len(streams)).Should(BeNumerically(">", 0), "expected loki streams to be non-empty")

	stream := streams[0]
	labels := stream.Stream
	for k, v := range l.Attributes {
		g.Expect(labels).To(HaveKeyWithValue(k, v))
	}
	for k, v := range l.AttributeRegexp {
		g.Expect(labels).To(HaveKey(k))
		g.Expect(labels[k]).To(MatchRegexp(v))
	}
	line := stream.Values[0][1]
	for _, s := range l.Contains {
		g.Expect(line).To(ContainSubstring(s))
	}
}
