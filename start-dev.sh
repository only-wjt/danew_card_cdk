#!/bin/bash

echo "================================================"
echo "CDK 充值系统 - 本地开发启动"
echo "================================================"
echo ""

PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

# 检查环境变量文件（后端不会自己加载 .env，必须由本脚本 export）
if [ ! -f .env.local ]; then
    echo "✗ 未找到 .env.local 文件"
    exit 1
fi

echo "✓ 环境文件已找到"
echo ""

# 载入环境变量（后端只读进程环境变量）
set -a
# shellcheck disable=SC1091
. ./.env.local
set +a

# 准备 SQLite 数据库文件目录（SQLite 会自动建文件，但不会自动建目录）
# DB_PATH 默认值 ../data/cdk_recharge.db 是相对 backend 目录的，即项目根的 data/
DB_FILE="${DB_PATH:-./data/cdk_recharge.db}"
case "$DB_FILE" in
    ../*) DB_FILE="$PROJECT_ROOT/${DB_FILE#../}" ;;
esac
DB_DIR="$(dirname "$DB_FILE")"
echo "检查数据库文件..."
mkdir -p "$DB_DIR" || { echo "✗ 无法创建数据库目录: $DB_DIR"; exit 1; }
if [ -f "$DB_FILE" ]; then
    echo "✓ 使用已有 SQLite 数据库: $DB_FILE"
else
    echo "✓ 数据库不存在，后端启动时会自动创建: $DB_FILE"
fi

echo ""
echo "启动服务..."
echo ""

# 启动后端（go-sqlite3 依赖 cgo，必须开启）
cd "$PROJECT_ROOT/backend"
export CGO_ENABLED=1
echo "启动后端服务 (端口 ${SERVER_PORT:-8080})..."
go run ./cmd/server/main.go &
BACKEND_PID=$!
echo "  后端 PID: $BACKEND_PID"

sleep 2

# 启动前端
cd ../frontend
echo "启动前端服务 (端口 5173)..."
npm run dev &
FRONTEND_PID=$!
echo "  前端 PID: $FRONTEND_PID"

echo ""
echo "================================================"
echo "✓ 服务已启动！"
echo "================================================"
echo ""
echo "前端:    http://localhost:5173"
echo "后端:    http://localhost:8080"
echo "API:     http://localhost:8080/api/v1"
echo "健康检查: curl http://localhost:8080/health"
echo ""
echo "按 Ctrl+C 停止所有服务..."
echo ""

# 等待中断信号
trap "kill $BACKEND_PID $FRONTEND_PID; exit" SIGINT SIGTERM

# 保持脚本运行
wait
