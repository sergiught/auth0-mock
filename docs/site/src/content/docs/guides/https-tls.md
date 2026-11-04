---
title: HTTPS / TLS
description: Run auth0-mock over HTTPS and make clients trust its self-signed certificate.
sidebar:
  order: 5
---

The mock serves HTTPS with a self-signed cert by default; this guide covers trusting it, persisting it, and skipping verification.

## HTTPS / TLS

The auto-generated cert covers `localhost`, `127.0.0.1`, `::1` (override with `TLS_HOSTNAMES`). TLS behaviour is the same on macOS and Linux, but the cert is self-signed, so clients reject it unless told otherwise. Three options:

:::caution
**macOS gotcha:** Go on macOS pulls trust roots from the system Security framework and ignores `SSL_CERT_FILE` / `SSL_CERT_DIR` (Linux Go honors them). The Linux `SSL_CERT_FILE=./tls.crt go run …` trick simply doesn't work on macOS. On macOS, trust the cert via `mkcert` (option 1 below, easiest), or import it into the keychain (`security add-trusted-cert …`, recipe in [`docs/COOKBOOK.md`](/auth0-mock/guides/https-tls/#trusting-the-self-signed-cert)), or build a custom `tls.Config{RootCAs: pool}` in your client code.
:::

### 1. `mkcert` (recommended for local dev)

[`mkcert`](https://github.com/FiloSottile/mkcert) installs a local CA into your platform's trust store and signs certs with it. Browsers, Go, and `curl` accept the result without flags:

```bash
mkcert -install                                                # one-time per workstation
mkcert -cert-file tls.crt -key-file tls.key localhost 127.0.0.1 ::1

docker run --rm --user "$(id -u):$(id -g)" -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 \
  -e ADMIN_ALLOW_REMOTE=true -e TLS_CERT_FILE=/certs/tls.crt -e TLS_KEY_FILE=/certs/tls.key \
  -v "$PWD:/certs" ghcr.io/sergiught/auth0-mock:latest
```

`mkcert` makes `tls.key` readable only by you. `--user` runs the container as you so it can read the key.

### 2. `TLS_CACHE_DIR` (recommended for `docker compose` without mkcert)

Pick a path; the mock writes its auto-generated cert there on first boot and reuses it on subsequent restarts. Trust the cert once and trust persists:

```bash
docker compose run --rm -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 \
  -e ADMIN_ALLOW_REMOTE=true -e TLS_CACHE_DIR=/data/tls \
  -v auth0-mock-tls:/data/tls auth0-mock
```

### 3. Skip verification

Fine for ephemeral tests, ugly for anything else:

```bash
curl -k https://localhost:8443/.well-known/openid-configuration
# Go: &tls.Config{InsecureSkipVerify: true}
```

To install the mock's cert into your OS trust store (after option 2 so it's stable across boots), see [`docs/COOKBOOK.md`](/auth0-mock/guides/https-tls/#trusting-the-self-signed-cert).

## Run against HTTPS with a trusted cert

:::caution
**macOS Go ignores `SSL_CERT_FILE` and `SSL_CERT_DIR`**: those env vars are honored on Linux but not on macOS, where Go reads roots from the system Security framework. So the Linux shortcut (`SSL_CERT_FILE=./tls.crt go run …`) won't work on macOS. Use `mkcert` (which writes its CA into the keychain), the [trust-store recipe](#trusting-the-self-signed-cert) below (`security add-trusted-cert …`), or construct a `tls.Config{RootCAs: pool}` in client code.
:::

For local dev, use [`mkcert`](https://github.com/FiloSottile/mkcert):

```bash
mkcert -install                                                  # one-time
mkcert -cert-file tls.crt -key-file tls.key localhost 127.0.0.1 ::1

docker run --rm --user "$(id -u):$(id -g)" -p 127.0.0.1:8443:8443 -e ADMIN_ALLOW_REMOTE=true \
  -e TLS_CERT_FILE=/certs/tls.crt -e TLS_KEY_FILE=/certs/tls.key \
  -v "$PWD:/certs" ghcr.io/sergiught/auth0-mock:latest   # --user: mkcert's key is owner-only

curl https://localhost:8443/.well-known/openid-configuration   # no -k needed
```

For ephemeral CI tests that just need to skip verification, set `InsecureSkipVerify: true` on your client's TLS config (Go) or pass `-k` (curl). Don't do this in production.

## Trusting the self-signed cert

After running with `TLS_CACHE_DIR=/data/tls` so the cert is stable across reboots:

```bash
# Export from a running server (or read from $TLS_CACHE_DIR/tls.crt):
openssl s_client -connect localhost:8443 -showcerts </dev/null 2>/dev/null \
  | openssl x509 -outform pem > /tmp/auth0-mock.crt

# macOS
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain /tmp/auth0-mock.crt

# Debian / Ubuntu
sudo cp /tmp/auth0-mock.crt /usr/local/share/ca-certificates/auth0-mock.crt
sudo update-ca-certificates

# Arch / Fedora
sudo trust anchor /tmp/auth0-mock.crt
```

After this, `curl https://localhost:8443/...` works without `-k`, browsers stop nagging, and Go clients trust the cert via the system root pool. Combined with `TLS_CACHE_DIR`, trust persists across container restarts.
