# 如何上传到 GitHub 并自动构建

## 步骤 1: 在 GitHub 创建新仓库

1. 访问 https://github.com/new
2. 仓库名称: `google-bandwidth-controller` (或你喜欢的名字)
3. 描述: `Bandwidth traffic controller for Google PNI with natural traffic patterns`
4. 选择 **Public** 或 **Private** (根据你的需求)
5. **不要** 勾选 "Initialize this repository with a README"
6. 点击 "Create repository"

## 步骤 2: 推送代码到 GitHub

在本地项目目录执行:

```bash
cd "/Users/Mashiro/Projects/AVEN/Google ISP"

# 添加远程仓库 (替换 YOUR_USERNAME 为你的 GitHub 用户名)
git remote add origin https://github.com/YOUR_USERNAME/google-bandwidth-controller.git

# 推送代码
git push -u origin main
```

如果推送失败,可能需要配置 GitHub 认证:

### 使用 Personal Access Token (推荐)

1. 访问 https://github.com/settings/tokens
2. 点击 "Generate new token" -> "Generate new token (classic)"
3. 勾选 `repo` 权限
4. 生成并复制 token
5. 推送时输入用户名和 token (作为密码)

或者配置 credential helper:

```bash
git config --global credential.helper store
git push -u origin main
# 输入用户名和 token
```

### 使用 SSH (可选)

```bash
# 如果你已配置 SSH key
git remote set-url origin git@github.com:YOUR_USERNAME/google-bandwidth-controller.git
git push -u origin main
```

## 步骤 3: 等待自动构建

推送后,GitHub Actions 会自动:

1. 访问仓库页面
2. 点击 "Actions" 标签
3. 你会看到 "Build Binaries" workflow 正在运行
4. 等待构建完成 (通常 2-5 分钟)

构建完成后会生成:
- `bandwidth-controller-linux-amd64.tar.gz`
- `bandwidth-controller-linux-arm64.tar.gz`

## 步骤 4: 下载构建好的二进制文件

### 方法 1: 从 Actions Artifacts 下载

1. 进入 "Actions" 标签
2. 点击最新的 workflow run
3. 向下滚动找到 "Artifacts" 部分
4. 下载 `bandwidth-controller-linux-amd64` (或 arm64)

### 方法 2: 创建 Release (推荐用于生产)

```bash
# 在本地创建 tag
git tag -a v1.0.0 -m "First release"
git push origin v1.0.0
```

然后:
1. 访问 GitHub 仓库页面
2. 点击 "Releases"
3. 点击 "Draft a new release"
4. 选择 tag: v1.0.0
5. 填写发布说明
6. 点击 "Publish release"

GitHub Actions 会自动构建并上传二进制文件到 Release!

下载 Release:
```bash
# 直接下载 Release 文件
wget https://github.com/YOUR_USERNAME/google-bandwidth-controller/releases/download/v1.0.0/bandwidth-controller-linux-amd64.tar.gz
```

## 步骤 5: 部署到服务器

下载构建好的文件后,按照 [DEPLOYMENT.md](DEPLOYMENT.md) 部署。

### 快速部署示例

在 Controller 服务器上:

```bash
# 1. 下载
wget https://github.com/YOUR_USERNAME/google-bandwidth-controller/archive/refs/heads/main.zip
unzip main.zip
cd google-bandwidth-controller-main

# 或从 Release 下载
wget https://github.com/YOUR_USERNAME/google-bandwidth-controller/releases/download/v1.0.0/bandwidth-controller-linux-amd64.tar.gz
tar xzf bandwidth-controller-linux-amd64.tar.gz
cd bandwidth-controller-linux-amd64

# 2. 部署 Controller
chmod +x scripts/deploy-controller.sh controller-linux-amd64
mv controller-linux-amd64 controller
sudo ./scripts/deploy-controller.sh

# 3. 配置
sudo nano /etc/bandwidth-controller/controller.yaml

# 4. 启动
sudo systemctl enable bandwidth-controller
sudo systemctl start bandwidth-controller
```

在每台 Agent VPS 上:

```bash
# 1. 下载 agent 二进制
wget https://github.com/YOUR_USERNAME/google-bandwidth-controller/releases/download/v1.0.0/bandwidth-controller-linux-amd64.tar.gz
tar xzf bandwidth-controller-linux-amd64.tar.gz
cd bandwidth-controller-linux-amd64

# 2. 部署
chmod +x scripts/deploy-agent.sh agent-linux-amd64
mv agent-linux-amd64 agent
sudo ./scripts/deploy-agent.sh \
  agent-001 \
  "VPS-Tokyo-1" \
  controller.example.com \
  YOUR_AUTH_TOKEN

# 3. 启动
sudo systemctl enable bandwidth-agent
sudo systemctl start bandwidth-agent
```

## 检查部署状态

### Controller

```bash
# 查看实时控制面板
sudo journalctl -u bandwidth-controller -f

# 查看指标
curl http://localhost:9090/metrics | jq
curl http://localhost:9090/agents | jq
```

### Agent

```bash
# 查看状态
sudo systemctl status bandwidth-agent

# 查看日志
sudo journalctl -u bandwidth-agent -f
```

## 自动化建议

### 使用 GitHub Actions 定时构建

在 `.github/workflows/build.yml` 中添加:

```yaml
on:
  push:
    branches: [ main ]
  schedule:
    - cron: '0 0 * * 0'  # 每周日构建
```

### 设置部署密钥

如果想要自动部署到服务器:

1. 在服务器生成 SSH key
2. 将公钥添加到服务器的 authorized_keys
3. 将私钥添加到 GitHub Secrets
4. 创建部署 workflow

## 常见问题

### Q: 构建失败怎么办?

A: 检查 Actions 日志:
1. 进入 "Actions" 标签
2. 点击失败的 workflow
3. 查看错误信息
4. 常见原因: Go 版本不兼容、依赖下载失败

### Q: 如何更新代码?

A: 在本地修改后:

```bash
git add .
git commit -m "your changes"
git push
```

GitHub Actions 会自动重新构建。

### Q: 如何回滚到旧版本?

A: 使用 git tags:

```bash
# 查看所有版本
git tag

# 切换到旧版本
git checkout v1.0.0

# 重新推送
git push -f origin main
```

### Q: 可以本地构建吗?

A: 可以:

```bash
# 构建当前平台
make build

# 构建 Linux AMD64
make linux-amd64

# 构建所有平台
make linux
```

## 生产环境建议

1. **使用 Release Tags**: 不要直接使用 main 分支部署
2. **版本管理**: 使用语义化版本 (v1.0.0, v1.1.0, etc.)
3. **测试环境**: 先在测试环境验证
4. **回滚计划**: 保留旧版本二进制文件
5. **监控告警**: 设置带宽监控和告警

## 下一步

1. ✅ 上传代码到 GitHub
2. ✅ 等待自动构建完成
3. 📥 下载构建好的二进制文件
4. 🚀 按照 DEPLOYMENT.md 部署到服务器
5. 📊 访问监控面板验证运行状态
6. 🎯 开始跑 Google PNI 带宽!

## 技术支持

如遇问题:
- 查看 [README.md](README.md) - 项目说明
- 查看 [DEPLOYMENT.md](DEPLOYMENT.md) - 详细部署指南
- 查看 GitHub Issues - 已知问题和解决方案
- 查看 Actions 日志 - 构建错误信息
