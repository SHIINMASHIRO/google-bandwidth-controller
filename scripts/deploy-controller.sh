#!/bin/bash
set -e

# Deploy Controller Script
# This script deploys the bandwidth controller on the master server

echo "=== Deploying Bandwidth Controller ==="

# Configuration
INSTALL_DIR="/opt/bandwidth-controller"
CONFIG_DIR="/etc/bandwidth-controller"
LOG_DIR="/var/log/controller"
USER="controller"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (use sudo)"
  exit 1
fi

# Create user if doesn't exist
if ! id "$USER" &>/dev/null; then
  echo "Creating user: $USER"
  useradd -r -s /bin/false $USER
fi

# Create directories
echo "Creating directories..."
mkdir -p $INSTALL_DIR/bin
mkdir -p $CONFIG_DIR
mkdir -p $LOG_DIR
chown $USER:$USER $LOG_DIR

# Build controller binary
echo "Building controller binary..."
go build -o $INSTALL_DIR/bin/controller ./cmd/controller

# Copy configuration
echo "Copying configuration..."
if [ ! -f "$CONFIG_DIR/controller.yaml" ]; then
  cp configs/controller.yaml $CONFIG_DIR/
  echo "IMPORTANT: Edit $CONFIG_DIR/controller.yaml and configure:"
  echo "  1. Change auth_token"
  echo "  2. Update agent hostnames"
  echo "  3. Add download URLs"
else
  echo "Configuration already exists, skipping..."
fi

# Set permissions
chmod +x $INSTALL_DIR/bin/controller
chown -R $USER:$USER $INSTALL_DIR

# Install systemd service
echo "Installing systemd service..."
cp deployments/systemd/controller.service /etc/systemd/system/
systemctl daemon-reload

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit configuration: $CONFIG_DIR/controller.yaml"
echo "  2. Enable service: systemctl enable bandwidth-controller"
echo "  3. Start service: systemctl start bandwidth-controller"
echo "  4. Check status: systemctl status bandwidth-controller"
echo "  5. View logs: journalctl -u bandwidth-controller -f"
echo "  6. Access metrics: curl http://localhost:9090/metrics"
echo ""
