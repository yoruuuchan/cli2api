<div align="center">

<h1><img src="./frontend/public/apple-touch-icon.svg" width="40" height="40" align="top" alt=""> CLI2API</h1>

**Turn your own logins into a local OpenAI-compatible API**

Connect **Qoder (Global / CN)**, **WorkBuddy (Global / CN)**, **Trae CN Work**, and experimental **Devin** and **Command Code** accounts.

Deploy with Docker, manage accounts in the web console, and connect clients through compatible APIs.

[![License](https://img.shields.io/github/license/caigee-cmd/cli2api)](LICENSE)
[![Telegram](https://img.shields.io/badge/Telegram-community-26A5E4)](https://t.me/+jCjL8ou_g7Q2MzI1)

<sub>[中文](README.md) · [Issues](https://github.com/caigee-cmd/cli2api/issues) · [Telegram](https://t.me/+jCjL8ou_g7Q2MzI1)</sub>

<img src="./docs/assets/readme/hero-en.svg" width="100%" alt="CLI2API — turn your own logins into a local OpenAI-compatible API">

</div>

## Features

- **Compatible APIs**: Chat Completions, Responses, Anthropic Messages, and model listing, with streaming and function tools.
- **Multi-account routing**: round-robin, weighted round-robin, fill-first, and opt-in earliest-expiry-first (FEFO), with session affinity, concurrency limits, cooldowns, and failover.
- **Web console**: manage accounts, models, client keys, and proxies; view quotas and request logs.
- **Docker operations**: persistent account data; an optional host updater lets you download, confirm, and roll back updates from the console.

Qoder CN, WorkBuddy, and Trae adapters are implemented, but live-account acceptance is still pending. Devin and Command Code are experimental, not production-ready. Live upgrade and rollback acceptance is also pending.

## Quick start

You need Docker and an upstream account you control. Use Docker Desktop on macOS / Windows; Windows must use Linux containers.

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api
./scripts/start.sh        # Windows: scripts\start.ps1
```

1. Save the **administrator key** printed in the first-start logs.
2. Open `http://127.0.0.1:3010`, sign in with that key, and add an account in **Accounts**.
3. Create a client key in **API keys**, then choose a model and test it in **Access**.

Docker Compose is the supported install and managed-update path; source runs are for development. See the [deployment guide](deploy/README.md).

## Connect a client

In an OpenAI-compatible client, enter:

```text
Base URL: http://127.0.0.1:3010/v1
API Key:  <a client key created in API keys>
```

Get a model ID from **Access** or `/v1/models`. Routing is automatic and prefers the same account for later turns. Account pinning, session headers, and curl / PowerShell examples are in the [deployment guide](deploy/README.md).

## How it works

<p align="center">
  <img src="./docs/assets/readme/architecture-en.svg" width="100%" alt="CLI2API architecture: OpenAI clients are routed by the Go control plane to one isolated runtime per account, then to the provider upstream">
</p>

The Go gateway handles authentication, routing, and request logging. Each Qoder account has its own Node process and HOME; WorkBuddy, Trae, Devin, and Command Code use Go in-process adapters. No full CLI is started per request.

## Console

<p align="center">
  <img src="./docs/assets/readme/console-window-en.svg" width="100%" alt="CLI2API console Accounts page: each account shows its login method, ready state, and quota, with an Access panel offering the Base URL and a quick check">
</p>

Accounts, models, access, and logs all live in one web console: readiness and quota are visible at a glance, and the Access page lets you copy the Base URL and run a quick check.

Account cards provide WorkBuddy, QwenWork CN/Global, Qoder CN, and Qoder Global check-in actions and automatic check-in switches (off by default), with history under More → Check-in records; no separate check-in page is needed. Set each provider's default time under System → Automatic check-in, then choose inheritance or an override in Edit account. Default changes apply to inheriting accounts immediately. Existing WorkBuddy accounts retain their previous times until switched to inheritance. Inactive campaigns are skipped. Live-account check-in acceptance is still pending. QwenWork ships a check-in API in its desktop client, but production gateways do not route it (404): CN confirms the daily free credits from the wallet balance, while Global can only report the total quota and the skip reason.

## Earliest-expiry-first (FEFO)

Select **Earliest expiry first (FEFO)** under **System → Account routing strategy**, or send `{"routing_strategy":"fefo"}` to `PATCH /api/system/settings` with administrator authentication. Round-robin remains the default; existing weighted round-robin and fill-first behavior is unchanged.

- **Establish applicability first.** Model compatibility, health, grants, region, concurrency, and cooldown gates still apply. Only positive, unexpired packages that can actually fund the model qualify. Free models, Auto, and unknown prices are not assumed to spend credits; explicitly free models can still use exhausted accounts.
- **Rank by the nearest spendable package date.** Ignore depleted/expired packages on multi-package accounts. Known priority requires provider-confirmed applicability and earliest-expiring-first upstream deduction. Accounts expiring on the same **UTC+8 calendar day** rotate fairly, regardless of weights. Unknown dates/scopes come after known batches; an all-unknown cohort uses the existing round-robin rather than guessed dates.
- **Keep established bindings.** Session affinity and explicit account pins precede FEFO; existing failure, cooldown, concurrency, and quota escape rules still apply. Finishing a response/stream asynchronously coalesces quota refreshes per account; fresh upstream balances immediately affect ranking. Check-ins, manual refreshes, and cached packages expiring also affect the next selection. There is no estimated balance or guarantee of upstream debit timing.
- **Use one display calculation.** Cards and overview show the nearest active date, local calendar days left, and credits expiring that day. Expand all package balances/dates, including depleted/expired packages. Dates use the browser's timezone, which can differ from the routing calendar. Missing expiry explicitly says **Expiry unknown**; reset windows, token lifetimes, and unparsed date text are never substituted.

Current support:

| Provider / account type | FEFO and date source |
| --- | --- |
| WorkBuddy CN / Global personal accounts | Known positive-price models with complete, unrestricted `p_tcaca` credits packages; native `CycleEndTime` in UTC+8 and precise decimal balances. Deduction order is grounded in the [CN credit rules](https://www.codebuddy.cn/docs/ide/Account/credits) and [WorkBuddy credit rules](https://www.codebuddy.ai/docs/zh/workbuddy/credits), not inferred from account totals. |
| WorkBuddy enterprise/shared identities, unconfirmed lists reaching 100 results, unknown products/units/deduction restrictions | No known FEFO priority. Real package dates may still be displayed without claiming that a request can spend those packages. |
| Qoder, Trae, Devin, Command Code, Codex, Zhipu, QwenWork | Current adapters do not provide verifiable per-package expiry plus applicability/deduction order; retain unknown priority and round-robin fallback. Reset windows, subscription dates, and check-in reward definitions are not expiry of spendable packages. |

FEFO selects accounts; it does not change upstream billing, plans, or region rules. Refresh quotas and model catalogs before expecting old caches to acquire verified FEFO metadata.

## Limitations

- Bring your own accounts. CLI2API does not supply accounts, quotas, or an official API service.
- Compatibility is not full API parity. Messages / Responses are stateless adapters, without server-side conversations or upstream-specific tool execution.
- Image support depends on the upstream and model. WorkBuddy / Trae do not support images; file inputs are rejected. See the [deployment guide](deploy/README.md) for details.
- Cross-provider routing for shared model IDs follows system settings, key grants, and region constraints; failover is not unrestricted.
- Upstream changes can break compatibility. Request logs do not store prompt or completion bodies by default.

## Documentation

- [Deployment and operations: setup steps, environment variables, endpoints, managed updates](deploy/README.md)
- [Changelog](CHANGELOG.md)

## Security

The default deployment exposes only `127.0.0.1:3010`; do not expose it directly to the public internet. Administrator keys manage the console; client keys only access `/v1/*`. Protect keys, credential exports, and database backups. Report security issues privately as described in [SECURITY.md](SECURITY.md).

## Community & Contributing

Discussion is in the [Telegram group](https://t.me/+jCjL8ou_g7Q2MzI1). In China you can also scan the personal WeChat QR below. Bugs and feature requests go to GitHub [Issues](https://github.com/caigee-cmd/cli2api/issues); documentation improvements and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

<p align="center">
  <img src="./docs/assets/readme/wechat-contact.jpg" width="220" alt="Personal WeChat QR code">
</p>

## Acknowledgements

Thanks to open-source projects including `workbuddy2api`, `CLIProxyAPI`, and `sub2api` for ideas and inspiration.

## License

[MIT](LICENSE). Follow each upstream platform's terms when using its accounts.
