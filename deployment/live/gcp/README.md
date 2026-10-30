# GCP live configs

This directory contains Terragrunt configs we use to run static-ct-api logs and
other related pieces of infrastructure:

- [./static-ct-ci](./static-ct-ci/): configures a continuous integration environment using the [hammer](/internal/hammer/)
- [./test](./test/): configures a test log using a GCP VM
