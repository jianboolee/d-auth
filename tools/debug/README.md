# tools/debug —— 调试工具集

> **不是项目代码。**
>
> 本目录是独立调试工具，**不参与 d-auth 的研发、构建、测试与部署**：不进任何
> 镜像或裸机产物，不被 `api/` 或 `web/` 引用，也不挂到仓库根的 `Makefile`。
> 它有自己的 `go.mod` 和 `Makefile`，需要时手动进来跑。
>
> 只对**开发库**使用。两个工具都默认拒绝非本机数据库，远程库必须显式加
> `-allow-remote`。

## 两个工具

| 目录 | 作用 | 会写什么 |
| --- | --- | --- |
| [`seed-users`](seed-users/) | 生成一批测试用户，用于调列表/筛选/分页 | 46 个用户（26 英文名 + 20 中文名），虚拟号、无密码 |
| [`make-admin`](make-admin/) | 创建/修复一个固定的测试管理员，用于登录后台 | 1 个带密码的 superadmin |

`seed-users` 造的是「列表里的数据」，故意不设密码，只能用短信登录（而虚拟号
收不到码，所以它们登不进去）；`make-admin` 造的是「操作者的账号」。两者责任
不同，刻意分成两个子命令。

## 测试管理员凭据

`make-admin` 的默认账号如下，**仅用于本地开发库**，记在这里以免每次都翻库找：

| 项 | 值 |
| --- | --- |
| 登录标识 | `12000000001` |
| 密码 | `dev-admin-password` |
| 昵称 | 调试管理员 |
| 角色 | `superadmin`、`admin`、`user` |

用**虚拟号**当登录标识，是为了顺便验证「密码登录放行 12 开头虚拟号」这条规则
（验证码登录仍然拒绝它）。

> 这个账号在本地库里一旦建好就会一直存在。`make-admin` 是幂等的：账号被误改
> 状态、误清密码、甚至被软删（`deleted_at` 有值）之后，重跑一次即可修回可用。

## 用法

```bash
cd tools/debug

make help
make admin            # 创建/修复测试管理员
make seed             # 插入第 1 批测试用户
make seed BATCH=2     # 第 2 批：昵称加 -2 后缀，手机号换号段
make seed-dry         # 只看会创建谁、跳过谁
```

也可以直接跑单个二进制：

```bash
./bin/make-admin -dry-run
./bin/seed-users -batch 3 -spread 30m -dry-run
```

## 连接哪个库

优先级：`-dsn` 参数 > 环境变量 `DAUTH_POSTGRES_DSN` > 仓库根 `.env`。
运行时会打印实际连到的库（不含密码），先看清那一行再确认写入。

## 清理

两个工具都不做删除。手工清理时按手机号前缀精确匹配，先 SELECT 确认命中范围：

```sql
-- seed-users 第 1 批（12001 开头）
SELECT u.id, u.nickname, i.identifier
FROM auth_users u JOIN auth_identities i ON i.user_id = u.id
WHERE i.type = 'phone' AND i.identifier LIKE '12001%';

-- make-admin 的测试管理员
SELECT u.id, u.nickname, i.identifier
FROM auth_users u JOIN auth_identities i ON i.user_id = u.id
WHERE i.type = 'phone' AND i.identifier = '12000000001';
```

删除顺序：先删 `auth_identities`，再按 id 列表删 `auth_users`
（`auth_user_roles` 有 `ON DELETE CASCADE`）。别只按昵称删——昵称可能和手工
建的用户重名。