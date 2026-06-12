# 变更说明：将可运营配置迁移到 `system_settings`

## 目的

- 减少将系统设置、登录安全和通知设置硬编码在 `panel.env` 或 Go 默认值中的依赖。
- 让管理员在后台修改后的配置直接进入数据库，而不是回写部署环境文件。
- 项目按首次使用处理，不保留旧环境变量到数据库的迁移方案。

## 范围

- 复用表：`system_settings`
- 迁移的设置键：
  - `site_title`
  - `site_icon_url`
  - `public_api_base_url`
  - `login_background_url`
  - `bruteforce_enabled`
  - `bruteforce_max_attempts`
  - `bruteforce_window_minutes`
  - `bruteforce_lock_minutes`
  - `smtp_enabled`
  - `smtp_host`
  - `smtp_port`
  - `smtp_encryption`
  - `smtp_username`
  - `smtp_password`
  - `smtp_from_email`
  - `smtp_from_name`
  - `smtp_notify_email`

## 执行顺序

1. 确认 `system_settings` 表已存在
2. 部署新版本 Go 后端
3. 首次安装或服务启动时，把系统默认值和初始化输入写入数据库缺失键
4. 后续后台保存系统设置或通知设置时，仅更新数据库

## 回滚方案

1. 回滚应用版本到仍然依赖 `panel.env` 的旧版本
2. 如需恢复文件配置，手动将 `system_settings` 中对应键值回填到 `panel.env`
3. 不建议直接删除表数据，避免新版本运行时丢失设置

## 验证方式

- 后台保存系统设置后，确认 `system_settings` 中对应键值更新
- 后台保存通知设置后，确认 SMTP 相关键值更新
- 重启 Go 面板后，确认上述设置仍然生效
- 检查 `panel.env`，确认不再写入这些运行时配置

## 风险

- 若数据库不可用，新版本只能回退到环境变量中的旧值或代码默认值
- 首次安装后若未完成数据库初始化，这些配置不会落盘到 `panel.env`
