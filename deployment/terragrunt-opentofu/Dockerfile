FROM alpine/terragrunt:1.22.2@sha256:0f6a9fb75d7ed40b654d28ae0cfd0d83f46b0808a80d2b82bcbfb99280e37fc2 AS terragrunt
FROM ghcr.io/opentofu/opentofu:minimal@sha256:9fe8ce8425bbae93fff97ff683fc1694f3ddbabaaec39ebdad24882aceb2d5eb AS tofu

# Build release image
FROM alpine:3.22.0@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715

RUN apk add --update --no-cache bash git openssh

# Copy the terragrunt binary from the image
COPY --from=terragrunt /usr/local/bin/terragrunt /usr/local/bin/terragrunt

# Copy the tofu binary from the minimal image
COPY --from=tofu /usr/local/bin/tofu /usr/local/bin/tofu
