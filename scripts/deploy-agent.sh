#!/bin/bash
set -e

# Deploy Agent Script
# Usage: ./deploy-agent.sh <agent-id> <agent-name> <controller-host> <auth-token>
#
# Example: ./deploy-agent.sh agent-001 "VPS-Tokyo-1" controller.example.com mySecretToken

if [ $# -lt 4 ]; then
  echo "Usage: $0 <agent-id> <agent-name> <controller-host> <auth-token>"
  echo "Example: $0 agent-001 VPS-Tokyo-1 controller.example.com mySecretToken"
  exit 1
fi

AGENT_ID="$1"
AGENT_NAME="$2"
CONTROLLER_HOST="$3"
AUTH_TOKEN="$4"

echo "=== Deploying Bandwidth Agent ==="
echo "Agent ID: $AGENT_ID"
echo "Agent Name: $AGENT_NAME"
echo "Controller: $CONTROLLER_HOST"
echo ""

# Configuration
INSTALL_DIR="/opt/bandwidth-agent"
CONFIG_DIR="/etc/bandwidth-agent"
LOG_DIR="/var/log/agent"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (use sudo)"
  exit 1
fi

# Check if wget is installed
if ! command -v wget &> /dev/null; then
  echo "Error: wget is not installed. Please install it first:"
  echo "  Ubuntu/Debian: apt-get install wget"
  echo "  CentOS/RHEL: yum install wget"
  exit 1
fi

# Create directories
echo "Creating directories..."
mkdir -p $INSTALL_DIR/bin
mkdir -p $CONFIG_DIR
mkdir -p $LOG_DIR
mkdir -p /tmp/bandwidth-test

# Build agent binary (if building locally)
if [ -f "go.mod" ]; then
  echo "Building agent binary..."
  go build -o $INSTALL_DIR/bin/agent ./cmd/agent
else
  echo "Copying pre-built agent binary..."
  # Assuming binary is provided
  if [ ! -f "agent" ]; then
    echo "Error: agent binary not found"
    exit 1
  fi
  cp agent $INSTALL_DIR/bin/
fi

# Create configuration file
echo "Creating configuration..."
cat > $CONFIG_DIR/agent.yaml <<EOF
agent:
  id: "${AGENT_ID}"
  name: "${AGENT_NAME}"

controller:
  host: "${CONTROLLER_HOST}"
  port: 8080
  auth_token: "${AUTH_TOKEN}"
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

# Set permissions
chmod +x $INSTALL_DIR/bin/agent
chmod 600 $CONFIG_DIR/agent.yaml

# Install systemd service
echo "Installing systemd service..."
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

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Next steps:"
echo "  1. Enable service: systemctl enable bandwidth-agent"
echo "  2. Start service: systemctl start bandwidth-agent"
echo "  3. Check status: systemctl status bandwidth-agent"
echo "  4. View logs: journalctl -u bandwidth-agent -f"
echo ""
echo "Agent configured:"
echo "  ID: $AGENT_ID"
echo "  Name: $AGENT_NAME"
echo "  Controller: $CONTROLLER_HOST:8080"
echo ""
