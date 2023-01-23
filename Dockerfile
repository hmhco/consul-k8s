FROM public.ecr.aws/docker/library/golang:1.18.4-alpine3.15 as build
ARG TARGETOS
ARG TARGETARCH

COPY . /go

RUN cd /go/control-plane && \
	set -x; go build -o pkg/bin/consul-k8s-control-plane

# final image
# we are simply copying our custom built binary over the standard binary in the image
# If we need to upgrade past 1.12.x this line should be updated to reflect that
FROM hashicorp/consul-k8s-control-plane:0.43.0

ARG TARGETOS
ARG TARGETARCH

COPY --from=build /go/control-plane/pkg/bin/ /bin