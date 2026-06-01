include ../common.mk

TEST_NAME := postgres-check-constraint-long-name-idempotent
SPEC_FILE := ./specs/measurements.yaml

# This test overrides the standard single-plan run target. It proves the HIGH-1
# fix (generated constraint names capped at 63 bytes) end-to-end against a real
# PostgreSQL:
#   1. plan #1 against an empty DB -> CREATE TABLE + a CHECK whose GENERATED name
#      would, uncapped, exceed 63 bytes; apply it.
#   2. plan #2 against the now-populated DB MUST be empty. If the name were not
#      capped, PostgreSQL would have stored a silently-truncated 63-byte name, the
#      lookup of the full name would miss, and plan #2 would emit ADD-then-DROP
#      (the wedge). An empty plan #2 is the proof there is no wedge.
.PHONY: run
run:
	docker pull postgres:$(PG_VERSION)
	docker build -t $(DATABASE_IMAGE_NAME) .
	@-docker rm -f $(DATABASE_CONTAINER_NAME) > /dev/null 2>&1 ||:
	# the $ is doubled due to make :-(
	docker run -p 15432:5432 --rm -d -e POSTGRES_PASSWORD='!@#$$%^&*(){}'\''";' -e POSTGRES_HOST_AUTH_METHOD=md5 --name $(DATABASE_CONTAINER_NAME) $(DATABASE_IMAGE_NAME)
	while ! docker exec $(DATABASE_CONTAINER_NAME) pg_isready --quiet; do sleep 1; done
	@sleep 1

	# Plan #1: create the table + the long-generated-name CHECK, then apply it.
	../../../../bin/kubectl-schemahero plan --seed-data --driver=$(DRIVER) --uri="$(URI)" --spec-file $(SPEC_FILE) --spec-type $(SPEC_TYPE) > out.sql
	@echo "Plan #1 output:" && cat out.sql
	../../../../bin/kubectl-schemahero apply --driver=$(DRIVER) --uri="$(URI)" --ddl out.sql

	# Plan #2: must be EMPTY (no ADD-then-DROP wedge). A blank expect file means the
	# second plan produced zero statements.
	../../../../bin/kubectl-schemahero plan --seed-data --driver=$(DRIVER) --uri="$(URI)" --spec-file $(SPEC_FILE) --spec-type $(SPEC_TYPE) > out2.sql
	@echo "Plan #2 output (must be empty):" && cat out2.sql
	@echo Verifying second plan is a no-op for $(TEST_NAME)
	diff -B expect.sql out2.sql

	# Check for leaked plugin processes
	@bash ../../../check-plugin-leaks.sh

	# Cleanup
	@-sleep 5
	rm ./out.sql ./out2.sql
	@-docker rm -f $(DATABASE_CONTAINER_NAME)
