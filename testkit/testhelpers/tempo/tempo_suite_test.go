package tempo_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTempo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tempo Suite")
}
