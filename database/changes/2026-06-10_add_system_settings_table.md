# 变更说明：新增 `system_settings` 表

## 目的

- 将登录页背景图片 URL 从浏览器本地存储迁移为服务端数据库持久化。
- 为后续可扩展的系统级键值配置提供统一存储位置。

## 范围

- 新增表：`system_settings`
- 首个使用键：`login_background_url`
- 影响接口：
  - `GET /api/app-settings`
  - `GET /api/system-settings`
  - `PUT /api/system-settings`

## 表结构

- `setting_key VARCHAR(128) PRIMARY KEY`
- `setting_value TEXT NOT NULL`
- `updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP`

## 执行顺序

1. 先执行 `database/migrations/2026-06-10_add_system_settings_table.sql`
2. 再部署包含 Go 后端和前端改动的新版本
3. 管理员在系统配置中保存一次背景图片 URL 后，服务端开始从数据库返回该值

## 可重复执行性

- 迁移脚本使用 `CREATE TABLE IF NOT EXISTS`
- 表内数据写入使用 `INSERT ... ON DUPLICATE KEY UPDATE`
- 同一脚本重复执行不会重复建表，也不会制造重复记录

## 回滚方案

1. 回滚应用版本到未使用 `system_settings` 的版本
2. 如需删除数据，可先备份后执行：
   - `DELETE FROM system_settings WHERE setting_key = 'login_background_url';`
3. 如需完全回滚结构，可确认无其他设置键使用后再执行：
   - `DROP TABLE system_settings;`

## 验证方式

- 执行迁移后确认 `system_settings` 表已创建
- 在后台保存背景图片 URL 后，确认表内存在 `login_background_url`
- 刷新登录页、切换浏览器会话后，确认背景图仍由服务端返回

## 风险

- 旧版本前端写入过的浏览器本地背景图不会自动回填到数据库，需管理员重新保存一次
- 若数据库不可用，背景图将退回为空值，但不会影响登录和主功能
