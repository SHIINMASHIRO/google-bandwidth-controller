# 部署指南 (Deployment Guide)

本指南说明如何从 GitHub 部署 Google Bandwidth Controller 系统到你的服务器上。

## 前置要求

### Controller 服务器
- Linux 操作系统 (Ubuntu/Debian/CentOS)
- 至少 1GB RAM
- 对外开放端口 8080 (WebSocket) 和 9090 (HTTP API)

### Agent 服务器 (15台 VPS)
- Linux 操作系统
- 已安装 `wget`
- 能够访问 Google 服务
- 能够连接到 Controller 服务器的 8080 端口

## 步骤 1: 下载构建好的二进制文件

### 从 GitHub Releases 下载

访问 GitHub Releases 页面,下载最新版本的构建包:

```bash
# 下载 Linux AMD64 版本
wget https://github.com/YOUR_USERNAME/google-bandwidth-controller/releases/latest/download/bandwidth-controller-linux-amd64.tar.gz

# 解压
tar xzf bandwidth-controller-linux-amd64.tar.gz
cd bandwidth-controller-linux-amd64
```

### 或从 GitHub Actions 下载

1. 访问 GitHub 仓库的 Actions 标签页
2. 选择最新成功的 workflow run
3. 下载 `bandwidth-controller-linux-amd64` artifact
4. 解压并进入目录

## 步骤 2: 部署 Controller (主控服务器)

在你的主控服务器上执行:

```bash
# 1. 赋予脚本执行权限
chmod +x scripts/deploy-controller.sh
chmod +x controller-linux-amd64

# 2. 将二进制文件重命名
mv controller-linux-amd64 controller

# 3. 运行部署脚本
sudo ./scripts/deploy-controller.sh

# 4. 编辑配置文件
sudo nano /etc/bandwidth-controller/controller.yaml
```

### 配置 Controller

编辑 `/etc/bandwidth-controller/controller.yaml`:

```yaml
server:
  auth_token: "生成一个强密码令牌"  # 重要!使用 openssl rand -base64 32 生成

agents:
  - id: "agent-001"
    host: "vps1.你的域名.com"  # 改成你的 VPS 地址
    name: "VPS-Tokyo-1"
    max_bandwidth: 1500
    region: "tokyo"

  # ... 配置所有 15 台 VPS

download_urls:
  - "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"
  - "https://dl.google.com/go/go1.21.6.linux-amd64.tar.gz"
  # ... 添加更多 Google 服务 URL
```

### 启动 Controller

```bash
# 启用开机自启
sudo systemctl enable bandwidth-controller

# 启动服务
sudo systemctl start bandwidth-controller

# 查看状态
sudo systemctl status bandwidth-controller

# 查看实时日志和控制面板
sudo journalctl -u bandwidth-controller -f
```

## 步骤 3: 部署 Agents (15台 VPS)

在每台 VPS 上执行以下操作:

### 方法 1: 使用部署脚本

```bash
# 1. 上传文件到 VPS
scp agent-linux-amd64 scripts/deploy-agent.sh root@vps1.example.com:~/

# 2. SSH 登录到 VPS
ssh root@vps1.example.com

# 3. 确保安装了 wget
apt-get install wget  # Debian/Ubuntu
# 或
yum install wget       # CentOS/RHEL

# 4. 赋予执行权限
chmod +x agent-linux-amd64 deploy-agent.sh
mv agent-linux-amd64 agent

# 5. 运行部署脚本
sudo ./deploy-agent.sh \
  agent-001 \
  "VPS-Tokyo-1" \
  controller.你的域名.com \
  你的auth_token

# 6. 启动 Agent
sudo systemctl enable bandwidth-agent
sudo systemctl start bandwidth-agent
sudo systemctl status bandwidth-agent
```

### 方法 2: 批量部署脚本

创建一个本地脚本来批量部署所有 agent:

```bash
#!/bin/bash
# deploy-all-agents.sh

AGENTS=(
  "agent-001:VPS-Tokyo-1:vps1.example.com"
  "agent-002:VPS-Tokyo-2:vps2.example.com"
  "agent-003:VPS-Singapore-1:vps3.example.com"
  # ... 添加所有 15 台
)

CONTROLLER_HOST="controller.example.com"
AUTH_TOKEN="你的auth_token"

for agent in "${AGENTS[@]}"; do
  IFS=':' read -r agent_id agent_name agent_host <<< "$agent"

  echo "Deploying $agent_id to $agent_host..."

  # 上传文件
  scp agent-linux-amd64 scripts/deploy-agent.sh root@$agent_host:~/

  # 执行部署
  ssh root@$agent_host "
    chmod +x agent-linux-amd64 deploy-agent.sh && \
    mv agent-linux-amd64 agent && \
    sudo ./deploy-agent.sh $agent_id '$agent_name' $CONTROLLER_HOST $AUTH_TOKEN && \
    sudo systemctl enable bandwidth-agent && \
    sudo systemctl start bandwidth-agent
  "

  echo "$agent_id deployed!"
done
```

运行:
```bash
chmod +x deploy-all-agents.sh
./deploy-all-agents.sh
```

## 步骤 4: 验证部署

### 检查 Controller 状态

```bash
# 查看实时控制面板
sudo journalctl -u bandwidth-controller -f

# 访问 API
curl http://localhost:9090/metrics | jq
curl http://localhost:9090/agents | jq
```

