# 2026-11-13（登录后立即初始化管理员角色）

- 日期：2026-11-13
- 范围：`server/internal/auth/handlers.go`、`server/internal/auth/http_test.go`、`web/src/views/LoginView.vue`、`web/src/views/LoginView.test.ts`、`web/src/lib/api/apiTypes.ts`
- 做了什么：`POST /api/auth/login` 响应新增 `is_admin`（与 `/api/auth/me` 同样取 `Service.IsAdmin`），登录页 `setUser` 写入 `isAdmin`（#1137）。
- 为什么：登录页只用登录响应初始化用户，响应里没有角色；路由守卫已有用户时不会再请求 `/auth/me`，所以管理员在本次页面会话里一直被当成普通用户，品牌设置等控件只读，刷新后才恢复。只是前端显示问题，后端管理员校验不受影响。
- 如何验证：新增 `TestLoginHandlerReportsAdminRole`（管理员 / 普通账号的登录响应与 `/me` 一致）和 `LoginView.test.ts` 的 `is_admin` 三例（true / false / 缺失）；临时去掉改动时两边新测试均失败。另用真实服务端 + 生产构建前端在 Playwright 里登录管理员和普通账号，核对设置页品牌输入框的可编辑状态。server 与 web 全部门禁通过。
