FROM sctfe-gcp:latest AS base

# Build release image
FROM alpine:3.20.2@sha256:0a4eaa0eecf5f8c050e5bba433f58c052be7587ee8af3e8b3910ef9ab5fbe9f5

# Copy roots into the container
# TODO(phbnf): use a better name without test, right now this is what works with main.tf
COPY ./deployment/live/gcp/static-ct-staging/logs/arche2025h1/roots.pem /bin/test_root_ca_cert.pem

# Copy the sctfe-gcp binary
COPY --from=base /bin/sctfe-gcp /bin/

ENTRYPOINT ["/bin/sctfe-gcp"]
