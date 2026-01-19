# 一键安装指南

本文档说明如何使用一键脚本快速部署 Google Bandwidth Controller。

## 🚀 快速开始

### 方式 1: 一键安装 (推荐)

#### 安装 Controller (主控服务器)

```bash
curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-controller.sh | sudo bash
```

安装完成后:

```bash
# 1. 生成强密码令牌
openssl rand -base64 32

# 2. 编辑配置 (必须!)
sudo nano /etc/bandwidth-controller/controller.yaml

# 修改:
# - server.auth_token (改成上面生成的密码)
# - agents[] (配置你的 15 台 VPS)
# - download_urls[] (添加 Google 服务 URL)

# 3. 启动服务
sudo systemctl enable bandwidth-controller
sudo systemctl start bandwidth-controller

# 4. 查看实时控制面板
sudo journalctl -u bandwidth-controller -f
```

#### 安装 Agent (每台 VPS)

在每台 VPS 上运行:

```bash
curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-agent.sh | \
  sudo bash -s -- agent-001 "VPS-Tokyo-1" controller.example.com YOUR_AUTH_TOKEN
```

参数说明:
- `agent-001` - Agent ID (必须与 controller.yaml 中一致)
- `"VPS-Tokyo-1"` - Agent 名称
- `controller.example.com` - Controller 服务器地址
- `YOUR_AUTH_TOKEN` - 认证令牌 (与 controller.yaml 中一致)

对 15 台 VPS 分别执行,修改 ID 和名称:
```bash
# VPS 1
curl -fsSL ... | sudo bash -s -- agent-001 "VPS-Tokyo-1" controller.example.com TOKEN

# VPS 2
curl -fsSL ... | sudo bash -s -- agent-002 "VPS-Tokyo-2" controller.example.com TOKEN

# ... 以此类推到 agent-015
```

### 方式 2: 批量部署 Agents

如果有多台 VPS 需要部署,可以使用批量脚本:

```bash
# 1. 下载批量部署脚本
wget https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/batch-deploy-agents.sh

# 2. 编辑脚本,配置:
nano batch-deploy-agents.sh

# 修改:
# - CONTROLLER_HOST (你的 controller 地址)
# - AUTH_TOKEN (认证令牌)
# - AGENTS 数组 (15 台 VPS 的信息)

# 3. 赋予执行权限
chmod +x batch-deploy-agents.sh

# 4. 执行批量部署
./batch-deploy-agents.sh
```

批量脚本会自动:
- ✅ SSH 连接到所有 VPS
- ✅ 下载并安装 agent
- ✅ 自动配置和启动服务
- ✅ 显示部署结果

### 方式 3: 手动下载安装

```bash
# 1. 下载最新版本
wget https://github.com/SHIINMASHIRO/google-bandwidth-controller/releases/download/v1.0.0/bandwidth-controller-linux-amd64.tar.gz

# 2. 解压
tar xzf bandwidth-controller-linux-amd64.tar.gz
cd bandwidth-controller-linux-amd64

# 3. 使用包含的部署脚本
sudo ./scripts/deploy-controller.sh  # Controller
sudo ./scripts/deploy-agent.sh agent-001 "VPS-Tokyo-1" controller.example.com TOKEN  # Agent
```

## 📋 完整部署流程

### 步骤 1: 部署 Controller

在主控服务器上:

```bash
# 一键安装
curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-controller.sh | sudo bash

# 生成强密码
TOKEN=$(openssl rand -base64 32)
echo "认证令牌: $TOKEN"
# 保存这个 TOKEN,稍后配置 agents 时需要

# 编辑配置
sudo nano /etc/bandwidth-controller/controller.yaml
```

配置文件示例:

```yaml
server:
  auth_token: "上面生成的TOKEN"  # 重要!

agents:
  - id: "agent-001"
    host: "vps1.你的域名.com"
    name: "VPS-Tokyo-1"
    max_bandwidth: 1500
    region: "tokyo"

  - id: "agent-002"
    host: "vps2.你的域名.com"
    name: "VPS-Tokyo-2"
    max_bandwidth: 1500
    region: "tokyo"

  # ... 配置所有 15 台

download_urls:
  - "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"
  - "https://dl.google.com/go/go1.21.6.linux-amd64.tar.gz"
  - "https://dl.google.com/android/repository/platform-tools-latest-linux.zip"
  # 添加更多 Google URL
```

启动 Controller:

```bash
sudo systemctl enable bandwidth-controller
sudo systemctl start bandwidth-controller

# 查看状态
sudo systemctl status bandwidth-controller

# 查看实时日志
sudo journalctl -u bandwidth-controller -f
```

### 步骤 2: 部署 15 个 Agents

**方式 A: 一台一台安装**

在每台 VPS 上执行:

```bash
# VPS 1
curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-agent.sh | \
  sudo bash -s -- agent-001 "VPS-Tokyo-1" controller.你的域名.com 你的TOKEN

# VPS 2
curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-agent.sh | \
  sudo bash -s -- agent-002 "VPS-Tokyo-2" controller.你的域名.com 你的TOKEN

# ... 依此类推
```

