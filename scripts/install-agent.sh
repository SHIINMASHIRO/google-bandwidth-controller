#!/bin/bash
set -e

# Google Bandwidth Agent - 一键部署脚本
# 使用方法: curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-agent.sh | sudo bash -s -- agent-001 "VPS-Tokyo-1" controller.example.com YOUR_TOKEN

echo "════════════════════════════════════════════════════════════════"
echo "     Google Bandwidth Agent - 一键部署"
echo "════════════════════════════════════════════════════════════════"
echo ""

# 检查参数
if [ $# -lt 4 ]; then
    echo "❌ 缺少参数!"
    echo ""
    echo "使用方法:"
    echo "  $0 <agent-id> <agent-name> <controller-host> <auth-token>"
    echo ""
    echo "示例:"
    echo "  $0 agent-001 'VPS-Tokyo-1' controller.example.com mySecretToken"
    echo ""
    echo "或通过 curl 一键安装:"
    echo "  curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-agent.sh | \\"
    echo "    sudo bash -s -- agent-001 'VPS-Tokyo-1' controller.example.com mySecretToken"
    echo ""
    exit 1
fi

# 检查是否为 root
if [ "$EUID" -ne 0 ]; then
  echo "❌ 请使用 sudo 运行此脚本"
  exit 1
fi

# 获取参数
AGENT_ID="$1"
AGENT_NAME="$2"
CONTROLLER_HOST="$3"
AUTH_TOKEN="$4"

echo "Agent ID:   $AGENT_ID"
echo "Agent 名称: $AGENT_NAME"
echo "Controller: $CONTROLLER_HOST"
echo ""

# 配置
GITHUB_REPO="SHIINMASHIRO/google-bandwidth-controller"
VERSION="v1.0.0"
INSTALL_DIR="/opt/bandwidth-agent"
CONFIG_DIR="/etc/bandwidth-agent"
LOG_DIR="/var/log/agent"

# 检查 wget
if ! command -v wget &> /dev/null; then
    echo "❌ 需要 wget，正在安装..."
    if command -v apt-get &> /dev/null; then
        apt-get update && apt-get install -y wget
    elif command -v yum &> /dev/null; then
        yum install -y wget
    else
        echo "❌ 无法自动安装 wget，请手动安装"
        exit 1
    fi
fi

echo "✓ wget 已安装"

# 检测系统架构
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        GOARCH="amd64"
        ;;
    aarch64|arm64)
        GOARCH="arm64"
        ;;
    *)
        echo "❌ 不支持的架构: $ARCH"
        exit 1
        ;;
esac

echo "✓ 检测到架构: $ARCH ($GOARCH)"

# 下载最新 release
DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/bandwidth-controller-linux-$GOARCH.tar.gz"
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

echo "📥 下载安装包..."
echo "   从: $DOWNLOAD_URL"

if command -v wget &> /dev/null; then
    wget -q --show-progress "$DOWNLOAD_URL" -O package.tar.gz
elif command -v curl &> /dev/null; then
    curl -L --progress-bar "$DOWNLOAD_URL" -o package.tar.gz
else
    echo "❌ 需要 wget 或 curl"
    exit 1
fi

if [ ! -f package.tar.gz ]; then
    echo "❌ 下载失败"
    exit 1
fi

echo "✓ 下载完成"

# 解压
echo "📦 解压安装包..."
tar xzf package.tar.gz

# 创建目录
echo "📁 创建目录..."
mkdir -p $INSTALL_DIR/bin
mkdir -p $CONFIG_DIR
mkdir -p $LOG_DIR
mkdir -p /tmp/bandwidth-test

# 安装二进制文件
echo "📋 安装程序..."
cp agent-linux-$GOARCH $INSTALL_DIR/bin/agent
chmod +x $INSTALL_DIR/bin/agent

# 创建配置文件
echo "⚙️  生成配置文件..."
cat > $CONFIG_DIR/agent.yaml <<EOF
# Agent 配置 - 由安装脚本自动生成
agent:
  id: "$AGENT_ID"
  name: "$AGENT_NAME"

controller:
  host: "$CONTROLLER_HOST"
  port: 8080
  auth_token: "$AUTH_TOKEN"
  reconnect_interval: 5s
  max_reconnect_attempts: 0

download:
  tool: "wget"
  output_dir: "/tmp/bandwidth-test"
  cleanup: true
  timeout: 300s

metrics:
  report_interval: 5s
  bandwidth_sample_rate: 1s

logging:
  level: "info"
  format: "json"
  output: "$LOG_DIR/agent.log"
EOF

chmod 600 $CONFIG_DIR/agent.yaml

# 复制文档
cp README.md $INSTALL_DIR/ 2>/dev/null || true
cp DEPLOYMENT.md $INSTALL_DIR/ 2>/dev/null || true

# 安装 systemd 服务
echo "🔧 安装 systemd 服务..."
cat > /etc/systemd/system/bandwidth-agent.service <<EOF
[Unit]
Description=Google Bandwidth Agent Service ($AGENT_NAME)
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/bin/agent -config $CONFIG_DIR/agent.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Security settings
NoNewPrivileges=true
PrivateTmp=true

# Resource limits
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload

# 清理
cd /
rm -rf "$TEMP_DIR"

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "✅ Agent 安装完成!"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "Agent 信息:"
echo "  ID:         $AGENT_ID"
echo "  名称:       $AGENT_NAME"
echo "  Controller: $CONTROLLER_HOST:8080"
echo ""
echo "📝 下一步:"
echo ""
echo "1. 启用服务:"
echo "   systemctl enable bandwidth-agent"
echo ""
echo "2. 启动服务:"
echo "   systemctl start bandwidth-agent"
echo ""
echo "3. 查看状态:"
echo "   systemctl status bandwidth-agent"
echo ""
echo "4. 查看实时日志:"
echo "   journalctl -u bandwidth-agent -f"
echo ""
echo "5. 测试连接:"
echo "   telnet $CONTROLLER_HOST 8080"
echo ""
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "📚 文件位置:"
echo "   安装目录: $INSTALL_DIR"
echo "   配置文件: $CONFIG_DIR/agent.yaml"
echo "   日志目录: $LOG_DIR"
echo ""
echo "🔄 重启服务:"
echo "   systemctl restart bandwidth-agent"
echo ""
echo "🛑 停止服务:"
echo "   systemctl stop bandwidth-agent"
echo ""
echo "════════════════════════════════════════════════════════════════"