你应该看到:
- 所有 15 个 agents 显示为 `connected: true`
- 总带宽接近 10Gbps
- 控制面板显示活跃的 agents 和实时带宽

### 检查每个 Agent

在每台 VPS 上:

```bash
# 查看状态
sudo systemctl status bandwidth-agent

# 查看日志
sudo journalctl -u bandwidth-agent -n 50
```

### 远程批量检查

```bash
#!/bin/bash
# check-all-agents.sh

AGENTS=(
  "vps1.example.com"
  "vps2.example.com"
  # ... 所有 VPS
)

for host in "${AGENTS[@]}"; do
  echo "Checking $host..."
  ssh root@$host "systemctl is-active bandwidth-agent"
done
```

## 步骤 5: 监控

### 实时控制面板

Controller 会在控制台显示实时仪表板:

```
═══════════════════════════════════════════════════════════════
          Google Bandwidth Controller Dashboard
═══════════════════════════════════════════════════════════════

Target: 10.00 Gbps | Current: 9.85 Gbps (98.5%)
Active Agents: 5/15
Next Rotation: 14:32:15 (45s)
Phase: stable | Rotations: 127

Agent Bandwidth Breakdown:
─────────────────────────────────────────────────────────────
VPS-Tokyo-1          [████████████░░░░░░░░]    1250 Mbps
VPS-Singapore-1      [██████████░░░░░░░░░░]    1050 Mbps
...
```

### HTTP API 监控

```bash
# 获取当前指标
curl http://controller:9090/metrics

# 获取系统状态
curl http://controller:9090/status

# 获取 agent 列表
curl http://controller:9090/agents

# 获取 24 小时统计
curl "http://controller:9090/stats?duration=24h"
```

### 设置定时检查

创建监控脚本:

```bash
#!/bin/bash
# monitor.sh

while true; do
  BW=$(curl -s http://localhost:9090/metrics | jq -r '.total_bandwidth_gbps')
  ACTIVE=$(curl -s http://localhost:9090/agents | jq -r '.connected')

  echo "$(date): Bandwidth: ${BW}Gbps, Connected: ${ACTIVE}/15"

  # 如果带宽低于 8.5 Gbps,发送告警
  if (( $(echo "$BW < 8.5" | bc -l) )); then
    echo "WARNING: Bandwidth too low!"
    # 这里可以添加邮件/Slack通知
  fi

  sleep 60
done
```

## 故障排查

### Agent 无法连接

1. 检查网络连接:
```bash
telnet controller.example.com 8080
```

2. 检查防火墙:
```bash
# Controller 上开放端口
sudo ufw allow 8080/tcp
sudo ufw allow 9090/tcp
```

3. 检查 auth_token 是否匹配

### 带宽过低

1. 检查有多少 agent 已连接:
```bash
curl http://controller:9090/agents | jq '.connected'
```

2. 检查单个 agent 的带宽上限是否合理

3. 检查 VPS 到 Google 的网络质量

### Controller 崩溃

```bash
# 查看崩溃日志
sudo journalctl -u bandwidth-controller -n 100

# 重启服务
sudo systemctl restart bandwidth-controller
```

## 更新部署

当有新版本时:

```bash
# 1. 下载新版本
wget https://github.com/YOUR_USERNAME/google-bandwidth-controller/releases/latest/download/bandwidth-controller-linux-amd64.tar.gz

# 2. 停止服务
sudo systemctl stop bandwidth-controller

# 3. 备份旧版本
sudo cp /opt/bandwidth-controller/bin/controller /opt/bandwidth-controller/bin/controller.bak

# 4. 替换二进制文件
sudo cp controller-linux-amd64 /opt/bandwidth-controller/bin/controller

# 5. 重启服务
sudo systemctl start bandwidth-controller
```

对 agents 执行相同操作。

## 卸载

### Controller

```bash
sudo systemctl stop bandwidth-controller
sudo systemctl disable bandwidth-controller
sudo rm /etc/systemd/system/bandwidth-controller.service
sudo rm -rf /opt/bandwidth-controller
sudo rm -rf /etc/bandwidth-controller
sudo userdel controller
```

### Agent

```bash
sudo systemctl stop bandwidth-agent
sudo systemctl disable bandwidth-agent
sudo rm /etc/systemd/system/bandwidth-agent.service
sudo rm -rf /opt/bandwidth-agent
sudo rm -rf /etc/bandwidth-agent
```

## 生产环境建议

1. **使用 TLS**: 在生产环境中添加 TLS 支持
2. **监控告警**: 集成 Prometheus/Grafana 进行监控
3. **日志轮转**: 配置日志轮转防止磁盘满
4. **备份配置**: 定期备份配置文件
5. **高可用**: 考虑部署多个 controller 实例(需要额外开发)

## 性能优化

1. 增加系统资源限制:
```bash
# /etc/security/limits.conf
* soft nofile 65536
* hard nofile 65536
```

2. 优化网络参数:
```bash
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.wmem_max=134217728
```

3. 使用 SSD 存储临时文件

## 支持

如遇问题:
1. 查看日志: `journalctl -u bandwidth-controller -f`
2. 检查 API: `curl http://localhost:9090/metrics`
3. 查看 GitHub Issues
