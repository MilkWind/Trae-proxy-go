# Model Match Rules and Fallback Trick

This document explains how model routing works in this project and how to force unknown client model IDs to a specific backend model.

## Current Match Rules

Routing logic (from `internal/proxy/router.go`) is:

1. Exact match: choose API where `active: true` and `custom_model_id == request.model`
2. No exact match: choose the first API where `active: true`
3. If none active: choose the first API in `apis`

After backend selection:

- Request model is rewritten to `target_model_id`
- Response model is rewritten back to `custom_model_id`

This means the client can send one model ID while the proxy actually calls another model ID on the real backend.

## Practical Trick: Force Unknown IDs to One Target Model

If you do not know what model ID the client will send, configure a default/fallback API as the first active entry:

1. Put the desired backend API first in `apis`
2. Keep it `active: true`
3. Set `target_model_id` to the real backend model you want
4. Optionally set other APIs to `active: false` for strict force-routing

Example:

```yaml
apis:
  - name: force-default
    format: openai
    endpoint: https://api.openai.com
    custom_model_id: default-visible-model
    target_model_id: gpt-4.1
    active: true

  - name: another-route
    format: openai
    endpoint: https://api.openai.com
    custom_model_id: gpt-4.1-mini
    target_model_id: gpt-4.1-mini
    active: false
```

Result:

- If client sends unknown `model`, request still goes to `force-default`
- Proxy forwards upstream as `gpt-4.1`
- Client sees response model as `default-visible-model`

## Notes

- Exact matches still win over fallback.
- Keep API format/path compatible with the request type:
  - `/v1/chat/completions` flow should point to an OpenAI-compatible backend
  - `/v1/responses` flow should point to a Responses-compatible backend (or OpenAI chat backend if bridge mode is expected)
  - `/v1/messages` flow should point to an Anthropic-compatible backend
- If you need more advanced rules (prefix/regex/domain-based), add custom logic in `selectBackendByModel`.

## Domain Handling (Important)

If you do not know what domain the client will request, but that domain is already listed in `domains`, this proxy can still work, with these conditions:

1. `domains` is used for TLS certificate loading, not backend model routing.
2. Backend selection is still decided by `request.model` using the rules above.
3. The domain must have valid cert files:
   - `ca/<domain>.crt`
   - `ca/<domain>.key`
4. The client must resolve that domain to this proxy (via hosts/DNS).
5. New domain certs are loaded at server startup, so after adding a new domain/cert, restart the proxy.

Practical interpretation:

- Domain match controls whether HTTPS handshake can succeed for that host.
- Model match controls which upstream API/backend is used.
