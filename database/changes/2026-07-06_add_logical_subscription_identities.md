# 增加逻辑用户订阅身份

## 目的

让订阅链接绑定同名逻辑用户，而不是绑定某个节点上的用户副本。只有管理员主动刷新订阅链接时，旧链接才失效。

## 范围

- 新增表：`hysteria_subscription_identities`
- 字段：`username`、`public_id`、`token_secret`、`refreshed_at`、`created_at`、`updated_at`
- 约束：`username` 唯一，`public_id` 唯一

## 执行顺序

1. 执行 `database/migrations/2026-07-06_add_logical_subscription_identities.sql`
2. 部署新版面板
3. 打开用户订阅信息，确认同名用户只展示一条稳定订阅链接

## 回滚

如需回滚到旧的节点副本订阅方式，先部署旧版程序，再执行：

```sql
DROP TABLE IF EXISTS hysteria_subscription_identities;
```

## 验证

- 同名用户在多个节点存在时，订阅链接不因子节点顺序变化而改变
- 编辑用户配额、状态、到期时间后，订阅链接不自动失效
- 点击刷新订阅链接后，旧 token 返回无效，新链接可正常订阅

## 风险

上线后新展示的链接会使用逻辑用户订阅身份。已经复制出去的旧 `usr_...` 节点副本链接应继续兼容；管理员点击刷新订阅链接后，该逻辑用户的旧链接和旧 token 才失效。
