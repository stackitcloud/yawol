package kubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	// +kubebuilder:scaffold:imports
)

var _ = Describe("version test suite", func() {
	When("testing IsEqual function", func() {
		It("should be equal", func() {
			v1 := Version{Major: 10, Minor: 10}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsEqual(&v2)).To(BeTrue())
		})

		It("should not be equal", func() {
			v1 := Version{Major: 10, Minor: 10}
			v2 := Version{Major: 11, Minor: 11}
			Expect(v1.IsEqual(&v2)).To(BeFalse())
		})
	})

	When("testing IsGreater function", func() {
		It("should be greater", func() {
			v1 := Version{Major: 11, Minor: 10}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsGreater(&v2)).To(BeTrue())
		})

		It("should be greater", func() {
			v1 := Version{Major: 10, Minor: 11}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsGreater(&v2)).To(BeTrue())
		})

		It("should not be greater", func() {
			v1 := Version{Major: 10, Minor: 10}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsGreater(&v2)).To(BeFalse())
		})

		It("should not be greater", func() {
			v1 := Version{Major: 9, Minor: 10}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsGreater(&v2)).To(BeFalse())
		})

		It("should not be greater", func() {
			v1 := Version{Major: 10, Minor: 9}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsGreater(&v2)).To(BeFalse())
		})
	})

	When("testing IsLower function", func() {
		It("should be lower", func() {
			v1 := Version{Major: 9, Minor: 10}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsLower(&v2)).To(BeTrue())
		})

		It("should be lower", func() {
			v1 := Version{Major: 10, Minor: 9}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsLower(&v2)).To(BeTrue())
		})

		It("should not be lower", func() {
			v1 := Version{Major: 10, Minor: 10}
			v2 := Version{Major: 10, Minor: 10}
			Expect(v1.IsLower(&v2)).To(BeFalse())
		})

		It("should not be lower", func() {
			v1 := Version{Major: 10, Minor: 10}
			v2 := Version{Major: 9, Minor: 10}
			Expect(v1.IsLower(&v2)).To(BeFalse())
		})
	})
})
