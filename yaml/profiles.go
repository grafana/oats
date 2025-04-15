package yaml

import (
	"encoding/json"
	"log/slog"

	"github.com/onsi/gomega"
)

type PyroscopeQueryResponse struct {
	Flamebearer struct {
		Names []string `json:"names"`
	} `json:"flamebearer"`
}

func AssertPyroscope(r *runner, p ExpectedProfiles) {
	b, err := r.endpoint.SearchPyroscope(p.Query)
	r.LogQueryResult("query %v response %v err=%v\n", p.Query, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())
	assertPyroscopeResponse(b, p, r)
}

func assertPyroscopeResponse(b []byte, p ExpectedProfiles, r *runner) {
	g := r.gomegaInst
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0), "expected pyroscope response to be non-empty")

	response := PyroscopeQueryResponse{}
	err := json.Unmarshal(b, &response)
	if err != nil {
		slog.Info("error unmarshalling pyroscope", "response", string(b))
	}

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(response.Flamebearer.Names).To(gomega.ContainElement(gomega.ContainSubstring(p.Flamebearers.Contains)))
}
