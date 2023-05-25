FROM public.ecr.aws/docker/library/golang:1.19.2-alpine as build
COPY . /go

RUN cd /go/control-plane && \
	set -x; go build -o pkg/bin/consul-k8s-control-plane

# final image
# we are simply copying our custom built binary over the standard binary in the image
# If we need to upgrade past 1.13.x this line should be updated to reflect that
FROM hashicorp/consul-k8s-control-plane:0.49.5

COPY --from=build /go/control-plane/pkg/bin/ /bin 

