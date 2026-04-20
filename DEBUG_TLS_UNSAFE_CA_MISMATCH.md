# TLS Unsafe Warning Debug Record (api.openai.com)

## Context
- Date: 2026-04-20
- Runtime: `main.exe` already started by user
- Symptom: Browser shows connection is unsafe when visiting `https://api.openai.com/v1/models`
- User assumption: certificates already exist in `ca/`

## Goal
Verify why browser trust failed and provide a reliable fix.

## Debug Process

### 1. Confirm service is running and listening
- Checked process:
  - `main` process exists (PID `25732`)
- Checked listening ports:
  - `0.0.0.0:443` and `0.0.0.0:8080` are both owned by PID `25732`
- Interpretation:
  - `443` = proxy HTTPS/API entry
  - `8080` = management UI (`manage_port`), not forward-proxy CONNECT endpoint

### 2. Verify proxy behavior is basically healthy
- Read `config.yaml`:
  - `server.port: 443`
  - `server.manage_port: 8080`
  - domains include `api.openai.com`, `api.anthropic.com`
- Functional probes against local service (via host headers / Python requests):
  - `GET /v1/models` with `Host: api.openai.com` -> `200`
  - `GET /v1/models` with `Host: api.anthropic.com` + Anthropic headers -> `200`
  - `POST /v1/responses` -> upstream `401 INVALID_API_KEY` (expected with fake key, proves forwarding works)
- Interpretation:
  - Core routing/forwarding is functioning.
  - The browser warning is a trust-chain issue, not routing failure.

### 3. Verify DNS/hosts mapping
- Command result:
  - `Resolve-DnsName api.openai.com` -> `127.0.0.1`
- Interpretation:
  - Domain is correctly redirected to local proxy.

### 4. Inspect generated certificates
- Files exist in `ca/`:
  - `ca.crt`, `ca.key`, `api.openai.com.crt`, `api.openai.com.key`, etc.
- `certutil -dump ca\api.openai.com.crt`:
  - Subject CN = `api.openai.com`
  - SAN includes `DNS Name=api.openai.com`
  - Issuer = `TraeProxy Root CA`
- Interpretation:
  - Leaf certificate content is structurally correct for hostname.

### 5. Verify Windows trust store
- Queried LocalMachine Root store:
  - Found `TraeProxy Root CA` with thumbprint:
    - `675A7D6C94D488171C64F5EC1F10D9B54085F455`
    - NotBefore `2026-04-16 17:22`
- Computed current file CA hash:
  - `certutil -hashfile ca\ca.crt SHA1` ->
  - `C75F7E1ACF215C47EF7E77D23D2B664C460A11CE`
- `certutil -verify ca\api.openai.com.crt` failed with signature validation error against machine-trusted CA chain.
- Interpretation:
  - Trusted root in Windows is an **older TraeProxy CA**
  - Current leaf cert is signed by a **newer/re-generated CA** in `ca/`
  - This mismatch causes browser unsafe warning.

## Root Cause
**CA mismatch between filesystem CA and OS-trusted root CA**.

`ca/` contains a new CA, but Windows trust store still trusts an older `TraeProxy Root CA` certificate.
Browser trust uses OS trust store, not just files in project directory.

## Final Solution
Use admin PowerShell to remove old trusted CA and install the current `ca/ca.crt`:

```powershell
certutil -delstore Root 675A7D6C94D488171C64F5EC1F10D9B54085F455
certutil -addstore -f Root .\ca\ca.crt
```

Then restart browser (or `chrome://restart`) and revisit:

```text
https://api.openai.com/v1/models
```

## Principles (Why this fix works)

1. TLS trust is chain-based
- Browser must validate: leaf cert -> issuing CA -> trusted root in OS store.
- Having cert files in project directory does not establish trust.

2. Root CA identity is key material, not just subject name
- Two certs can both be named `TraeProxy Root CA` but be cryptographically different.
- Subject name match is insufficient; public key/thumbprint must match issuer used to sign leaf cert.

3. Regeneration requires trust-store synchronization
- If `ca/ca.crt` is regenerated, any previously installed root CA with same name becomes stale.
- Always re-install current root CA after regeneration.

4. Verify with fingerprints, not assumptions
- Use SHA1/SHA256 thumbprints to confirm “trusted CA” equals “signing CA on disk”.

## Quick Verification Checklist (for future)

1. `api.openai.com` resolves to `127.0.0.1`
2. `main.exe` is listening on `443`
3. `ca\api.openai.com.crt` SAN includes `api.openai.com`
4. Trusted root thumbprint equals current `ca\ca.crt` thumbprint
5. Browser restart after trust-store update

