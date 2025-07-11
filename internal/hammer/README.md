# Hammer: A load testing tool for Static CT API logs

This hammer sets up read and write traffic to a Static CT API log to test correctness and performance under load.
The traffic is sent according to the [The Static Certificate Transparency API](https://c2sp.org/static-ct-api) spec.

## UI

The hammer runs using a text-based UI in the terminal that shows the current status, logs, and supports increasing/decreasing read and write traffic.
The process can be killed with `<Ctrl-C>`.
This TUI allows for a level of interactivity when probing a new configuration of a log in order to find any cliffs where performance degrades.

For real load-testing applications, especially headless runs as part of a CI pipeline, it is recommended to run the tool with `show_ui=false` in order to disable the UI.

## Usage

Example usage to test a deployment of `cmd/tesseract/gcp`:

```shell
go run ./internal/hammer \
  --origin=test-static-ct \
  --log_public_key=MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEZtouPtPylIR8RgvqdsXxTXEuOjL50GQvmkg25JpNnoNNbZZDVt1niU7xm4BwYu1JERIjTV2LlmyRbCBHCmG6Jg== \
  --log_url=https://storage.googleapis.com/transparency-dev-playground-test-static-ct-bucket \
  --write_log_url=http://localhost:6962/test-static-ct
  --max_read_ops=1024 \
  --num_readers_random=128 \
  --num_readers_full=128 \
  --num_writers=256 \
  --max_write_ops=42 \
  --num_mmd_verifiers=256 \
  --bearer_token=$(gcloud auth print-access-token)
```

For a headless write-only example that could be used for integration tests, this command attempts to write 2500 leaves within 1 minute.
If the target number of leaves is reached then it exits successfully.
If the timeout of 1 minute is reached first, then it exits with an exit code of 1.

```shell
go run ./internal/hammer \
  --origin=test-static-ct \
  --log_public_key=MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEZtouPtPylIR8RgvqdsXxTXEuOjL50GQvmkg25JpNnoNNbZZDVt1niU7xm4BwYu1JERIjTV2LlmyRbCBHCmG6Jg== \
  --log_url=https://storage.googleapis.com/transparency-dev-playground-test-static-ct-bucket \
  --write_log_url=http://localhost:6962/test-static-ct
  --max_read_ops=0 \
  --num_writers=512 \
  --max_write_ops=512 \
  --num_mmd_verifiers=512 \
  --max_runtime=1m \
  --leaf_write_goal=2500 \
  --bearer_token=$(gcloud auth print-access-token) \
  --show_ui=false
```

# Design

## Objective

Write a tool that can send write and read requests to a Static CT API log in order to check the performance of writes and reads, and ensure that these logs are behaving correctly.

## Architecture

### Components

Interactions with the log are performed by different implementations of worker, that are managed by separate pools:
  - writer: adds new leaves to the tree using a `POST` request to an `/add-chain` endpoint
  - full reader: reads all leaves from the tree, starting at 0 and fetching them all
  - random reader: reads leaves randomly within the size of the tree

All readers verify inclusion proofs against a common checkpoint, so it is cryptographically assured that they all see consistent views of the data.

The number of each of these workers, and the rate at which they work is configurable (both via flags and through the UI).
The number of workers is configured by increasing the size of the pool, which increases concurrency.
The amount of work to be performed in a given duration is controlled by a pair of throttles: one for read operations, and one for write operations.

Higher level components are:
  - Hammer: orchestrates the work and owns the worker pools
  - Hammer Analyser: consumes the results of the hammer and workers to determine throughput and system health
  - UI: displays the current state of the system and allows some control over the number of workers

The TUI is only intended to be used to interactively explore the capabilities of a log; actual load testing should be done headlessly.

## Deployment

There are 2 main modes that the hammer has been designed to run in:
  1. Headless, which has 2 main sub-modes:
     1. Goal-oriented: given both a timeout and a target number of entries to write, process exits successfully if enough entries can be _written_ to the log before the time expires, or otherwise an error code is returned
     1. Infinite: keeps running until killed in order to perform long-lived performance tests
  1. TUI: runs in the console with a Text UI that allows some interactivity to tune the load characteristics and see the results

