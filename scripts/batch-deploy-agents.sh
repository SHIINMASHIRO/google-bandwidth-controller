#!/bin/bash

# 批量部署 Agent 脚本
# 用于一次性在多台 VPS 上部署 agent

set -e

echo "════════════════════════════════════════════════════════════════"
echo "     批量部署 Bandwidth Agents"
echo "════════════════════════════════════════════════════════════════"
echo ""

# 配置 - 请修改以下信息
CONTROLLER_HOST="controller.example.com"
AUTH_TOKEN="YOUR_AUTH_TOKEN_HERE"

# Agent 列表 - 格式: "agent_id:agent_name:ssh_host"
AGENTS=(
    "agent-001:VPS-Tokyo-1:vps1.example.com"
    "agent-002:VPS-Tokyo-2:vps2.example.com"
    "agent-003:VPS-Singapore-1:vps3.example.com"
    "agent-004:VPS-Singapore-2:vps4.example.com"
    "agent-005:VPS-HongKong-1:vps5.example.com"
    "agent-006:VPS-HongKong-2:vps6.example.com"
    "agent-007:VPS-Seoul-1:vps7.example.com"
    "agent-008:VPS-Seoul-2:vps8.example.com"
    "agent-009:VPS-LA-1:vps9.example.com"
    "agent-010:VPS-LA-2:vps10.example.com"
    "agent-011:VPS-SanJose-1:vps11.example.com"
    "agent-012:VPS-SanJose-2:vps12.example.com"
    "agent-013:VPS-Seattle-1:vps13.example.com"
    "agent-014:VPS-Seattle-2:vps14.example.com"
    "agent-015:VPS-Taiwan-1:vps15.example.com"
)

# SSH 用户 (默认 root)
SSH_USER="${SSH_USER:-root}"

# 检查配置
if [ "$CONTROLLER_HOST" = "controller.example.com" ] || [ "$AUTH_TOKEN" = "YOUR_AUTH_TOKEN_HERE" ]; then
    echo "❌ 请先编辑此脚本，配置 CONTROLLER_HOST 和 AUTH_TOKEN!"
    echo ""
    echo "需要修改的变量:"
    echo "  CONTROLLER_HOST - Controller 服务器地址"
    echo "  AUTH_TOKEN      - 认证令牌 (与 controller.yaml 中一致)"
    echo "  AGENTS          - 15 台 VPS 的信息"
    echo ""
    exit 1
fi

echo "Controller: $CONTROLLER_HOST"
echo "将部署 ${#AGENTS[@]} 个 agents"
echo ""

# 确认
read -p "确认开始部署? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "已取消"
    exit 0
fi

echo ""
echo "开始批量部署..."
echo ""

SUCCESS_COUNT=0
FAIL_COUNT=0
FAILED_AGENTS=()

for agent_info in "${AGENTS[@]}"; do
    IFS=':' read -r agent_id agent_name ssh_host <<< "$agent_info"

    echo "════════════════════════════════════════════════════════════════"
    echo "部署 $agent_id ($agent_name) 到 $ssh_host"
    echo "════════════════════════════════════════════════════════════════"

    # 通过 SSH 执行安装脚本
    if ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no "$SSH_USER@$ssh_host" "curl -fsSL https://raw.githubusercontent.com/SHIINMASHIRO/google-bandwidth-controller/main/scripts/install-agent.sh | bash -s -- '$agent_id' '$agent_name' '$CONTROLLER_HOST' '$AUTH_TOKEN' && systemctl enable bandwidth-agent && systemctl start bandwidth-agent"; then
        echo "✅ $agent_id 部署成功"
        ((SUCCESS_COUNT++))
    else
        echo "❌ $agent_id 部署失败"
        FAILED_AGENTS+=("$agent_id ($ssh_host)")
        ((FAIL_COUNT++))
    fi

    echo ""
    sleep 2
done

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "批量部署完成"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "结果统计:"
echo "  ✅ 成功: $SUCCESS_COUNT"
echo "  ❌ 失败: $FAIL_COUNT"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo "失败的 Agents:"
    for failed in "${FAILED_AGENTS[@]}"; do
        echo "  - $failed"
    done
    echo ""
fi

echo "检查所有 Agent 状态:"
echo ""

for agent_info in "${AGENTS[@]}"; do
    IFS=':' read -r agent_id agent_name ssh_host <<< "$agent_info"

    echo -n "  $agent_id ($ssh_host): "
    if ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$SSH_USER@$ssh_host" "systemctl is-active bandwidth-agent" 2>/dev/null; then
        echo "✅ 运行中"
    else
        echo "❌ 未运行"
    fi
done

echo ""
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "💡 下一步:"
echo ""
echo "1. 检查 Controller 是否看到所有 agents:"
echo "   curl http://$CONTROLLER_HOST:9090/agents | jq"
echo ""
echo "2. 查看总带宽:"
echo "   curl http://$CONTROLLER_HOST:9090/metrics | jq"
echo ""
echo "3. 查看某个 agent 的日志:"
echo "   ssh $SSH_USER@vps1.example.com 'journalctl -u bandwidth-agent -f'"
echo ""
echo "════════════════════════════════════════════════════════════════"
