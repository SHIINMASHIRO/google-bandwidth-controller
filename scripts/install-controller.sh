#!/bin/bash
set -e

# Google Bandwidth Controller - 一键部署脚本 (Controller)
# 使用方法: curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-controller.sh | sudo bash

echo "════════════════════════════════════════════════════════════════"
echo "     Google Bandwidth Controller - 一键部署 (Controller)"
echo "════════════════════════════════════════════════════════════════"
echo ""

# 检查是否为 root
if [ "$EUID" -ne 0 ]; then
  echo "❌ 请使用 sudo 运行此脚本"
  exit 1
fi

# 配置
GITHUB_REPO="SHIINMASHIRO/google-bandwidth-controller"
VERSION="v1.0.1"
INSTALL_DIR="/opt/bandwidth-controller"
CONFIG_DIR="/etc/bandwidth-controller"
LOG_DIR="/var/log/controller"
USER="controller"

echo "📦 开始部署..."
echo "版本: $VERSION"
echo ""

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

# 创建用户
if ! id "$USER" &>/dev/null; then
    echo "👤 创建系统用户: $USER"
    useradd -r -s /bin/false $USER
fi

# 创建目录
echo "📁 创建目录..."
mkdir -p $INSTALL_DIR/bin
mkdir -p $CONFIG_DIR
mkdir -p $LOG_DIR
chown $USER:$USER $LOG_DIR

# 安装二进制文件
echo "📋 安装程序..."
cp controller-linux-$GOARCH $INSTALL_DIR/bin/controller
chmod +x $INSTALL_DIR/bin/controller
chown -R $USER:$USER $INSTALL_DIR

# 安装配置文件
if [ ! -f "$CONFIG_DIR/controller.yaml" ]; then
    echo "⚙️  复制配置模板..."
    cp configs/controller.yaml $CONFIG_DIR/
    echo ""
    echo "⚠️  重要: 请编辑配置文件!"
    echo "   配置文件位置: $CONFIG_DIR/controller.yaml"
    echo ""
    echo "   必须修改的项:"
    echo "   1. server.auth_token (改成强密码!)"
    echo "   2. agents[] (配置你的 15 台 VPS)"
    echo "   3. download_urls[] (添加 Google 服务 URL)"
    echo ""
else
    echo "✓ 配置文件已存在，跳过"
fi

# 复制部署脚本和文档
cp -r scripts $INSTALL_DIR/
cp -r deployments $INSTALL_DIR/
cp README.md $INSTALL_DIR/ 2>/dev/null || true
cp DEPLOYMENT.md $INSTALL_DIR/ 2>/dev/null || true

# 安装 systemd 服务
echo "🔧 安装 systemd 服务..."
cat > /etc/systemd/system/bandwidth-controller.service <<EOF
[Unit]
Description=Google Bandwidth Controller Service
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=$USER
Group=$USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/bin/controller -config $CONFIG_DIR/controller.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$LOG_DIR /tmp

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
echo "✅ Controller 安装完成!"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "📝 下一步:"
echo ""
echo "1. 编辑配置文件:"
echo "   nano $CONFIG_DIR/controller.yaml"
echo ""
echo "   必须修改:"
echo "   - server.auth_token (使用强密码!)"
echo "   - agents[] (配置 15 台 VPS)"
echo "   - download_urls[] (添加 Google URL)"
echo ""
echo "2. 启用服务:"
echo "   systemctl enable bandwidth-controller"
echo ""
echo "3. 启动服务:"
echo "   systemctl start bandwidth-controller"
echo ""
echo "4. 查看状态:"
echo "   systemctl status bandwidth-controller"
echo ""
echo "5. 查看实时日志:"
echo "   journalctl -u bandwidth-controller -f"
echo ""
echo "6. 查看监控指标:"
echo "   curl http://localhost:9090/metrics | jq"
echo ""
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "📚 文档位置:"
echo "   安装目录: $INSTALL_DIR"
echo "   配置目录: $CONFIG_DIR"
echo "   日志目录: $LOG_DIR"
echo ""
echo "💡 生成强密码令牌:"
echo "   openssl rand -base64 32"
echo ""
echo "════════════════════════════════════════════════════════════════"
