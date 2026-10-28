# Release versions used by builds and runtime defaults.
# Override individual values on the make command line or in the environment.
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)

# VERSION remains a compatibility alias for existing release commands.
ifneq ($(origin VERSION), undefined)
OPERATOR_VERSION ?= $(VERSION)
endif
OPERATOR_VERSION ?= 2.0.0-Alpha1
VERSION ?= $(OPERATOR_VERSION)

# Keep these streams independently configurable even while their defaults align.
GATEWAY_VERSION ?= 2.0.0-Alpha1
DATA_INDEX_VERSION ?= 2.0.0-SNAPSHOT-postgresql
QUARKUS_FLOW_RUNNER_VERSION ?= 1.3.0
