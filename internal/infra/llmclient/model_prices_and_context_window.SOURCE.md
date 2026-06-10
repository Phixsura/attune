# LiteLLM Model Prices

`model_prices_and_context_window.json` is vendored from LiteLLM:

- Source: <https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json>
- License: MIT, see `model_prices_and_context_window.LICENSE`.
- Purpose: estimate LLM prompt/completion token cost from provider-reported usage.
- Drift monitor: `.github/workflows/litellm-price-catalog.yml` opens a weekly
  update PR when upstream changes.

Update with:

```sh
curl -L https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json \
  -o internal/infra/llmclient/model_prices_and_context_window.json
```
