FROM tesseract-hammer:latest AS base

# Build release image
FROM alpine:3.23.2@sha256:865b95f46d98cf867a156fe4a135ad3fe50d2056aa3f25ed31662dff6da4eb62

# Copy the hammer test intermediate CA certificate, intermediate CA key and certificate signing private key into the container
COPY ./internal/hammer/testdata/test_intermediate_ca_cert.pem ./internal/hammer/testdata/test_intermediate_ca_private_key.pem ./internal/hammer/testdata/test_leaf_cert_signing_private_key.pem /bin/

# Copy the hammer binary
COPY --from=base /bin/hammer /bin/

ENTRYPOINT ["/bin/hammer"]
