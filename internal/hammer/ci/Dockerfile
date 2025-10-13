FROM tesseract-hammer:latest AS base

# Build release image
FROM alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412

# Copy the hammer test intermediate CA certificate, intermediate CA key and certificate signing private key into the container
COPY ./internal/hammer/testdata/test_intermediate_ca_cert.pem ./internal/hammer/testdata/test_intermediate_ca_private_key.pem ./internal/hammer/testdata/test_leaf_cert_signing_private_key.pem /bin/

# Copy the hammer binary
COPY --from=base /bin/hammer /bin/

ENTRYPOINT ["/bin/hammer"]
