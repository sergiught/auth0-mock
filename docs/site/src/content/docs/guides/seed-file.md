---
title: Seed file
description: Start the mock with the same stubs, claims and permissions every time, and save a running mock's state as a seed file.
sidebar:
  order: 1.2
---

From release 0.234.0 the mock can load its starting state from a file. Without it, you register the same stubs by hand every time the compose stack starts. With it, the stubs are there when the container is healthy, and `POST /admin0/reset` puts them back after each test.

## Mount a seed file

Put the file in its own folder and mount the folder, not the file:

```yaml
# docker-compose.yaml
services:
  auth0-mock:
    image: ghcr.io/sergiught/auth0-mock:latest
    ports:
      - "127.0.0.1:8080:8080"
      - "127.0.0.1:8443:8443"
    environment:
      ADMIN_ALLOW_REMOTE: "true"
      SEED_FILE: /seed/seed.yaml
    volumes:
      - ./seed:/seed:ro
```

Mounting the folder matters. On Linux, a single-file bind mount keeps pointing at the original file, and most editors save by writing a new file and renaming it over the old one, so the container never sees the change.

Then write `seed/seed.yaml`:

```yaml
expectations:
  - method: GET
    path: /api/v2/users/auth0|seed
    response:
      status: 200
      body:
        user_id: auth0|seed
        email: seed@example.test
claims:
  tier: gold
permissions:
  "https://api.example.test/":
    - read:things
mfa_required: false
```

Start the stack and list the stubs:

```bash
docker compose up -d
curl -s http://localhost:8080/admin0/expectations
```

## File format

The file is YAML. JSON works too, because JSON is valid YAML. All keys are optional.

| Key | Content |
|---|---|
| `expectations` | A list. Each item is the body you would send to `POST /admin0/expectations` (see [Stubbing](/auth0-mock/guides/stubbing/)) |
| `claims` | Custom claims, as for `PUT /admin0/claims` |
| `claim_mappings` | Request-parameter to claim-name map, as for `PUT /admin0/claims/mappings` |
| `permissions` | Audience mapped to a list of permissions |
| `mfa_required` | `true` or `false` |

The mock is strict about the file:

- An unknown key is an error, and keys are case-sensitive (`Expectations` is unknown). Inside an expectation, `id` and `hits` are the only extra keys allowed, so an export with `detail=true` still loads.
- An error in an expectation names the item, for example `expectations[2]: ...`.
- Two expectations with the same method, path and request matcher are an error.
- A date or timestamp written without quotes, such as `2026-11-07`, stays exactly as you wrote it.

If the file is bad at startup, the mock refuses to start. It prints `fatal: seed file <path>: ...` and exits with code 1. Check `docker compose logs auth0-mock`.

## Reset

`POST /admin0/reset` wipes the mock and then applies the seed again. Stubs you registered during a test are gone and the seeded ones are back.

To wipe everything, the seed included, add `keep_seed=false`:

```bash
curl -X POST "http://localhost:8080/admin0/reset?keep_seed=false"
```

A value that is not `true` or `false` gets a 400. Without `SEED_FILE`, `keep_seed` changes nothing.

## Edits are picked up

The mock checks the file about once a second and reloads once the new content has settled, which takes two checks, so about two seconds. When the content changes, it resets to the new file. Stubs registered at runtime are dropped by that reset.

- If an edit leaves the file broken, the mock keeps its current state and logs one ERROR line.
- If the file is deleted, the mock keeps its current state and logs one WARN line.

Fix the file or put it back and the next check loads it.

## Save the current state as a seed file

Set up the mock by hand or with your tests, then export it:

```bash
curl -s "http://localhost:8080/admin0/export?format=yaml" > seed/seed.yaml.new
mv seed/seed.yaml.new seed/seed.yaml
```

The mock watches `seed/seed.yaml`, and writing it in place can be read half-written, so write a new file and move it over.

| Query | Values | Effect |
|---|---|---|
| `format` | `json` (default), `yaml` | `yaml` returns `application/yaml` |
| `detail` | `true`, `false` (default) | `true` adds `id` and `hits` to each expectation |

Any other value gets a 400. Empty sections are left out and keys are sorted. Exporting, loading and exporting again gives the same bytes. Numbers keep the exact text you wrote, so `1.0` stays `1.0`; a number written in a YAML-only form such as `0x1F` comes back as plain decimal (`31`).

Reading the export does not change the mock, so you can call it at any point.
