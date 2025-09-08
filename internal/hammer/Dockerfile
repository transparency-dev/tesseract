
FROM golang:1.25.1-alpine3.21@sha256:331bde41663c297cba0f5abf37e929be644f3cbd84bf45f49b0df9d774f4d912 AS builder

ARG GOFLAGS="-trimpath -buildvcs=false -buildmode=exe"
ENV GOFLAGS=$GOFLAGS

# Move to working directory /build
WORKDIR /build

# Copy and download dependency using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build the application
RUN go build -o bin/hammer ./internal/hammer

# Build release image
FROM alpine:3.22.1@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1

COPY --from=builder /build/bin/hammer /bin/hammer
ENTRYPOINT ["/bin/hammer"]
