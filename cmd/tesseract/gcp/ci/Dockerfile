FROM tesseract-gcp:latest AS base

# Build release image
FROM alpine:3.22.0@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715

# Copy the hammer test root CA certificate into the container
COPY ./internal/hammer/testdata/test_root_ca_cert.pem /bin/

# Copy the tesseract-gcp binary
COPY --from=base /bin/tesseract-gcp /bin/

ENTRYPOINT ["/bin/tesseract-gcp"]
