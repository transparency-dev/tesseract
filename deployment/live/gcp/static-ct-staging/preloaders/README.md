# Preloader deployment instructions

## How to build

Run the following commands to build the preloader container and store it in
the project's Artifact Registry:

```bash
HEAD=$(curl -s https://api.github.com/repos/google/certificate-transparency-go/branches/master | jq -r '.commit.sha')
docker build -t us-central1-docker.pkg.dev/static-ct-staging/docker-staging/preloader:$(echo $HEAD | cut -c1-7) -t us-central1-docker.pkg.dev/static-ct-staging/docker-staging/preloader:latest -f ./Dockerfile . --build-arg HEAD=$HEAD
gcloud auth login --project=static-ct-staging
docker -v push --all-tags  us-central1-docker.pkg.dev/static-ct-staging/docker-staging/preloader
```

## How to deploy

Export the name of the target log:

```bash
export LOGNAME=arche2025h1
```

To apply the Terragrunt config, run:

```bash
terragrunt apply --working-dir=deployment/live/gcp/static-ct-staging/preloaders/$LOGNAME
```

> [!NOTE]
> This will request for a start index, until we automate this part.
> For now, for `arche2025h1`, use the current log size +300000, and for
> `arche2025h2`, the current log size +240000. This accounts for entries from
> Argon logs that have not made it into Arche logs since we're now using
> lax509.

Applying the Terragrunt config might or might not update the container
depending on the change applied by Terragrunt. For instance,
a flag change would trigger a container restart, but a new build wouldn't
necessarily trigger a restart.

To update the container, run:

```bash
gcloud compute instances update-container $LOGNAME-preloader --zone=us-central1-f
```
