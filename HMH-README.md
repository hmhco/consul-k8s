### Building

This must be built from a linux machine, will not work on m1 mac.

#### 0.12.x
```
git clone https://github.com/pveasey/consul-k8s
cd consul-k8s/
git checkout v0.43.0-hmhco
docker build . -t docker.br.hmheng.io/kubernetes/consul-k8s-control-plane:0.43.0-veaseyp-2
docker push docker.br.hmheng.io/kubernetes/consul-k8s-control-plane:0.43.0-veaseyp-1
```

#### future releases
first checkout hashicorp target tag 

#               our branch   hashicorps tag
git checkout -b v0.X.X-hmhco v0.x.x 

add the changes in this commit 
https://github.com/Pveasey/consul-k8s/commit/3fb8965c5848abc9c1f39a23fc7a12134290ccb7

