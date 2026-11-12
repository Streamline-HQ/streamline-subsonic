package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// app is the Streamline instance every spec of this process talks to, booted
// once with subsonic.wasm granted exactly what its manifest asks for.
var app *instance

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Subsonic E2E Suite")
}

var _ = BeforeSuite(func() {
	app = boot()
})
