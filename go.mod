module github.com/stackitcloud/yawol

go 1.16

require (
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	sigs.k8s.io/controller-runtime v0.8.3
	sigs.k8s.io/controller-runtime/tools/setup-envtest v0.0.0-20210916143346-8e1263d50ea2
	sigs.k8s.io/controller-tools v0.6.2
)

replace (
	k8s.io/api => k8s.io/api v0.19.14
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.14
)
