package transcode

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTranscode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Transcode Suite")
}
