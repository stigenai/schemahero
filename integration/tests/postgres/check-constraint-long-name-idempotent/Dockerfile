FROM postgres

ENV POSTGRES_USER=schemahero
ENV POSTGRES_DB=schemahero

# No fixtures: this test creates the long-named CHECK via SchemaHero itself (plan
# #1), then re-plans (plan #2) and asserts the second plan is empty. Driving the
# CREATE through SchemaHero guarantees the constraint is stored under EXACTLY the
# capped name the generator produces, so the HIGH-1 proof does not depend on
# hardcoding the hash suffix into a fixture.
