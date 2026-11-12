package ids

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIDs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IDs Suite")
}