**方式 B: 批量安装 (推荐)**

在本地机器上:

```bash
# 1. 下载批量脚本
wget https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/batch-deploy-agents.sh

# 2. 编辑配置
nano batch-deploy-agents.sh

# 修改这些变量:
CONTROLLER_HOST="controller.你的域名.com"
AUTH_TOKEN="你的TOKEN"

AGENTS=(
    "agent-001:VPS-Tokyo-1:vps1.你的域名.com"
    "agent-002:VPS-Tokyo-2:vps2.你的域名.com"
    # ... 所有 15 台
)

# 3. 执行
chmod +x batch-deploy-agents.sh
./batch-deploy-agents.sh
```

### 步骤 3: 验证部署

```bash
# 检查所有 agents 是否连接
curl http://controller.你的域名.com:9090/agents | jq

# 期望输出: 所有 15 个 agents 显示 "connected": true

# 检查总带宽
curl http://controller.你的域名.com:9090/metrics | jq

# 期望输出: total_bandwidth_gbps 接近 10.0
```

## 🔍 故障排查

### Controller 无法启动

```bash
# 查看日志
sudo journalctl -u bandwidth-controller -n 50

# 常见问题:
# 1. 端口被占用 - 检查 8080 和 9090 端口
# 2. 配置错误 - 检查 /etc/bandwidth-controller/controller.yaml
# 3. 权限问题 - 检查 /var/log/controller 权限
```

### Agent 无法连接

```bash
# 在 Agent VPS 上检查
sudo systemctl status bandwidth-agent
sudo journalctl -u bandwidth-agent -n 50

# 测试网络连接
telnet controller.你的域名.com 8080

# 检查认证令牌是否正确
sudo cat /etc/bandwidth-agent/agent.yaml | grep auth_token
```

### 带宽过低

```bash
# 检查有多少 agents 连接
curl http://controller.你的域名.com:9090/agents | jq '.connected'

# 检查单个 agent 日志
ssh root@vps1.example.com 'journalctl -u bandwidth-agent -f'

# 检查 VPS 网络质量
ssh root@vps1.example.com 'wget -O /dev/null https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb'
```

## 🔄 更新版本

当有新版本发布时:

```bash
# 停止服务
sudo systemctl stop bandwidth-controller

# 重新运行安装脚本 (会自动下载最新版本)
curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-controller.sh | sudo bash

# 启动服务
sudo systemctl start bandwidth-controller
```

对 agents 执行相同操作。

## 🗑️ 卸载

### 卸载 Controller

```bash
sudo systemctl stop bandwidth-controller
sudo systemctl disable bandwidth-controller
sudo rm /etc/systemd/system/bandwidth-controller.service
sudo rm -rf /opt/bandwidth-controller
sudo rm -rf /etc/bandwidth-controller
sudo userdel controller
sudo systemctl daemon-reload
```

### 卸载 Agent

```bash
sudo systemctl stop bandwidth-agent
sudo systemctl disable bandwidth-agent
sudo rm /etc/systemd/system/bandwidth-agent.service
sudo rm -rf /opt/bandwidth-agent
sudo rm -rf /etc/bandwidth-agent
sudo systemctl daemon-reload
```

## 📊 监控命令

```bash
# 查看实时控制面板
sudo journalctl -u bandwidth-controller -f

# 查看当前指标
curl http://localhost:9090/metrics | jq

# 查看系统状态
curl http://localhost:9090/status | jq

# 查看所有 agents
curl http://localhost:9090/agents | jq

# 查看 24 小时统计
curl "http://localhost:9090/stats?duration=24h" | jq
```

## 💡 最佳实践

1. **使用强密码**:
   ```bash
   openssl rand -base64 32
   ```

2. **配置防火墙**:
   ```bash
   # Controller 服务器
   sudo ufw allow 8080/tcp  # WebSocket
   sudo ufw allow 9090/tcp  # HTTP API
   ```

3. **定期检查日志**:
   ```bash
   sudo journalctl -u bandwidth-controller --since "1 hour ago"
   ```

4. **备份配置**:
   ```bash
   sudo cp /etc/bandwidth-controller/controller.yaml ~/controller.yaml.backup
   ```

5. **测试单个 VPS**:
   先在 1-2 台 VPS 上测试成功后再全量部署

## 🎯 预期结果

部署成功后:
- ✅ 15 台 VPS agents 全部连接
- ✅ 总带宽达到 ~10Gbps
- ✅ 流量模式自然、非线性
- ✅ 实时监控面板正常显示
- ✅ 满足 Google PNI 申请要求

## 📞 获取帮助

- GitHub Issues: https://github.com/SHIINMASHIRO/google-bandwidth-controller/issues
- 文档: README.md, DEPLOYMENT.md
- 检查日志: `journalctl -u bandwidth-controller -f`
