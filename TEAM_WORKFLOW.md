# 团队 Git 协作与 Ubuntu 生产发布

团队项目：Niu-DaLi（大力牛-Niu DaLi）。

本仓库 `zuimeidamowang/Niu-DaLi-AWS` 仅服务 AWS 环境。唯一应用镜像包是
`ghcr.io/zuimeidamowang/niu-dali-aws`，与旧项目的代码发布、镜像标签、服务器配置和业务数据分别管理。
禁止向旧项目推送本仓库任务分支、复用旧镜像包或通过本仓库操作其他服务器。

适用场景：在 new-api 基础上维护团队自己的 API 服务。多人可能同时开发，也可能轮流接手；每个人在 Mac mini 或自己的电脑上开发，共用一个团队 Git 仓库，最终由 Ubuntu 服务器运行生产服务。保留原项目名称、QuantumNous 归属及许可证信息，继续遵守 [AGENTS.md](AGENTS.md)。

## 1. 固定分工

| 位置 / 角色 | 职责 |
| --- | --- |
| 开发者电脑 | 独立克隆仓库，修改、测试、提交；使用测试配置和测试数据 |
| 团队仓库 `origin` | 保存团队代码、任务分支、PR 和检查结果，是团队协作的唯一代码来源 |
| 官方 new-api 项目 | 保留来源归属；如需引入更新，另行授权并通过独立任务分支评估 |
| `main` | 保存已审核的集成结果；合并不代表已上线 |
| Ubuntu 生产服务器 | 运行负责人选定的版本，保存独立的生产配置与业务数据 |
| 发布负责人 | 决定发布版本、核实备份、执行发布并记录结果；一次只允许一人操作生产发布 |

先采用 `main` 加短期任务分支，不设置长期 `develop`、个人永久分支或服务器专用开发分支。多人不需要同时在线，通过已推送的分支和 PR 交接；不要共享同一个工作目录或正在编辑的分支。

## 2. AWS 仓库与本机设置

本工作目录的唯一开发与发布远程为：

```text
origin    git@github.com:zuimeidamowang/Niu-DaLi-AWS.git
```

本机 `remote.pushDefault=origin`，`main` 跟踪 AWS 的 `origin/main`。不把旧项目配置为本仓库的开发或发布远程。
每次推送前核实实际远程 URL，显式使用 `git push -u origin HEAD`，不向其他仓库镜像同步。

本机 Git 使用仓库级 SSH 443 配置；其他成员使用各自凭据，不复制私钥。真实密钥、服务器登录文件和环境配置不进入 Git。
已继承的 new-api 历史、QuantumNous 归属与许可证保留；环境隔离不删除来源记录，也不重写任何仓库历史。
官方来源：<https://github.com/QuantumNous/new-api.git>。此链接仅记录来源归属，不作为本目录的开发或发布远程。

先检查：

```sh
git status --short --branch
git remote -v
```

已有工作目录若仍关联其他项目，应先检查其工作区和远程配置，不自动覆盖远程、丢弃改动或向旧项目推送。
本 AWS 目录不保留旧项目的 `upstream`；禁止 `git push --mirror`、强制推送或跨项目自动同步。

确认团队 `main` 存在后，运行 `git fetch origin`，再用 `git branch --set-upstream-to=origin/main main` 绑定本地分支，并检查 `git remote -v` 和 `git branch -vv`。

新成员独立克隆 AWS 仓库：

```sh
git clone git@github.com:zuimeidamowang/Niu-DaLi-AWS.git
cd Niu-DaLi-AWS
git config --local remote.pushDefault origin
git config --local pull.ff only
```

项目仓库名称可以作为团队地址使用，源码中的 new-api 名称、QuantumNous 归属和许可证保持原样。

团队 GitHub 仓库由维护者设置：

- 成员使用各自账号和凭据，按职责授予权限，不共享账号或 SSH 私钥。
- 默认分支为 `main`；要求 PR 合并、另一名协作者批准、讨论已解决，禁止强制推送和删除 `main`。
- 要求实际运行并成功的 CI 检查；更新分支后重新检查，代码变更后重新评审。首次 PR 跑完后，从平台实际显示的检查名称中选择必需检查。
- 当前 [CI](.github/workflows/ci.yml) 包括 `Backend vet, build, and test` 和 `Frontend typecheck and test`。前端构建及领域专项验证仍按 `AGENTS.md` / `web/AGENTS.md` 执行，不能仅凭这两个检查推断所有要求均已覆盖。
- GitHub 私有仓库的分支保护等功能取决于账号方案；确认规则实际可用并已启用。此 Markdown 文件本身不会开启服务端限制。

