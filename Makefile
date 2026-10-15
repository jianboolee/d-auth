.PHONY: help install api web migrate up down stop tidy \
	docker-build docker-release docker-push docker-run nginx-push \
	docker-dev docker-dev-down docker-dev-logs docker-dev-build

help:
	@echo "d-auth 常用命令"
	@echo ""
	@echo "  make up             起本地 Postgres:$(PG_PORT) + Redis:$(REDIS_PORT)（默认端口）"
	@echo "  make api            启动后端 :$(API_PORT)（air 热重载，改 .go 自动重启）"
	@echo "  make web            启动前端 :$(WEB_PORT)"
	@echo "  make migrate        单独执行数据库迁移"
	@echo "  make stop           停掉 $(APP_PORT)/$(API_PORT)/$(WEB_PORT) 上的残留进程与 dev 容器栈"
	@echo "                      不动 Postgres / Redis；要连基础设施一起停用 make down"
	@echo "  make docker-dev     本地开发栈（容器内热重载）前端 :$(WEB_PORT)"
	@echo "  make docker-build   构建发布镜像"
	@echo "  make docker-release 构建并推送 $(DOCKER_IMAGE):$(DOCKER_TAG)"
	@echo "  make docker-push    构建 amd64 并推送 $(REGISTRY):{$(RELEASE_TAG),latest}"
	@echo "  make docker-run     用发布镜像本地试跑 :$(APP_PORT)"
	@echo "  make nginx-push     推送 deploy/nginx/*.conf 到服务器并 reload nginx"
	@echo ""
	@echo "不想用热重载时直接跑：cd api && HTTP_PORT=$(API_PORT) go run ./cmd/server"
	@echo ""

# ---------- 端口约定 ----------
# 本项目独占 8680-8682，避免和 Vite 5173、常见 8080、d-specs 8400 段冲突。
#   8680  生产对外（宿主机 -> 容器 80）
#   8681  API（dev 宿主机；生产仅容器内）
#   8682  Web vite
# Postgres / Redis 不再单独占端口，直接用默认 5432 / 6379（make up 也映射到默认端口）。
# 需要临时错开时用 make ... API_PORT=... WEB_PORT=... APP_PORT=... 覆盖。
API_PORT ?= 8681
WEB_PORT ?= 8682
APP_PORT ?= 8680
PG_PORT ?= 5432
REDIS_PORT ?= 6379

DOCKER_IMAGE ?= jianboo/d-auth
DOCKER_TAG ?= latest
DOCKER_PLATFORMS ?= linux/amd64
# 生产推送目标：阿里云北京私有仓库。TAG 默认取 git describe，便于回滚到具体提交。
REGISTRY ?= registry.cn-beijing.aliyuncs.com/xrv/d-auth
RELEASE_TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo latest)
DOCKER_DEV_FILE ?= docker-compose.dev.yml
# make docker-run 用的容器环境文件，不要复用根目录 .env（那是宿主机直跑 API 的配置）
DOCKER_ENV_FILE ?= deploy/.env.docker
NGINX_SERVER ?=
NGINX_PORT ?= 22
# 与 api/Dockerfile.dev 的 AIR_VERSION 保持一致
AIR_VERSION ?= v1.66.0

# 端口预检：被占用时打印占用它的进程（PID + 命令行）并以非零退出。
# 不这么做的话，vite 只会抛一句 Port already in use，不告诉你是谁占着；
# 后端更糟——它 bind 失败的信息容易被日志淹没，看起来像启动没反应。
#
# 刻意内联而不抽成 make 变量 + $(call)：$(call) 在 recipe 位置展开出的 tab 行
# 会在解析阶段被当成新 target（实测报 "overriding commands for target"），
# 重复几行换掉这层展开陷阱，更划算。

install:
	npm install
	cd api && go mod download

tidy:
	cd api && go mod tidy

up:
	PG_PORT=$(PG_PORT) REDIS_PORT=$(REDIS_PORT) \
		docker compose -f deploy/docker-compose.infra.yml up -d

down:
	docker compose -f deploy/docker-compose.infra.yml down
	- docker compose -f $(DOCKER_DEV_FILE) down

