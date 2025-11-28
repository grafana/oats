package yaml

import (
	"encoding/json"
	"log/slog"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
)

type PyroscopeQueryResponse struct {
	Flamebearer struct {
		Names []string `json:"names"`
	} `json:"flamebearer"`
}

func AssertPyroscope(r *Runner, p model.ExpectedProfiles) {
	b, err := r.endpoint.SearchPyroscope(p.Query)
	r.LogQueryResult("query %v response %v err=%v\n", p.Query, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())
	assertPyroscopeResponse(b, p, r)
}

func assertPyroscopeResponse(b []byte, p model.ExpectedProfiles, r *Runner) {
	g := r.gomegaInst
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0), "expected pyroscope response to be non-empty")

	response := PyroscopeQueryResponse{}
	err := json.Unmarshal(b, &response)
	if err != nil {
		slog.Info("error unmarshalling pyroscope", "response", string(b))
	}

	g.Expect(err).ToNot(gomega.HaveOccurred())
	f := p.Flamebearers
	equals := f.NameEquals
	if len(equals) > 0 {
		g.Expect(response.Flamebearer.Names).To(gomega.ContainElement(gomega.Equal(equals)))
	}
	regexp := f.NameRegexp
	if len(regexp) > 0 {
		g.Expect(response.Flamebearer.Names).To(gomega.ContainElement(gomega.MatchRegexp(regexp)))
	}
}
