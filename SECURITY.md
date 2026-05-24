# Security Policy

## Supported versions

auth0-mock is pre-1.0; only the latest tagged release receives security fixes.
Pin a tagged version and upgrade to pick up fixes.

| Version        | Supported          |
| -------------- | ------------------ |
| latest release | :white_check_mark: |
| older          | :x:                |

## Reporting a vulnerability

**Please do not open a public issue for security reports.**

Report privately via GitHub's private vulnerability reporting:

→ https://github.com/sergiught/auth0-mock/security/advisories/new

We aim to acknowledge reports within **5 business days** and to provide a status
update within **10 business days**. Coordinated disclosure is appreciated — we'll
agree on a timeline before any public detail is shared.

## Scope

auth0-mock is a **testing tool**, not a production identity provider. Some
behaviours are insecure *by design* and are out of scope for reports:

- `/admin0/*` is intentionally **unauthenticated** — never expose it to an
  untrusted network. Bind to `127.0.0.1` (the default) or your CI runner.
- The auto-generated TLS certificate is self-signed.
- Tokens are signed with an in-process key and audience is echoed, not enforced
  (unless `BEARER_REQUIRE_AUDIENCE` is set).

In scope: anything that lets an attacker compromise the **host running the
mock** or escape the documented test-tool threat model (e.g. RCE via a crafted
request, path traversal, a crash/DoS from a well-formed request).
