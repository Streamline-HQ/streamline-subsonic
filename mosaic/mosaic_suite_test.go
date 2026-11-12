package mosaic

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMosaic(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mosaic Suite")
}
