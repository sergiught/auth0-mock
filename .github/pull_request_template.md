<!--
Thanks for the PR! A few notes:
  • One change per PR. Refactors and behavior changes go in separate PRs.
  • The PR title must follow Conventional Commits — CI lints it (it becomes the squash-merge subject). Local pre-commit also lints each commit message.
  • CI runs lint + unit tests + features + govulncheck on every push.
  • See CONTRIBUTING.md for the full workflow.
-->

## Summary

<!--
One or two sentences explaining what changes and *why*. Focus on the why —
the diff already shows the what.
-->

## Type of change

<!-- Tick all that apply. -->

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that would change existing behavior in a way that requires consumers to update)
- [ ] Refactor (no functional change)
- [ ] Documentation only
- [ ] Build / CI / tooling

## Related issues

<!--
e.g. `Closes #123`, `Refs #456`. If there's no issue and the change is more
than a typo fix, mention briefly why you didn't open one first.
-->

## How to verify

<!--
Concrete commands a reviewer can paste to reproduce your testing locally.
Prefer curl/make targets over screenshots so the verification is automatable.
-->

```bash
make lint
make test
make test-features
```

## Checklist

- [ ] Tests added or updated (unit and/or `features/*.feature` as appropriate)
- [ ] Documentation updated (README, `docs/COOKBOOK.md`, `docs/ARCHITECTURE.md`, or per-package godoc)
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) — CI's `commitlint` job will reject otherwise (it becomes the squash-merge subject that release-please derives `CHANGELOG.md` from; don't hand-edit the changelog)
- [ ] `make lint`, `make test`, and `make test-features` all pass locally
