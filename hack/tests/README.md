# yawol-tests
## apply 

```
kubectl apply -f ./
```

## normal

This returns a Nginx default page
```
curl "http://$(kubectl get services normal --output jsonpath='{.status.loadBalancer.ingress[0].ip}')"
```

## multible-replicas

This returns a Nginx default page and multible LBMs in shoot namespace on seed
```
curl "http://$(kubectl get services multible-replicas --output jsonpath='{.status.loadBalancer.ingress[0].ip}')"
```

## internal

This returns internal IP:
```
kubectl get services internal --output jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

## proxy protocol

This returns a Nginx default page and in the logs you should find your external IP
```
curl "http://$(kubectl get services proxy-protocol --output jsonpath='{.status.loadBalancer.ingress[0].ip}')"
```

## udp

This returns test from the UDP echo (cancel with control-c):
```
echo "test"  | nc -u $(kubectl get services udp-echo --output jsonpath='{.status.loadBalancer.ingress[0].ip}') 80
```

## udp-timeout

This returns test from the UDP echo (cancel with control-c):
```
echo "test"  | nc -u $(kubectl get services udp-timeout --output jsonpath='{.status.loadBalancer.ingress[0].ip}') 80
```

## tcp-timeout

This command should be closed with no output in approximately 10 sec

Bash:
```
time nc $(kubectl get services tcp-timeout --output jsonpath='{.status.loadBalancer.ingress[0].ip}') 80 < <( sleep 11; echo "test" )
```

Zsh:
```
{ sleep 11; echo "test" } | time nc $(kubectl get services tcp-timeout --output jsonpath='{.status.loadBalancer.ingress[0].ip}') 80
```

## cleanup 

```
kubectl delete -f ./
```
