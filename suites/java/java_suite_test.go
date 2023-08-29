package java_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJava(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Java Suite")
}
