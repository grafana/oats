package yaml

import (
	"encoding/json"
	"fmt"
	. "github.com/onsi/gomega"
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
	AssertLokiResponse(b, l, r)
}

func AssertLokiResponse(b []byte, l ExpectedLogs, r *runner) {
	g := r.gomega
	g.Expect(len(b)).Should(BeNumerically(">", 0), "expected loki response to be non-empty")

	response := QueryResponse{}
	err := json.Unmarshal(b, &response)
	if err != nil {
		_, _ = fmt.Fprintf(r.queryLogger.Logger, "error unmarshalling loki response: %s\n", string(b))
	}
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(response.Status).To(Equal("success"))
	streams := response.Data.Result
	g.Expect(len(streams)).Should(BeNumerically(">", 0), "expected loki streams to be non-empty")

	stream := streams[0]
	line := stream.Values[0][1]
	if len(l.Equals) > 0 {
		// check for exact match in additional asserts
		g.Expect(line).To(ContainSubstring(l.Equals))
	}
	if len(l.Regexp) > 0 {
		g.Expect(line).To(MatchRegexp(l.Regexp))
	}
	for _, s := range l.Contains {
		g.Expect(line).To(ContainSubstring(s))
	}

	// don't retry we've found the log
	r.additionalAsserts = append(r.additionalAsserts, func() {
		if len(l.Equals) > 0 {
			g.Expect(line).To(Equal(l.Equals))
		}
		assertLabels(l, stream.Stream)
	})
}

func assertLabels(l ExpectedLogs, labels map[string]string) {
	for k, v := range l.Attributes {
		Expect(labels).To(HaveKeyWithValue(k, v))
	}
	for k, v := range l.AttributeRegexp {
		Expect(labels).To(HaveKey(k))
		Expect(labels[k]).To(MatchRegexp(v))
	}
	if l.NoExtraAttributes {
		var allowedKeys []string
		for k := range l.Attributes {
			allowedKeys = append(allowedKeys, k)
		}
		for k := range l.AttributeRegexp {
			allowedKeys = append(allowedKeys, k)
		}
		var keys []string
		for k := range labels {
			keys = append(keys, k)
		}
		Expect(keys).To(ConsistOf(allowedKeys))
	}
}
