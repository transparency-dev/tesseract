# GCP Cloud Build Triggers and Steps

This directory contains terragrunt files to configure our Cloud Build pipeline(s).

The Cloud Build pipeline is triggered when a commit in the repo is tagged with
`^staging-deploy-(.+)$` and is responsible for:

1. Building the `cmd/tesseract/gcp` and `cmd/tesseract/gcp/staging` Docker images from the last
commit with a `^staging-deploy-(.+)$` tag.
1. Updating [staging
logs'](/deployment/live/gcp/static-ct-staging/logs/) Cloud Run service with the
latest Docker image.
1. Updating [staging
logs'](/deployment/live/gcp/static-ct-staging/logs/) infrastructure with the
latest Terraform config.

## Initial setup

The first time this is run for a pair of {GCP Project, GitHub Repo} you will get
an error message such as the following:

```bash
Error: Error creating Trigger: googleapi: Error 400: Repository mapping does not exist. Please visit $URL to connect a repository to your project
```

This is a manual one-time step that needs to be followed to integrate GCP Cloud
Build and the GitHub repository.
