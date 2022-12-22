# Run YAWOL Controllers
# NOTE: set the following environment variales
#export CLUSTER_NAMESPACE=yawol-test
#export API_ENDPOINT="https://<IP/URL to KubeAPI>"

go run ./cmd/yawol-controller/main.go \
--enable-loadbalancer-controller --metrics-addr-lb=":8081" \
--enable-loadbalancerset-controller --metrics-addr-lbs=":8082" \
--enable-loadbalancermachine-controller --metrics-addr-lbm=":8083"