# 清理本项目的残留进程。两条线索，都不误伤其它项目：
#   1. 端口——本项目独占 8680-8682（可用 API_PORT=... 覆盖）；
#   2. 本仓库内的 air 进程（按 cwd 判断，不受端口覆盖影响——air 是「整个后端
#      的看门狗」，单独留着一个没有意义，所以这一步始终执行）。
# 第 2 条是必须的：air 只监听文件变化，杀掉它的子进程后它自己仍活着
# （实测不会自动重启子进程），但之后任何 .go 改动都会让它重建并把端口抢回去
# ——那正是「不知道哪里偷偷又起了一个」的来源。
# 用 pgrep -x air 而不是 -f 'air -c'：后者会匹配到 make 自己的 recipe shell
# （它的 argv 里就含这串），有把自己杀掉的风险。
# 先停 dev 容器栈再杀进程：容器发布端口时 lsof 会返回 docker 的进程，
# 先 down 就不必去猜哪个 PID 是容器的。
stop:
	@echo "▶ 停止 dev 容器栈"
	-@docker compose -f $(DOCKER_DEV_FILE) down --remove-orphans 2>/dev/null
	@echo "▶ 结束占用 $(APP_PORT) / $(API_PORT) / $(WEB_PORT) 的本机进程"
	@pids=""; \
	for p in $(APP_PORT) $(API_PORT) $(WEB_PORT); do \
		pids="$$pids $$(lsof -ti :$$p 2>/dev/null)"; \
	done; \
	pids=$$(echo $$pids | tr ' ' '\n' | grep -v '^$$' | sort -u | tr '\n' ' '); \
	if [ -z "$$pids" ]; then \
		echo "  没有进程占用这些端口"; \
	else \
		for pid in $$pids; do \
			echo "  kill $$pid  $$(ps -o command= -p $$pid 2>/dev/null | cut -c1-110)"; \
		done; \
		kill $$pids 2>/dev/null; \
	fi
	@echo "▶ 结束本仓库的 air（否则下次改 .go 会被它重新拉起）"
	@root=$$(pwd); \
	airs=""; \
	for pid in $$(pgrep -x air 2>/dev/null); do \
		cwd=$$(lsof -a -d cwd -p $$pid -Fn 2>/dev/null | grep '^n' | cut -c2-); \
		case "$$cwd" in \
			"$$root"|"$$root"/*) airs="$$airs $$pid"; echo "  kill $$pid  air (cwd=$$cwd)";; \
		esac; \
	done; \
	if [ -n "$$airs" ]; then kill $$airs 2>/dev/null; fi
	@sleep 1; \
	left=$$(for p in $(APP_PORT) $(API_PORT) $(WEB_PORT); do lsof -ti :$$p 2>/dev/null; done | sort -u); \
	if [ -n "$$left" ]; then \
		echo "▶ 以下 PID 未退出，强制结束：$$left"; \
		kill -9 $$left 2>/dev/null; \
	fi
	@echo "▶ 完成（Postgres / Redis 未受影响，要一起停用 make down）"

migrate:
	cd api && go run ./cmd/migrate

api:
	@command -v air >/dev/null 2>&1 || { \
		echo "✖ 未找到 air（make api 用它做热重载）。安装："; \
		echo "    go install github.com/air-verse/air@$(AIR_VERSION)"; \
		exit 1; \
	}
	@if pids=$$(lsof -ti :$(API_PORT) 2>/dev/null); then \
		echo "✖ 端口 $(API_PORT) 已被占用，后端起不来："; \
		lsof -nP -iTCP:$(API_PORT) -sTCP:LISTEN 2>/dev/null | tail -n +2 | sed 's/^/    /'; \
		echo "  → 执行 make stop 清理，或 kill $$(echo $$pids | tr '\n' ' ')"; \
		exit 1; \
	fi
	cd api && HTTP_PORT=$(API_PORT) air -c .air.toml

web:
	@if pids=$$(lsof -ti :$(WEB_PORT) 2>/dev/null); then \
		echo "✖ 端口 $(WEB_PORT) 已被占用，前端起不来："; \
		lsof -nP -iTCP:$(WEB_PORT) -sTCP:LISTEN 2>/dev/null | tail -n +2 | sed 's/^/    /'; \
		echo "  → 执行 make stop 清理，或 kill $$(echo $$pids | tr '\n' ' ')"; \
		exit 1; \
	fi
	cd web && WEB_PORT=$(WEB_PORT) npm run dev

docker-build:
	@echo "构建 $(DOCKER_IMAGE):$(DOCKER_TAG) ($(DOCKER_PLATFORMS))"
	docker build --platform $(DOCKER_PLATFORMS) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-release:
	@echo "发布 $(DOCKER_IMAGE):$(DOCKER_TAG) ($(DOCKER_PLATFORMS))"
	docker build --platform $(DOCKER_PLATFORMS) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

# 构建并推送到阿里云私有仓库，同时打 git 版本标签与 latest。
# 构建阶段在 BUILDPLATFORM 原生跑，Go 交叉编译到 TARGETARCH（见 Dockerfile）。
# 需先 docker login $(REGISTRY 的域名部分)。覆盖：make docker-push REGISTRY=... RELEASE_TAG=v1.2.3
docker-push:
	@echo "推送 $(REGISTRY):$(RELEASE_TAG) 与 :latest ($(DOCKER_PLATFORMS))"
	docker buildx build --platform $(DOCKER_PLATFORMS) \
		-t $(REGISTRY):$(RELEASE_TAG) \
		-t $(REGISTRY):latest \
		--push .

# 本地试跑发布镜像。镜像已内置 APP_ENV=production / HTTP_PORT=8681 / RUN_MIGRATIONS=true。
# 端口被占用时覆盖：make docker-run APP_PORT=8690
docker-run:
	@if [ -f "$(DOCKER_ENV_FILE)" ]; then \
		echo "▶ 使用容器环境文件 $(DOCKER_ENV_FILE)"; \
		docker run --rm -p $(APP_PORT):80 --env-file "$(DOCKER_ENV_FILE)" $(DOCKER_IMAGE):$(DOCKER_TAG); \
	else \
		echo "▶ 未找到 $(DOCKER_ENV_FILE)，见 deploy/.env.docker.example"; \
		docker run --rm -p $(APP_PORT):80 $(DOCKER_IMAGE):$(DOCKER_TAG); \
	fi

docker-dev: up
	API_PORT=$(API_PORT) WEB_PORT=$(WEB_PORT) \
		docker compose -f $(DOCKER_DEV_FILE) up

docker-dev-down:
	API_PORT=$(API_PORT) WEB_PORT=$(WEB_PORT) \
		docker compose -f $(DOCKER_DEV_FILE) down

docker-dev-logs:
	docker compose -f $(DOCKER_DEV_FILE) logs -f

docker-dev-build:
	docker compose -f $(DOCKER_DEV_FILE) build --no-cache api

nginx-push:
	@SERVER=$(NGINX_SERVER) PORT=$(NGINX_PORT) deploy/nginx/upload.sh
