# Run YAWOL Cloud Controller
# NOTE: set the following environment variales
#export CLUSTER_NAMESPACE="yawol-test"
#export SECRET_NAME="cloud-provider-config"
#export FLOATING_NET_ID="test"
#export NETWORK_ID="test"
#export FLAVOR_ID="osAyv1W3z2TU5D6h" # m1.amphora
#export IMAGE_ID="332c90c3-3141-4413-9ef9-f70a472cedb6" # yawol-alpine-v0.5.0

go run ./cmd/yawol-cloud-controller/main.go --classname test --target-kubeconfig=inClusterConfig --control-kubeconfig=inClusterConfig
