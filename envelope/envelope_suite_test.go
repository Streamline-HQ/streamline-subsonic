package envelope

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEnvelope(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Envelope Suite")
}
