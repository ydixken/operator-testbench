# Lessons

- Write regression coverage with each behavior change, but run functional tests only in CI.
  Locally, format touched files and run `task lint` and `task test` only.
- Do not add local docs-build, smoke, or skill-evaluation gates; CI owns them.
- A required status check that never reports blocks every merge.
  Never add a `paths:` filter to a workflow whose job is a required check.
