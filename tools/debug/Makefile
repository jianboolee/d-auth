# tools/debug 的独立 Makefile。
#
# 刻意不挂到仓库根的 Makefile：本目录是调试工具，不参与项目构建与部署，
# 需要用时手动进来跑。详细说明见 README.md。

BIN_DIR := bin
BATCH   ?= 1
SPREAD  ?= 10m

# 与项目一致：纯 Go 依赖，不引入 cgo
export CGO_ENABLED := 0

.PHONY: help build seed seed-dry admin admin-dry clean

help:
	@echo "tools/debug —— 本地开发库的调试工具"
	@echo ""
	@echo "  make build              构建两个工具到 $(BIN_DIR)/"
	@echo "  make admin              创建/修复测试管理员（幂等）"
	@echo "  make admin-dry          只看会创建/修复什么，不写库"
	@echo "  make seed               插入第 $(BATCH) 批测试用户"
	@echo "  make seed BATCH=2       指定批次"
	@echo "  make seed-dry           只看会创建谁、跳过谁"
	@echo "  make clean              删除构建产物"
	@echo ""
	@echo "连接优先级：-dsn > DAUTH_POSTGRES_DSN > 仓库根 .env；默认只允许本机库。"

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/make-admin ./make-admin
	go build -o $(BIN_DIR)/seed-users ./seed-users
	@echo "已构建 $(BIN_DIR)/make-admin $(BIN_DIR)/seed-users"

# dry-run 直接用 go run，改完代码立刻能看到结果
admin-dry:
	go run ./make-admin -dry-run

admin: build
	./$(BIN_DIR)/make-admin

seed-dry:
	go run ./seed-users -batch $(BATCH) -spread $(SPREAD) -dry-run

seed: build
	./$(BIN_DIR)/seed-users -batch $(BATCH) -spread $(SPREAD)

clean:
	rm -rf $(BIN_DIR)