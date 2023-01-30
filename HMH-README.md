### Building

This must be built from a linux machine, will not work on m1 mac.

#### 0.12.x
```
git clone https://github.com/hmhco/consul-k8s
cd consul-k8s/
git checkout v0.43.0-hmhco
docker build . -t docker.br.hmheng.io/kubernetes/consul-k8s-control-plane:0.43.0-hmhco-1
docker push docker.br.hmheng.io/kubernetes/consul-k8s-control-plane:0.43.0-hmhco-1
```

#### future releases
first checkout hashicorp target tag 

#               our branch   hashicorps tag
git checkout -b v0.X.X-hmhco v0.x.x 

add the go changes and dockerfile like this PR
https://github.com/hmhco/consul-k8s/pull/1/

You will need to update the `FROM hashicorp/consul-k8s-control-plane:0.43.0` to match the target version of the build
