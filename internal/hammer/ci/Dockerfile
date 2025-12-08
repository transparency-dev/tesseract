FROM tesseract-hammer:latest AS base

# Build release image
FROM alpine:3.23.0@sha256:51183f2cfa6320055da30872f211093f9ff1d3cf06f39a0bdb212314c5dc7375

# Copy the hammer test intermediate CA certificate, intermediate CA key and certificate signing private key into the container
COPY ./internal/hammer/testdata/test_intermediate_ca_cert.pem ./internal/hammer/testdata/test_intermediate_ca_private_key.pem ./internal/hammer/testdata/test_leaf_cert_signing_private_key.pem /bin/

# Copy the hammer binary
COPY --from=base /bin/hammer /bin/

ENTRYPOINT ["/bin/hammer"]
