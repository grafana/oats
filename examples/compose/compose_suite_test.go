package compose_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCompose(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Compose Suite")
}
