FROM tesseract-hammer:latest AS base

# Build release image
FROM alpine:3.22.1@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1

# Copy the hammer test intermediate CA certificate, intermediate CA key and certificate signing private key into the container
COPY ./internal/hammer/testdata/test_intermediate_ca_cert.pem ./internal/hammer/testdata/test_intermediate_ca_private_key.pem ./internal/hammer/testdata/test_leaf_cert_signing_private_key.pem /bin/

# Copy the hammer binary
COPY --from=base /bin/hammer /bin/

ENTRYPOINT ["/bin/hammer"]
