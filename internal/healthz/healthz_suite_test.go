package healthz_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHealthz(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Healthz Suite")
}
