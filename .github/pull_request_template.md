<!--
👋 Thanks for the PR! Quick conventions:
  • One change per PR — split refactors from behavior changes.
  • PR title follows Conventional Commits (it's the squash-merge subject; CI lints it).
  • See CONTRIBUTING.md for the full workflow.
-->

## 📝 Summary

<!-- One or two sentences on what changes and *why*. The diff shows the what. -->

## 🏷 Type of change

<!-- Tick all that apply. -->

- [ ] 🐛 Bug fix (non-breaking)
- [ ] ✨ Feature (non-breaking)
- [ ] 💥 Breaking change (consumers must update)
- [ ] ♻️ Refactor (no functional change)
- [ ] 📚 Docs only
- [ ] 🛠 Build / CI / tooling

## 🔗 Related issues

<!-- `Closes #123` / `Refs #456`. No issue? Say why in a line (typos excepted). -->

## ✅ How to verify

<!-- Commands a reviewer can paste to reproduce your testing. -->

```bash
make lint
make test
make test-features
```

## 📋 Checklist

- [ ] 🧪 Tests added or updated (unit and/or `features/*.feature`)
- [ ] 📖 Docs updated (README, `docs/COOKBOOK.md`, `docs/ARCHITECTURE.md`, or godoc)
- [ ] 🏷 PR title follows [Conventional Commits](https://www.conventionalcommits.org/) — CI rejects otherwise (release-please derives `CHANGELOG.md` from it; don't hand-edit the changelog)
- [ ] 🟢 `make lint`, `make test`, `make test-features` pass locally
