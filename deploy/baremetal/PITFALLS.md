# 裸机部署：踩坑记录与现状

这份文件记录的是**实施过程中真实遇到的问题**和**当前完成度**，不重复使用说明。
用法、命令、目录结构请看同目录的 [README.md](README.md)。

写给谁：以后要部署 d-auth 的人，或想把这套方案搬到别的项目的人。
**先读 [第 1 节 现状](#1-当前状态)**，那里有「已验证 / 未验证」的明确分界——
不要把未验证的部分当成能用。

---

## 1. 当前状态

记录日期：2026-10-31（已上生产验证）

### 1.1 完成度

| 项目 | 状态 |
| --- | --- |
| 本地构建链路（`make bare-build`） | ✅ 实跑通过，产出 11MB tar 包 |
| Go 交叉编译 | ✅ 实测产物 `statically linked` ELF x86-64 |
| 二进制能否启动 | ✅ 起真实 Postgres + Redis 跑通，5 个迁移全部执行 |
| nginx vhost 语法 | ✅ 真实 nginx 1.27 / 1.24 容器 `nginx -t` 通过 |
| 静态托管 + `/v1` 反代 + `/health` 反代 | ✅ 端到端实测通过 |
| SPA fallback 与 `/assets/` 缓存头 | ✅ 实测通过 |
| `.env.prod` 校验逻辑 | ✅ 双向测试（干净环境通过、故意加冲突键被拒） |
| 端口占用检测 | ✅ 三态双向测试：占用能识别 / 空闲正确 / 缺工具时明确告警（Ubuntu + alpine + Alinux 三环境） |
| `install.sh --check` 预检路径 | ✅ alpine、Ubuntu 24.04、生产机三处跑通 |
| **systemd unit 真实加载** | ✅ **生产机验证**：`active` + `enabled`，进程以 `dauth` 用户运行 |
| **`install.sh` 完整执行** | ✅ **生产机验证**：装用户/目录、搬私钥、停容器、装 nginx vhost、起服务、两级探活全通 |
| **`rollback-to-docker.sh`** | ✅ **生产机验证**：切回 Docker 成功，容器 healthy，私钥延续 |
| **双向切换（裸机 → Docker → 裸机）** | ✅ **生产机验证**：两次切换后私钥指纹与 JWKS 完全一致 |
| **公网链路（经入口机）** | ✅ `/health` 200、`/login` 200、`/v1/auth/session` 200、JWKS 一致 |
| 代码提交 | ✅ PR #1，分支 `feat/baremetal-deploy` |

### 1.2 生产验证的关键证据

2026-10-31 在业务机（Alibaba Cloud Linux 3，内网 172.17.255.64）实际切换，
用「私钥指纹 + JWKS 公钥指纹」双重比对证明私钥没有重建：

| 时点 | 私钥 md5 | JWKS kid | 公钥 n 的 sha256 前 32 位 |
| --- | --- | --- | --- |
| 部署前（容器） | `fa89ac05cff3202948ad3c4f7ae858cd` | `tMscXfahUDs` | `5e398793d3c29854f8123bc8fe753c21` |
| 切换后（裸机） | 同上 | 同上 | 同上 |
| 回滚后（容器） | 同上 | 同上 | 同上 |
| 再切裸机（最终） | 同上 | 同上 | 同上 |

**四次测量完全一致**，说明 `install.sh` 的取私钥、`rollback-to-docker.sh` 的
写回卷两条路径都正确。这个比对方式比只看 `/health` 有说服力——健康检查通过
不代表 token 还能被下游验签。

### 1.3 上生产时观察到的两个现象

**① 停容器后有孤儿进程残留（不影响服务）。**
切换后 `ps` 里仍有 `root /app/server` 进程，启动时间为 10 月 29 日 —— 
它们属于**更早消失的旧容器实例**，不是本次停止的那个。判断依据：

- 在独立 PID namespace（`4026532454` / `4026532383`）
- `docker ps -a` 里已无对应容器
- **没有占用 8680 或 8681**（`ss -ltnp` 确认端口只属于 dauth 和 nginx）

结论：无害的历史遗留，可择机 `kill`，但不影响新服务。切换时看到这类进程
不要误以为「容器没停干净」。

**② `/opt/d-auth` 权限原本是 750，已被 `install.sh` 修正为 755。**
生产机上切换前是 `drwxr-x---`（部署前的旧状态），`install.sh` 的
`chmod 755 "$REMOTE_DIR"` 生效后现在是 `drwxr-xr-x`。

这点值得记下来，因为它是**切换前排查时的一个真实干扰项**：勘察服务器看到 750，
会怀疑 nginx 读不到静态文件。验证方式很直接：

```sh
sudo -u nginx test -r /opt/d-auth/web/index.html   # 实测通过
```

同时确认了 nginx worker 以非 root 用户（uid 100）运行，所以 755 是必需的，
不能改回 750。`data/` 目录是 `dauth:dauth`，私钥 `600`，与设计一致。

### 1.4 下一步

1. 用浏览器真实登录一次，确认登录流程端到端可用（本次只验到 HTTP 层：
   `GET /login`、`GET /v1/auth/session` 均 200，未走完整表单提交）
2. 观察几天 `journalctl -u d-auth`，确认无异常
3. 备份 `/opt/d-auth/data/`（私钥 + 上传文件，唯一需要备份的目录）
4. 择机清掉 10 月 29 日的孤儿容器进程（见 1.3 ①，无害，可不处理）

---

## 2. 必须守住的约束（设计期的坑）

这几条是写代码前就识别出来的，已经写进脚本和 unit 里。**改动部署时不要破坏它们。**

### 2.1 JWT 私钥丢了会静默重建（最危险的一条）

`api/internal/infra/tokens/jwt.go:259-275` 的 `loadOrCreateKey`：路径下找不到私钥时，
**不报错**，而是生成一把新的 2048 位 RSA 密钥写盘。

后果是连锁的：

- 所有已签发的 access / refresh token 立即失效，全部用户被登出
- `/v1/.well-known/jwks.json` 换了公钥
- 所有下游 Go 服务（d-camp 等）本地验签全部失败 —— 因为它们缓存了旧公钥

而且**日志里不会有明显错误**，只有一行 `jwt signer ready`，看起来像正常启动。
这是最容易「以为部署成功了，其实整站挂了」的地方。

所以：

- `build.sh` 刻意**不打**私钥进 tar 包（私钥是运行时状态，不是构建产物）
- `install.sh` 在**停容器之前**用 `docker cp` 从运行中的容器取私钥，
  取不到再退回读 `d-auth_app_data` 卷的 Mountpoint
- 两种来源都失败时打印醒目警告，不静默继续
- `rollback-to-docker.sh` 反向把私钥写回数据卷，保证裸机期间签发的 token
  回滚后依然有效

### 2.2 `DAUTH_HTTP_ADDR` 与 `HTTP_PORT` 不能混用

`api/internal/config/config.go:107-110`：

```go
httpAddr := env("DAUTH_HTTP_ADDR", "")
if httpAddr == "" {
    httpAddr = ":" + env("HTTP_PORT", "8681")
}
```

`DAUTH_HTTP_ADDR` 优先。**只设 `HTTP_PORT` 会绑 `0.0.0.0:8681`**，API 直接暴露在
所有网卡上 —— 安全组若放行 8681 就是公网可访问的后端，绕过 nginx 的限流与日志。

unit 里用 `DAUTH_HTTP_ADDR=127.0.0.1:8681` 显式绑回环。
对比：容器方案下这不成问题，因为容器网络本身就是隔离的，`HTTP_PORT=8681` 绑
容器内的 `0.0.0.0` 也只有同容器的 nginx 能连。

### 2.3 systemd 里 `EnvironmentFile=` 会覆盖 `Environment=`

unit 里用 `Environment=` 注入了四个键（`APP_ENV`、`DAUTH_HTTP_ADDR`、
`DAUTH_JWT_PRIVATE_KEY_PATH`，以及 `.env.prod` 里没有的 `RUN_MIGRATIONS` 不用管）。
如果 `.env.prod` 里也有同名键，**文件里的值会赢**，unit 里的值失效。

这会造成典型的「改了 unit 不生效、查半天」问题。所以 `install.sh` 直接拒绝：

```sh
for k in APP_ENV DAUTH_HTTP_ADDR HTTP_PORT DAUTH_JWT_PRIVATE_KEY_PATH; do
  # 发现即 fail，并提示从 .env.prod 删除
```

已验证：给 `.env.prod.example` 追加 `APP_ENV` / `DAUTH_HTTP_ADDR` 后，
预检正确报出这两个键并拒绝执行。

> 说明：这条优先级规则来自 systemd.exec(5) 的既有文档行为，实施时尝试联网核对
> 原文但没抓到该章节。规则本身可靠（变量由文件读取后覆盖先前设置），但如果你在
> 某台机器上遇到不符合的情况，以实测为准。

### 2.4 `/opt/d-auth` 权限必须是 755，不能是 700

`jwt-private.pem` 需要 `600 dauth:dauth`（服务以非 root 的 `dauth` 用户运行），
但**父目录不能跟着收紧**：nginx 以 `nginx` 用户身份读 `/opt/d-auth/web/`，
如果 `/opt/d-auth` 是 700，nginx 连目录都进不去，返回 403。

两者要分开设：目录 755，私钥 600。

### 2.5 8680 是独占端口，两套方案互斥

容器方案映射 `宿主 8680 → 容器 80`，裸机方案 nginx 直接监听 8680。
同一时刻只能有一个占用者。

- `install.sh` 会主动 `compose down` 或 `docker stop d-auth`
- `rollback-to-docker.sh` 会主动 `systemctl disable --now d-auth`
- 两种情况都**不会**同时跑起来抢端口

这样做的收益是入口机 `39.107.77.69` 的 `proxy_pass http://172.17.255.64:8680`
一个字都不用改，切换对上游透明。

---

## 3. 实施时真实踩到的坑

### 3.1 `${CHECK_ONLY:+ (check)}` 在 `CHECK_ONLY=0` 时也会展开

`${VAR:+...}` 判断的是「变量是否非空」，不是「是否为真」。
`CHECK_ONLY=0` 是非空字符串，所以**正常部署时也会打印 `(check)`**，
看起来像进了预检模式。

改成显式判断：

```sh
CHECK_LABEL=""
[ "$CHECK_ONLY" = "1" ] && CHECK_LABEL=" (check)"
```

### 3.2 端口检测的假阳性（花了最多时间的一条）

第一版是这样的：

```sh
port_owner() {
  if command -v ss >/dev/null; then
    ss -ltnp 2>/dev/null | awk -v p=":$1 " '$4 ~ p {print $0; found=1} END{exit !found}'
  elif command -v lsof >/dev/null; then
    lsof -nP -iTCP:"$1" -sTCP:LISTEN 2>/dev/null | tail -n +2   # ← 问题在这
  fi
}
```

两个问题叠在一起：

1. **`lsof` 分支没有按端口过滤**。`-iTCP:PORT` 理论上会过滤，但测试环境里
   `lsof` 返回了大量与本端口无关的 `/run/rosetta/rosetta` 进程（几十行），
   `tail -n +2` 只砍掉表头，剩下的全被当成「占用」。
2. **`ss` 的 `$4 ~ p` 用的是正则**，`:` 和端口号里的字符没转义；
   且 `-ltnp` 不带 `-H` 时有表头，`NR>1` 与 `found` 的配合也不严谨。

结果：预检把 8680 / 8681 都报成「被占用」，还刷出几十行无关进程，
真实信息被淹没。

改成：`ss -ltnH` 取第 4 列（本地地址）精确匹配以 `:PORT` 结尾；
`lsof` 分支再补一层 `grep -F ":PORT"`；输出 `head -3` 限制行数。

修完做了双向验证：

```
# 起真实监听
检测到占用: nc  13 root 3u IPv6 ... TCP *:8680 (LISTEN)
# 空端口
9999 正确报告空闲
```

**教训**：这种「检测型」代码必须反向测 —— 只测「没占用时报空闲」是发现不了
假阳性的。

### 3.2b 端口检测的第二轮修正：不能把「查不了」当成「没占用」

修完假阳性之后，在 **Ubuntu 24.04 minimal** 里测出另一个方向的错误：
该镜像默认**既没有 `ss` 也没有 `lsof`**。第一版在两者都缺失时：

```sh
else
  return 1          # ← 和非 0 输出走同一个分支
fi
```

而调用方是 `if owners=$(port_owner 8680); then ... else ok "$port 空闲"; fi`，
于是「没有检测工具」被渲染成 **「端口空闲」**。这是**假阴性**，比假阳性危险：
端口真被占用时预检照样放行，接着 systemd 启动失败，报的是一个和端口无关的
难懂错误，排查方向直接被带偏。

修法是把结果做成三态，调用方按返回码分支：

| 返回码 | 含义 | 处理 |
| --- | --- | --- |
| 0 | 有进程在监听 | 打印进程；8681 被占则 fail |
| 1 | 确认空闲 | 提示空闲 |
| 2 | 没有 ss/lsof，状态未知 | **明确警告**「无法检测」，安装阶段仍继续但提示看 journalctl |

验证过的三种情况（Ubuntu 容器里实测）：

```
A. 空闲 8681        -> 返回码 1  ✅ 8681 空闲
B. nc 监听 8680     -> 返回码 0  ✅ 识别到 LISTEN 0.0.0.0:8680
C. 藏掉 ss/lsof     -> 返回码 2  ⚠️ 无法检测，状态未知
```

顺带验证了 `ss` 分支本身（此前 alpine 测试从未执行过它，因为 alpine 只有 lsof）：
`-ltnH` 去掉表头后第 4 列是本地地址，按「以 `:PORT` 结尾」匹配。
边界测试：查端口 `680` 不会命中 `8680`。

**教训**：检测型代码要把「真 / 假 / 不知道」区分清楚。把「不知道」折叠进
「假」，在预检这类场景里比直接报错更糟 —— 它给出的是虚假的安全感。

### 3.3 `deploy/.env.prod.example` 结尾没有换行符

测试时用 `printf 'APP_ENV=production\n' >> file` 追加，结果粘到了末行
`TZ=Asia/Shanghai` 后面变成 `TZ=Asia/ShanghaiAPP_ENV=production`，
行首锚点 `^APP_ENV=` 匹配不到，误判成「无冲突」。

虽然是我测试手法的巧合，但暴露了真实问题：**用户照着 `.env.prod.example`
往末尾追加配置时也会遇到同样的粘连**。已给原文件补了结尾换行。

### 3.4 测试环境的坑（不影响生产，但会误导验证）

本地验证脚本踩的：

- **alpine 只有 `lsof`，没有 `ss`** → 端口检测一直走 `lsof` 分支，`ss` 分支
  在早期版本里**从未被真正执行过**。后来在 Ubuntu 里补测才发现缺 `ss`/`lsof`
  时的假阴性（见 3.2b）
- **Ubuntu 24.04 minimal 既无 `ss` 也无 `lsof`** → 需要先
  `apt-get install -y iproute2` 才能测 `ss` 分支
- **alpine 默认无 `bash`** → `bash -n` 检查前要先 `apk add bash`
  （`install.sh` 依赖 bash，不是 POSIX sh）
- `ubuntu:24.04` 第一次拉取超时（`registry-1.docker.io` 连接超时），
  重试一次即成功 —— 是网络抖动，不是镜像问题

生产业务机是 CentOS/Alinux，`ss` 和 `bash` 都有，不受影响。但这些差异说明：
**在单一测试环境里通过，不等于在别的发行版上通过**，尤其是依赖外部命令的代码。

---

## 4. 与 Docker 方案的差异清单

改部署时要清楚哪些是「等价」、哪些是「有意改好」。

### 4.1 行为等价

| 关注点 | Docker | 裸机 | 说明 |
| --- | --- | --- | --- |
| 对外端口 | 8680 | 8680 | 上游无感知 |
| API 端口 | 容器内 8681 | 本机回环 8681 | 都只有 nginx 能连 |
| 前端产物 | 镜像内 nginx 托管 | 系统 nginx 托管 `/opt/d-auth/web` | 同一套 npm 构建 |
| 迁移 | entrypoint `migrate up` + `app.New` 再跑 | 只有 `app.New` | 见 4.3 |
| 进程守护 | `restart: unless-stopped` | `Restart=always` | 能力对等 |
| 环境变量 | compose + `.env.prod` | unit + `.env.prod` | 同一份文件 |

### 4.2 有意改好的两点

**① `X-Forwarded-Proto` 不再被降级。**
`docker/nginx.conf:19` 写死 `proxy_set_header X-Forwarded-Proto $scheme;`。
请求链是「入口机 HTTPS → 业务机容器 nginx」，`$scheme` 在容器 nginx 这里是
`http`，于是后端收到的 `X-Forwarded-Proto` 永远是 `http`，HTTPS 信息丢了。

裸机版用 `map` 保留入口机传下来的值，直连时才回落：

```nginx
map $http_x_forwarded_proto $dauth_forwarded_proto {
    default $http_x_forwarded_proto;
    ""      $scheme;
}
```

实测：传 `https` 得到 `https`，不传得到 `http`。
（当前代码没读这个头，只影响日志与未来可能的绝对 URL 生成，属于改好不改坏。）

**② nginx 与 Go 拆成两个 systemd 服务。**
容器里 `docker/entrypoint.sh:34-40` 把两个进程塞进同一个 PID 命名空间，
`42-71` 行轮询「任一退出则整体退出」——那是容器只能有一个入口的妥协。
裸机上照搬会得到同样的耦合：nginx 挂掉会连带 Go 一起重启。

拆开后各自独立重启、日志分开（`journalctl -u d-auth` / `-u nginx`），
nginx 也变成全机共用服务。

### 4.3 迁移：从两次变一次

容器方案跑两次迁移：`entrypoint.sh:27-32` 的 `migrate up`，然后
`app.New`（`api/internal/app/app.go:35-38`）**无条件再跑一次**。
第二次是空操作（`auth_schema_migrations` 表已记录），但确实是两次。

裸机方案只依赖 `app.New` 的那一次。`bin/migrate` 仍然打进包里，需要手动执行时可用。

### 4.4 丢掉的 Docker 能力：健康检查（但不算退步）

Dockerfile 有 `HEALTHCHECK`（`Dockerfile:70-71`），裸机没有对应物。

需要说清楚的是：**原方案的健康检查本来也不具备自愈能力**。
`restart: unless-stopped` 只在进程退出时重启，容器变成 `unhealthy` 不会触发重启。
健康检查的实际作用只有两个：`docker ps` 里显示状态、`deploy-prod.sh:94-103`
部署时等待。

所以裸机不做定时健康检查，可靠性与现状持平。想更强（超过原方案）可以加
timer，配置样例在 README 的「健康检查」一节——**未包含在 `install.sh` 中**，
属于可选增强。

---

## 5. 复用到其它项目时值得抄的部分

如果以后别的项目也要这套（Go + 静态前端 + 单入口），下面这些是可以独立搬走的：
（注：本次未调研 d-specs / d-camp 的具体形态，套用前需自行确认技术栈一致。）

**可搬**：

- `build.sh` 的构建顺序：先 `npm run build:sdk` 再 `build:web`（web 依赖 sdk 的 dist），
  后端用与 Dockerfile 相同的 `CGO_ENABLED=0 GOOS/GOARCH ... -trimpath -ldflags="-s -w"`
- tar 包结构约定：`bin/` + `web/` + `migrations/`，**私钥/数据不进包**
- `install.sh` 的骨架：预检 → 建用户目录 → 搬持久状态 → 停旧方案 → 铺文件 →
  nginx `-t` 失败自动回滚 → 起 systemd → 两级探活
- unit 的沙箱选项组合（`NoNewPrivileges` + `ProtectSystem=full` +
  `ReadWritePaths` 只留 data 目录）
- 「检测型代码必须反向测」这个习惯

**必须改**：

- 端口（本项目 8680/8681，其它项目要改成自己的段）
- 服务名、用户、目录名
- 持久化状态的具体文件（本项目的 JWT 私钥是特例，别的项目可能是上传目录或别的密钥）
- `.env` 的键名与 unit 注入项的冲突清单
- `DAUTH_TRUSTED_PROXIES` 这类与本项目业务绑定的变量

**这个项目特有的、不能照抄的**：

- `jwt-private.pem` 的静默重建行为（是 `tokens/jwt.go` 的实现细节，别的项目不一定有）
- `DAUTH_HTTP_ADDR` vs `HTTP_PORT` 的优先级（本项目 config.go 的实现细节）

---

## 6. 复现验证的方法

想重新确认这套方案还能用，按这个顺序跑：

```bash
# 1. 语法
for f in deploy/baremetal/*.sh; do bash -n "$f"; done

# 2. 构建（产出 dist/d-auth-bare-<tag>.tar.gz）
make bare-build

# 3. 确认二进制是静态且架构正确
tar -xzf dist/d-auth-bare-*.tar.gz -C /tmp/verify
file /tmp/verify/bin/server        # 应含 "statically linked" 和 "x86-64"

# 4. 确认包内无私钥
tar -tzf dist/d-auth-bare-*.tar.gz | grep jwt-private  # 应无输出

# 5. nginx 配置校验（需要 docker）
docker run --rm --platform linux/amd64 \
  -v "$PWD/deploy/baremetal/nginx-d-auth.conf:/etc/nginx/conf.d/d-auth.conf:ro" \
  -v "$PWD/dist/bare/web:/opt/d-auth/web:ro" \
  nginx:1.27-alpine nginx -t

# 6. 预检（本地模拟，不改服务器）
make bare-check
```

**反向测试**（别只测 happy path）：

```bash
# 端口检测假阳性：起一个真实监听，确认能识别
# .env.prod 冲突检查：故意加 APP_ENV= 一行，确认预检拒绝
```

---

## 7. 上线前 checklist

- [ ] `make bare-check` 全部通过
- [ ] 「JWT 私钥来源」显示为 `existing` / `container` / `volume` 之一，
      **不是**「找不到任何私钥来源」
- [ ] `.env.prod` 里没有 `APP_ENV` / `DAUTH_HTTP_ADDR` / `HTTP_PORT` /
      `DAUTH_JWT_PRIVATE_KEY_PATH`（预检会自动拦）
- [ ] `DAUTH_TRUSTED_PROXIES` 含 `172.17.255.63`
- [ ] 业务机安全组没有对公网放行 8681
- [ ] 部署后 `curl 127.0.0.1:8680/health` 返回 `{"status":"ok"}`
- [ ] 从入口机 `curl https://auth.x-rv.com/health` 同样返回 ok
- [ ] 用浏览器登录一次，确认能签发 token（验证私钥真的延续了）
- [ ] `make bare-rollback` 试一次，确认能切回 Docker 再切回来
- [ ] 备份 `/opt/d-auth/data/`（私钥 + 上传文件，这是唯一需要备份的目录）