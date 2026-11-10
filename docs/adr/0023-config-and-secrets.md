# Configuration is JSON with environment-variable references for secrets

Non-secret settings live in `.yangjian/config.json` (or `~/.config/yangjian/config.json`): provider profiles, rate limits, default chain, cache paths, and budgets. Sensitive values are referenced by environment-variable name (`api_key_env`) and never stored in the repository or config file. Precedence is CLI flag, then environment, then config, then defaults. JSON avoids an additional parser dependency and can be validated with JSON Schema; `yangjian config init` generates a documented example.
