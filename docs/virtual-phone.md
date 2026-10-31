# 后台虚拟手机号

> 需求记录（2026-09-15）。本次改动只放宽**后台**建号与改绑的手机号校验，
> 登录、注册、短信、自助改绑的严格校验保持不变。

## 需求原文

> 后台添加用户的时候允许使用虚拟手机号，以 120 开头的，提供一个服务端生成虚拟手机号的能力，
> 管理后台添加用户的时候加一个"生成虚拟号"的按钮，自动填入。手机号判断调整一下，允许**任意手机号**。
> 这里的调整**仅限后台添加、编辑用户的接口**。登录注册端还是按正常处理。并记录一下这次需求。

拆成四条：

1. 虚拟号形态：`120` 开头。
2. 服务端提供生成虚拟号的能力（不是前端写死拼字符串）。
3. 后台添加用户表单加「生成虚拟号」按钮，点了自动填入。
4. 手机号校验放宽，但**只作用于后台的建号与改绑两个入口**。

## 接口契约

| 项 | 值 |
| --- | --- |
| 方法 / 路径 | `POST /v1/admin/virtual-phone` |
| 鉴权 | 管理端登录态 + scope `users:write` |
| 请求体 | 无 |
| 响应 | `{ "phone": "120xxxxxxxx" }`（120 + 8 位随机数字，共 11 位） |

几点约定：

- **只返回候选值，不写库**。号码在用户真正提交建号表单时才落库。
- 生成时会先查 `auth_identities` 避开已占用的号码；真正的唯一性仍由
  `auth_identities (type, identifier)` 唯一索引在写入时兜底（并发下也不会插重）。
- 随机源用 `crypto/rand`，不是 `math/rand`。
- 连续 8 次都撞到已占用号码才会返回 `PHONE_GENERATE_FAILED`；号码空间 1e8，正常一次命中。

## 虚拟号的语义（重点）

**虚拟号不是「能收码」的登录凭据，但允许密码登录。**（2026-10-31 调整）

`12` 不是真实号段，面向用户的 `normalize.Phone` 会直接拒绝它。因此虚拟号：

- **不能注册**、不能用来接收短信验证码（`SendSMS` / `SMSLogin` 走的是严格校验，
  在发码前就挡掉）；
- **可以用于密码登录**：后台给运营/测试账号建的虚拟号需要能登进后台，而它收不到
  短信，密码登录是唯一出路。这条路径不发码、不建号，校验的只是「库里已有该标识 +
  密码正确」，因此放行不会产生新的注册入口；
- 注册之外，它仍是一个占位标识，让后台能建出不占用真实号码的用户。

实现上放宽不是改 `normalize.Phone` 的全局行为，而是给密码登录一个专用入口
`normalize.PasswordIdentifier`：先走严格 `Identifier`，没过再额外认 **12 开头**的
纯数字（限 6–15 位，规范化时原样保留、不套 `+86`，以便精确命中后台落库的值）。
只认 `12` 而不是任意数字串，是为了让虚拟号可登录的同时不把手机号字段变成任意用户名。
`SendSMS`、`SMSLogin`、注册、自助改绑仍走严格校验，行为不变。

> 早期版本的本文档写过「虚拟号不能用于登录」。那句话在 2026-10-31 之后不再成立：
> 密码登录放行了 `12` 开头的虚拟号，短信验证码登录仍然拒绝。

## 校验放宽的边界

后台建号（`CreateUser`）与改绑（`SetUserIdentity`）走 `normalize.AdminPhone`：

| 输入 | 结果 |
| --- | --- |
| `13800138000` | `+8613800138000`（真实号码，与严格校验一致） |
| `+8613800138000` | `+8613800138000` |
| `12012345678` | `12012345678`（虚拟号，**原值保留**） |
| `120 1234-5678` | `12012345678`（去空格与连字符） |
| `12012` | 拒绝（少于 6 位） |
| `1234567890123456` | 拒绝（超过 15 位） |
| `120abcd5678` | 拒绝（含字母） |

两条规则：

1. 先按严格的 `Phone` 处理 —— 真实号码的规范化结果与从前完全一致，不存在行为回退。
2. 其余**纯数字 6–15 位**原样保留，**不猜国码**。虚拟号就属于这一类，刻意不套 `+86`，
   否则库里存的值（`+86120...`）与用户看到的、以及前端回填的值会不一致。

对照严格校验：

