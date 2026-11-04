---
title: "Verifying releases"
description: "Verify the Cosign signature on Docker images and find the SBOM for each release."
sidebar:
  order: 3
---

Every tagged release ships with a Cosign signature on each Docker image and an SPDX-JSON SBOM per release archive. Both are produced by GitHub-hosted CI and uploaded as part of the same workflow that publishes the binaries.

**Verify a Docker image** (keyless signing — no shared secret required). Replace `X.Y.Z` with the tag you want to verify, e.g. `0.227.0`:

```bash
cosign verify \
  --certificate-identity-regexp 'https://github.com/sergiught/auth0-mock/\.github/workflows/release\.yml@.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/sergiught/auth0-mock:X.Y.Z
```

:::note
Only the `ghcr.io/...` tag is cosign-signed by the release workflow. The `docker.io/sergiught/...` mirror is a publish-only convenience; verify the equivalent GHCR digest if you need attestation.
:::

A successful verification proves the image was built by *this* repo's release workflow at *that* tag — not a CDN-substituted copy.

**Find an SBOM:** every release on the [GitHub Releases page](https://github.com/sergiught/auth0-mock/releases) carries an `auth0-mock_<version>_<os>_<arch>.tar.gz.spdx.json` alongside each archive. Pass it to your SBOM scanner of choice (Snyk, FOSSA, Black Duck, `grype`, etc.).
