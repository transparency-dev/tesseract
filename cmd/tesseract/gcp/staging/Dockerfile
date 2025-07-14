FROM tesseract-gcp:latest AS base

# Build release image
FROM alpine:3.22.0@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715

# Copy roots into the container
# TODO(phbnf): use a better name without test, right now this is what works with main.tf
COPY ./deployment/live/gcp/static-ct-staging/logs/arche2025h1/roots.pem /bin/test_root_ca_cert.pem

# Copy the tesseract-gcp binary
COPY --from=base /bin/tesseract-gcp /bin/

ENTRYPOINT ["/bin/tesseract-gcp"]
