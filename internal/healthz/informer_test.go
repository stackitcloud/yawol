package healthz_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/stackitcloud/yawol/internal/healthz"
)

var _ = Describe("NewCacheSyncHealthz", func() {
	It("should succeed if all informers sync", func() {
		checker := NewCacheSyncHealthz(fakeSyncWaiter(true))
		Expect(checker(nil)).NotTo(HaveOccurred())
	})
	It("should fail if informers don't sync", func() {
		checker := NewCacheSyncHealthz(fakeSyncWaiter(false))
		Expect(checker(nil)).To(MatchError(ContainSubstring("not synced")))
	})
})

type fakeSyncWaiter bool

func (f fakeSyncWaiter) WaitForCacheSync(_ context.Context) bool {
	return bool(f)
}
