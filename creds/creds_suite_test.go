package creds

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCreds(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Creds Suite")
}