AWS 应用镜像只通过 `.github/workflows/publish-image.yml` 发布到独立 `niu-dali-aws` 包。
旧 Docker Hub、GitCode 工作流在本仓库跳过；普通 CI 和只写当前仓库的 Release/Electron 工作流保留。
现阶段使用完整提交 SHA 记录生产版本；构建、合并与服务器部署是独立动作。新包的 public 权限须单独设置并验证。

## 3. 每次开始开发

先检查工作区。如果存在未提交修改，确认归属并保存到原任务分支；不要自动丢弃、覆盖或混入新任务，也不要在他人的工作目录中切换分支。

下面的命令仅用于工作区干净、团队 `origin` 已配置的情况；分支名用本次任务名替换：

```sh
git status --short --branch
git remote -v
git fetch origin
git switch main
git pull --ff-only origin main
git switch -c feat/channel-setup
```

命名约定：功能用 `feat/<task>`，修复用 `fix/<task>`，维护用 `chore/<task>`；AI 代理默认用 `codex/<task>`。一个分支只处理一项聚焦任务。`pull --ff-only` 失败时先检查本地提交与远程差异，不用强制重置解决。

提交前检查差异、运行适用验证，只暂存本次修改的具体文件：

```sh
git diff --check
git diff
git add path/to/changed-file
git diff --cached
git commit -m "feat: describe the change"
git push -u origin HEAD
```

`path/to/changed-file` 和提交说明都是示例，需要替换。推送前核实远程地址和暂存内容，尤其不要将本地配置、凭据或其他人的未完成工作一并提交。创建目标为团队 `main` 的 PR；模板和 AI 辅助披露遵守 `AGENTS.md` 的现有规定。

## 4. 轮流开发和交接

工作尚未完成时，可在任务分支提交清晰的阶段成果，推送后用 Draft PR 交接。仅保存在本机或 stash 中的修改，其他人无法接手。按照现有 PR 模板填写，并在适当段落包含：

- 分支、完整提交 SHA、当前负责人及下一位接手人。
- 已完成、剩余工作和已知问题。
- 实际运行过的验证命令及结果；未运行的检查明确注明。
- 配置变更的键名与用途，不写真实凭据。

接手人先确认前一位已停止修改该分支，再在自己的干净工作区操作。首次获取该分支：

```sh
git fetch origin
git switch --track origin/feat/channel-setup
```

本地已经有该分支时，使用 `git switch feat/channel-setup` 后执行 `git pull --ff-only origin feat/channel-setup`，不要重复创建。核对 `git rev-parse HEAD` 与交接记录，再继续工作。

共同使用过的分支不重写历史、不强推。需要同步团队 `main` 时，在任务分支上执行 `git fetch origin` 和 `git merge origin/main`，解决冲突后重跑相关验证。遇到其他人已更新远程导致推送失败，先获取和整合提交，不用强推覆盖。

两个人同时做不同需求时，各建分支；需要修改同一功能时先明确负责人和改动边界。原任务合并后，后续工作从最新 `main` 创建新分支，不继续向已合并分支累积修改。

## 5. 合并与上游同步

维护者异步检查 PR 的范围、行为、验证结果及配置影响，满足评审和检查要求后合并。日常任务使用 squash merge；首次代码导入和上游同步使用 merge commit，保留远程初始提交与上游的完整祖先关系。服务器发布记录使用合并到 `main` 后的提交 SHA，而不是功能分支合并前的 SHA。

如需引入官方 new-api 更新，先明确授权来源和版本，再从 AWS `main` 创建独立任务分支评估与验证。
不自动获取或合并旧部署项目的代码，不将外部 `main` 覆盖 AWS `main`，生产服务器只消费经审核的 AWS 镜像。

## 6. Ubuntu 生产发布

初期采用负责人手动发布，不要求所有协作者拥有生产 SSH 权限。流程为：

```text
任务分支 → PR 和验证 → 团队 main → 负责人选定提交
                                      ↓
                       备份与升级检查 → 部署 → 验收与记录
```

每次发布依次执行：

