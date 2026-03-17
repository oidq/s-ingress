# E2E tests

This is a collection of some very basic E2E tests for the S-Ingress.
They work by summoning a full K8s cluster using kind and then running
tests with [ginkgo](https://onsi.github.io/ginkgo/).

## Prerequisites

The host must have these tools installed:

- Docker
- Kind
- Helm
- Kubectl

## Structure

Directory `basic-cluster` contains specification and test cases for simple testing.
The tests are invoked with `make` and running `make e2e` should create everything and run the tests.
Target `clean` should dismantle the kind cluster.

## Example

```shell
# setup everything and run tests
make e2e

# redeploy s-ingress and rerun tests (the cluster must be running)
make retest

# run just tests
make run-test

# clean up the kind cluster
make clean
```
