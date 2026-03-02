FROM golang:1.25.0-alpine3.21@sha256:c8e1680f8002c64ddfba276a3c1f763097cb182402673143a89dcca4c107cf17 AS builder

ARG GOFLAGS="-trimpath"
ARG HEAD=master
ENV GOFLAGS=$GOFLAGS

# Move to working directory /build
WORKDIR /build

RUN go install github.com/google/certificate-transparency-go/preload/preloader@$HEAD

# Build release image
FROM alpine:3.20.2@sha256:0a4eaa0eecf5f8c050e5bba433f58c052be7587ee8af3e8b3910ef9ab5fbe9f5

COPY --from=builder /go/bin/preloader /bin/preloader

ENTRYPOINT ["/bin/preloader"]