1. 记录目标完整提交 SHA、当前线上 SHA 和当前应用镜像标识。确认目标属于团队 `main`，所需检查已通过、变更已审核。
2. 确认没有另一人正在发布，检查 Ubuntu 发布目录没有未解释的修改。配置、持久化数据和备份独立于源码发布目录管理。
3. 在升级前完成可恢复的数据库及必要文件备份，记录位置和恢复办法。涉及数据库升级时先完成项目要求的数据库验证，评估旧版程序是否仍能使用升级后的数据库。
4. 获取选定提交，构建适配 Ubuntu CPU 架构的应用镜像，或拉取由该提交构建的团队镜像。记录完整 SHA 与镜像 ID / digest 的对应关系，保留上一版镜像；不要用可变 `latest` 作为唯一发布记录。
5. 用已配置的生产 Compose 更新应用服务。例行应用发布不顺带升级数据库或 Redis，不删除数据卷，不运行 `docker compose down -v`。
6. 检查服务健康、错误日志，并用专用测试账号验证实际 API 请求、流式返回和消费记录。确认结果后记录发布时间、操作人、目标 SHA、镜像、备份与验收结果。
7. 失败时先判断旧版与当前数据库是否兼容。兼容时切回保留的旧应用镜像；不兼容时按评审过的修复/恢复方案处理。不要直接覆盖生产数据库，恢复备份可能丢失备份后的客户数据，必须明确影响和处理决定。

**当前部署边界：** 根目录 [docker-compose.yml](docker-compose.yml) 保留本地开发用途及 `calciumion/new-api:latest` 上游标识，AWS 生产使用 [deploy/docker-compose.prod.yml](deploy/docker-compose.prod.yml) 参考模板与独立 `.env`。
生产 `APP_IMAGE` 仅使用 `ghcr.io/zuimeidamowang/niu-dali-aws:sha-<完整SHA>`。AWS 的域名、路径、数据库、Redis、备份与凭据按服务器实际情况独立核对，不能复用旧服务器的真实配置或数据。
本规范不代表生产服务器已同步修改、GitHub 包已设为 public 或自动部署已经启用。

发布记录保存在团队 PR / 发布记录或受控运维记录中；只记录备份标识和配置键名，不附密钥或数据库内容。需要自动部署时，再配置独立的生产发布入口、权限和部署互斥，避免多人触发并发上线。

## 7. 环境与敏感文件

| 内容 | 是否进入 Git | 管理方式 |
| --- | --- | --- |
| 源码、锁文件、Dockerfile、无密钥的共享配置与脚本 | 是 | PR 审核，统一版本 |
| `.env.example` / `.env.*.example` | 是 | 只含配置名和示例值，不含真实凭据 |
| `.env`、`.env.*` 实际配置、`local.env*` | 否 | 每台机器独立维护 |
| `服务器/` 下的登录信息和私钥 | 否 | 仅本地保存，也不进入 Docker 构建上下文 |
| 客户数据、上游 Key、余额、日志、备份 | 否 | 存储于独立环境及受控备份位置，不用 Git 同步 |

已有 `.gitignore` / `.dockerignore` 排除相关本地文件。忽略规则不会移除已经被 Git 跟踪或进入历史的秘密；发现已提交凭据时先停止继续传播，安排撤销/轮换与历史清理，不以新增忽略规则代替处理。

生产目前按应用、PostgreSQL、Redis 三个容器规划；人数增加不需要复制生产容器。每位开发者使用自己的测试数据库与缓存。若后续增加公共测试环境，数据库、Redis、端口、卷、凭据及测试渠道必须独立，严禁本机测试库覆盖正式库，也不以开发配置连接客户生产数据。

## 8. 落地清单

- [x] 确认 AWS 团队仓库地址，本机只保留 AWS `origin`，解除旧项目远程关联。
- [x] 完成本机 SSH 认证并获取远程 `main`，核实远程初始历史。
- [x] 按负责人指示将首次导入代码纳入 `main`。
- [ ] 配置团队成员权限。
- [ ] 创建首个团队 PR，核实 CI 实际结果并启用 `main` 保护。
- [ ] 指定 PR 维护者、发布负责人和交接方式。
- [ ] 确认 Ubuntu 当前运行版本、配置与数据位置，建立备份和恢复方案。
- [ ] 配置团队应用镜像及生产 Compose，用测试账号完成一次发布验收。

这些事项需要实际配置和验证后再勾选，不能仅因文档已写入就视为完成。
