.PHONY: help up down build logs ps dev-gateway dev-web seed test loadtest clean

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

up: ## 一键构建并启动所有服务
	docker compose up --build -d

down: ## 停止并清理
	docker compose down

build: ## 构建镜像
	docker compose build

logs: ## 跟随网关日志
	docker compose logs -f gateway

ps: ## 查看服务状态
	docker compose ps

seed: ## 重建种子数据（注册 mock 服务 + 工具 + 管理员）
	docker compose exec gateway sh -c './bin/ctl seed'

dev-gateway: ## 本地运行网关（需本地 Go + 依赖服务）
	cd gateway && go run ./cmd/server

dev-web: ## 本地运行前端
	cd web && npm install && npm run dev

test: ## 运行 Go 测试
	cd gateway && go test ./...

loadtest: ## 网关压测（默认 50 RPS × 5s）
	python tools/loadtest.py --rps 50 --seconds 5

clean: ## 清理构建产物
	rm -rf gateway/bin web/dist
