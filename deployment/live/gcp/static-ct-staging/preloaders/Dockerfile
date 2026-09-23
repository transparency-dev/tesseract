FROM golang:1.26.8-alpine3.24@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS builder

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
