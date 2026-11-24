# Release versions used by builds and runtime defaults.
# Override individual values on the make command line or in the environment.
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)

# VERSION remains a compatibility alias for existing release commands.
ifneq ($(origin VERSION), undefined)
OPERATOR_VERSION ?= $(VERSION)
endif
OPERATOR_VERSION ?= 2.0.0-Alpha4
VERSION ?= $(OPERATOR_VERSION)

# Keep these streams independently configurable even while their defaults align.
GATEWAY_VERSION ?= 2.0.0-Alpha4
DATA_INDEX_VERSION ?= 2.0.0-Alpha4-postgresql
QUARKUS_FLOW_RUNNER_VERSION ?= 1.4.0
DB_MIGRATOR_VERSION ?= 2.0.0-Alpha4

# logic-apps tag that provides the Vector config embedded in the operator
# (synced by `make sync-configs`). Bump it together with the versions above on
# each release. Use `main` only to test unreleased logic-apps changes.
LOGIC_APPS_REPO ?= https://github.com/kubesmarts/logic-apps
LOGIC_APPS_REF ?= v2.0.0-Alpha4