| 号码 | 后台建号 / 改绑（`AdminPhone`） | 登录 / 注册 / 自助改绑（`Phone`） |
| --- | --- | --- |
| `13800138000` | 通过 | 通过 |
| `+14155552671` | 通过 | 通过 |
| `12012345678` | **通过** | 拒绝 |
| `10012345678` | **通过** | 拒绝 |
| 含字母 / 过短 | 拒绝 | 拒绝 |

## 实现分布

后端：

| 文件 | 改动 |
| --- | --- |
| `api/internal/normalize/normalize.go` | 新增 `AdminPhone`（宽松）+ 内部 `isASCIIDigits`；`Phone` **未动** |
| `api/internal/modules/admin/service.go` | `CreateUser`、`SetUserIdentity` 的手机号分支改走 `AdminPhone`；新增 `GenerateVirtualPhone` 与 `newVirtualPhone` |
| `api/internal/httpx/handlers.go` | 新增 `AdminGeneratePhone` handler |
| `api/internal/httpx/router.go` | 注册 `POST /v1/admin/virtual-phone`（scope `users:write`） |

前端 / SDK：

| 文件 | 改动 |
| --- | --- |
| `packages/sdk/src/index.ts` | 新增 `adminGenerateVirtualPhone()`；`dist/` 重新构建并提交 |
| `packages/admin/src/phone.ts` | 新增 `adminPhoneLooksValid`（与 `AdminPhone` 对应）、`randomVirtualPhone`（兜底）；`phoneLooksValid` **保持严格**，自助流程仍在用 |
| `packages/admin/src/pages/CreateUserPage.vue` | 手机号输入框外套 `n-input-group`，右侧加「生成虚拟号」按钮；提交前用 `adminPhoneLooksValid` 校验 |
| `packages/admin/src/pages/UserDetailPage.vue` | 改绑手机号改用 `adminPhoneLooksValid`；提示文案改为说明虚拟号只能密码登录 |

**未触碰**（登录注册端「按正常处理」）：`normalize.Phone` 及其全部调用方，
包括 `SendSMS`、`SMSLogin`、`ChangePhone`，以及前端自助页 `LoginPage.vue`、
`BindPhonePage.vue`、`SecurityPage.vue`。

**2026-10-31 追加的改动**（密码登录放行虚拟号）：只有两处——

| 文件 | 改动 |
| --- | --- |
| `api/internal/normalize/normalize.go` | 新增 `PasswordIdentifier`：严格 `Identifier` 之外多认 `12` 开头纯数字；`Identifier` 与 `Phone` **未动** |
| `api/internal/modules/auth/service.go` | `PasswordLogin` 改用 `PasswordIdentifier` |

前端 `packages/admin/src/phone.ts` 未动：登录页只校验非空，号码格式由后端判定。

## 验证

```bash
cd api && go build ./... && go vet ./...
go test ./internal/normalize/...          # 不依赖外部服务

# 真库用例（后台建号/改绑的虚拟号路径需要唯一索引与真实约束）
PG_TEST_DSN="postgres://postgres:postgres@localhost:5432/<测试库>?sslmode=disable" \
  go test ./internal/modules/admin/...

npm run build:sdk && cd web && npx vue-tsc --noEmit && cd .. && npm run build:web
```

新增用例：

- `normalize_test.go`：`TestAdminPhone`（真实号码结果与 `Phone` 一致、虚拟号原值保留、边界拒绝）、
  `TestPhoneStillRejectsVirtual`（守住「严格校验不认虚拟号」这条前提）。
- `admin/service_test.go`：`TestCreateUserWithVirtualPhone`、`TestCreateUserRejectsDuplicateVirtualPhone`
  （虚拟号重复建号要报 `PHONE_TAKEN` 而不是撞索引变 500）、`TestGenerateVirtualPhone`
  （形态正确且不会返回已占用的号）。

## 取舍

- **放宽放在后端，而不是前端遮一下**。前端只做即时提示，真正的准入判定在后端；
  否则绕过前端就能把任意字符串写进手机号字段，且会污染唯一索引。
- **生成接口不预占号码**。预占需要写一张临时表或带 TTL 的 Redis key，而号码空间有 1e8，
  撞号概率极低，加上唯一索引兜底，预占的复杂度换不来收益。
- **虚拟号不做特殊标记位**。`auth_identities` 里它就是一个普通的 `phone` 标识，
  不新增 `is_virtual` 列：判定"是不是虚拟号"用 `120` 前缀即可，避免为展示需求改表。
  代价是将来若要统计虚拟号数量，只能按前缀模糊匹配。
- **虚拟号与真实号码共享同一唯一索引**。同一号码不会既被真实用户又被虚拟账号占用；
  若某天 120 号段被正式启用，需要先清理已发出的虚拟号。
