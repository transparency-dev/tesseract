
FROM golang:1.25.4-alpine3.21@sha256:3289aac2aac769e031d644313d094dbda745f28af81cd7a94137e73eefd58b33 AS builder

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
FROM alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412

COPY --from=builder /build/bin/hammer /bin/hammer
ENTRYPOINT ["/bin/hammer"]
