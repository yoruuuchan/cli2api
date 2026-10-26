### English

- Add opt-in earliest-expiry-first (FEFO) routing for verified WorkBuddy personal credit packages and known metered models, with UTC+8 same-day rotation, conservative unknown fallback, and coalesced quota refresh after responses/streams. Preserve existing strategies, affinity, region/grant/concurrency gates, cooldowns, and failover.
- Show the nearest active expiry, local calendar days left, and expiring balance on account cards; expand all package balances/dates, including depleted, expired, and explicitly unknown packages. Share overview calculations and exclude already-expired-today packages from upcoming credits.
- Preserve WorkBuddy precise fractional balances and apply package expiry/summary filtering to spendable packages; do not mistake subscription resets, token expiry, enterprise/shared balances, incomplete lists, unknown scopes, or other providers' reset windows for FEFO package metadata.

### 中文

- 增加可选的最早到期优先（FEFO）调度，支持已核验 WorkBuddy 个人积分包与明确收费模型，按 UTC+8 同日公平轮询、未知保守兜底，并在响应/流结束后合并刷新额度；保留原有策略、会话粘性、地域/权限/并发约束、冷却及故障切换。
- 账号卡片显示最近有效到期日期、本地自然日剩余天数及该日到期积分，可展开全部包的余额/日期（含已耗尽、已过期和明确未知）；与概览共用计算，今日较早已失效的包不再计入即将到期积分。
- 保留 WorkBuddy 精确小数余额，按仍可消费包汇总到期信息；不将订阅重置、令牌有效期、企业共享余额、不完整列表、未知适用范围或其他 Provider 的重置窗口误作 FEFO 积分包元数据。
