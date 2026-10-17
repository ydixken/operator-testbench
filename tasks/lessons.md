# Lessons

- Write regression coverage with each behavior change, but run functional tests only in CI.
  Locally, format touched files and run `task lint` and `task test` only.
- Do not add local docs-build, smoke, or skill-evaluation gates; CI owns them.
- A required status check that never reports blocks every merge.
  Never add a `paths:` filter to a workflow whose job is a required check.
- Commits and pull request text carry no AI co-author trailer and no agent attribution line.
  The owner stated this as a hard requirement; the guides (AGENTS.md, CONTRIBUTING.md) say so and the history was rewritten once to remove them.
