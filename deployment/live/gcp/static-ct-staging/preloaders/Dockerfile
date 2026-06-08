FROM golang:1.25.8-alpine3.23@sha256:8e02eb337d9e0ea459e041f1ee5eece41cbb61f1d83e7d883a3e2fb4862063fa AS builder

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
