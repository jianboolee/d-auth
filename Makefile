.PHONY: help install api web migrate up down tidy \
	docker-build docker-release docker-run nginx-push \
	docker-dev docker-dev-down docker-dev-logs docker-dev-build

help:
	@echo "d-auth 常用命令"
	@echo ""
	@echo "  make up             起本地 Postgres:$(PG_PORT) + Redis:$(REDIS_PORT)"
	@echo "  make api            启动后端 :$(API_PORT)"
	@echo "  make web            启动前端 :$(WEB_PORT)"
	@echo "  make migrate        执行数据库迁移"
	@echo "  make docker-dev     本地开发栈（热重载）前端 :$(WEB_PORT)"
	@echo "  make docker-build   构建发布镜像"
	@echo "  make docker-release 构建并推送 $(DOCKER_IMAGE):$(DOCKER_TAG)"
	@echo "  make docker-run     用发布镜像本地试跑 :$(APP_PORT)"
	@echo "  make nginx-push     推送 deploy/nginx/*.conf 到服务器并 reload nginx"
	@echo ""

# ---------- 端口约定 ----------
# 本项目独占 8680-8684，避免和 Vite 5173、常见 8080、d-specs 8400 段冲突。
#   8680  生产对外（宿主机 -> 容器 80）
#   8681  API（dev 宿主机；生产仅容器内）
#   8682  Web vite
#   8683  本地 Postgres（make up）
#   8684  本地 Redis（make up）
# 需要临时错开时用 make ... API_PORT=... WEB_PORT=... APP_PORT=... 覆盖。
API_PORT ?= 8681
WEB_PORT ?= 8682
APP_PORT ?= 8680
PG_PORT ?= 8683
REDIS_PORT ?= 8684

DOCKER_IMAGE ?= jianboo/d-auth
DOCKER_TAG ?= latest
DOCKER_PLATFORMS ?= linux/amd64
DOCKER_DEV_FILE ?= docker-compose.dev.yml
# make docker-run 用的容器环境文件，不要复用根目录 .env（那是宿主机直跑 API 的配置）
DOCKER_ENV_FILE ?= deploy/.env.docker
NGINX_SERVER ?=
NGINX_PORT ?= 22

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

migrate:
	cd api && go run ./cmd/migrate

api:
	cd api && HTTP_PORT=$(API_PORT) go run ./cmd/server

web:
	cd web && WEB_PORT=$(WEB_PORT) npm run dev

docker-build:
	@echo "构建 $(DOCKER_IMAGE):$(DOCKER_TAG) ($(DOCKER_PLATFORMS))"
	docker build --platform $(DOCKER_PLATFORMS) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-release:
	@echo "发布 $(DOCKER_IMAGE):$(DOCKER_TAG) ($(DOCKER_PLATFORMS))"
	docker build --platform $(DOCKER_PLATFORMS) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

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
