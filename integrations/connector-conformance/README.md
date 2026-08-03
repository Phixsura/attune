# Connector Conformance

Attune connectors must prove the same lifecycle before they are treated as
platform-ready:

1. Install metadata declares credential type, scopes, webhook events, and docs.
2. Webhook delivery fixtures carry a deterministic signature header.
3. Fixture replay normalizes provider payloads into Attune object records.
4. Field mappings cover required local and external fields.
5. Provider failures classify into retry, reauthorize, dead-letter, or manual
   review recovery modes.

Run the repository gate with:

```sh
make connector-conformance
```

The gate intentionally uses only Node.js standard library modules so it can run
in local hooks, CI, and SDK release checks without provisioning provider
accounts.

## Adding a Provider

Add a provider entry to `manifest.json`, include at least one signed webhook
fixture under `fixtures/<provider>/`, and extend `sdk/connector-sdk.mjs` only
when the normalized provider payload cannot be expressed with the existing
contract. Every provider fixture must include:

- `rawBody`: the exact bytes signed by the provider webhook.
- `headers`: delivery, event type, and signature headers.
- `expected`: normalized Attune fields used by the replay assertion.

The conformance gate is a compatibility contract. Product UI may show live
evidence from Console APIs, but the manifest and fixtures remain the source of
truth for connector SDK readiness.
