# 统一素材接口：部署与验收

本功能先开发、测试，确认后发布。开发分支 `codex/unified-media-api` 基于 `b69dc87bd452d7e305b1ba0eda775cd0a27ab751`，未修改生产环境。

## 资源与配置

初期复用现有 EC2 和数据库；新增一个与服务器同区域的私有 S3 bucket，以及当前应用工作负载角色对 `input-media/*` 的 `PutObject / GetObject / DeleteObject` 权限即可。无需为用户开放 S3，也无需让用户注册上游或取得上游上传链接。资源参考模板是 `deploy/media-assets-s3.yaml`；模板只创建 bucket 和附加到指定现有角色的最小对象权限，不创建新服务器。创建资源及授予 IAM 权限属于发布操作，待确认后执行。

参考 `deploy/media-assets.env.example`，向应用环境加入配置。生产 Compose 的权威文件仍为 `/srv/new-api/production/compose.yaml`，先核对 `env_file` 和现网代理，不覆盖现网文件。默认图片 50 MiB、视频 500 MiB、每账号 5 GiB。生成一个独立随机签名密钥，例如 `openssl rand -hex 32`，放进服务器秘密配置，所有实例使用同一个值；不要提交到 Git，也不要放入渠道设置。AWS SDK 使用默认凭据链。生产建议使用 EC2/ECS 角色并自动刷新临时凭据；移除残留的 AWS_ACCESS_KEY_ID、AWS_SECRET_ACCESS_KEY、AWS_SESSION_TOKEN 和共享凭据覆盖，否则 SDK 会先选这些凭据。验证实际应用容器可取得角色身份；Docker bridge 下需核对 IMDSv2 网络与响应 hop limit（常需为 2），不开放公网元数据访问。

上传使用受限临时文件，之后写入私有 S3；为应用临时目录预留上传并发量对应的磁盘空间。500 MiB 文件并发上传会消耗相应临时磁盘、带宽及网关连接。S3 对象用 SSE-S3 加密。模板不启用版本控制；若接入已有启用版本控制的 bucket，应另外配置非当前版本清理，否则删除标记不会移除历史原件。

## 代理

只为 `POST /v1/media/assets` 放宽代理请求体上限至至少 501 MiB（500 MiB 文件加 multipart 边界），其他 API 保持原限制。代理限制传输体，应用限制解压后的请求体并验证单个文件大小。确认 CDN/WAF/负载均衡器也支持这个上限；若前置服务不支持 500 MiB，需为本站上传路径使用能承载该大小的入口，再验证 CORS。不要默默宣称 500 MiB 已上线。

内部素材来源路径 `/v1/media/assets/:id/source` 是 48 小时 HMAC 签名链接，只发给已鉴权的渠道插件供上游下载。网关流式读取私有 S3，支持 HEAD 与单段 Range，无需公开 bucket。应用仍校验素材状态、任务引用和过期时间。签名自解析请求时起固定有效 48 小时，不会随任务自动续期；上游应在此期间取得素材，当前不会自动更新已提交给上游的链接。原件在活动任务期间保留与链接有效期是独立规则。确保上游可以通过公网 HTTPS 访问该路径，代理保留 Range/Content-Range/Content-Length；不做公开缓存。

访问日志必须去掉签名查询参数，也不要记录 Authorization。Nginx 如需单独的日志格式，使用 `$request_method $uri $server_protocol`，不要在该路径记录 `$request` 或 `$request_uri`；Caddy、CDN、WAF、APM 的查询日志也应脱敏。应用访问日志已去掉素材路径的查询串，生产应使用 Gin release 模式。日志脱敏应覆盖成功与失败请求。

## 发布顺序

1. 按 `TEAM_WORKFLOW.md` 审核并合入团队 main，记录正式目标 commit、构建镜像 digest、上一个镜像和唯一发布负责人。
2. 备份现网数据库和渠道插件配置，先用副本试运行新表迁移两次。本功能新增 `media_assets` 和 `media_asset_tasks`，不改已有字段。不要删除旧表或自动回退数据库。
3. 创建/确认私有 S3、角色权限、HTTPS 代理、临时磁盘。配置环境，先保持 `MEDIA_ASSET_ENABLED=false`。签名密钥由服务器秘密配置注入。
4. 使用已审查 commit 构建网关和画布。画布在固定版本上按 `integrations/infinite-canvas/apply-defaults.sh` 叠加补丁，包含新增统一素材适配；不能只更换网关二进制。
5. 只更新应用服务。核对健康检查、迁移和已有文字/图片/视频 API 后，启用素材配置并重新启动应用服务。不要重建数据库/Redis/代理数据卷。
6. 当前生产数据库中安装的 Routek 插件为 1.0.2，它会覆盖内置插件。需先导出旧插件，再在管理端将该插件升级至本次 `plugins/tasks/routek-seedance/plugin.js`（1.1.0），或确认恢复内置版本。保留现有渠道 Key、模型映射和价格，不根据上游价自动修改客户售价。
7. 按下方步骤完成真实环境验收，再开放入口和告知客户。公共文档入口 `/docs` 随前端发布，示例使用本站 Key 和素材 ID。

## 验收与回退

先用专门测试账号上传无敏感内容的 PNG 和 MP4。确认响应只有本站素材元数据，无 S3 key、签名来源链接和上游凭据；另一账号访问、引用同一 ID 都失败。验证超大/伪造格式/多文件上传、存储额度、过期、HEAD、单段 Range，以及源链接篡改和过期。

用受控测试渠道验证“上传 → 图片参考/首尾帧/视频参考 → 查询 → 下载”，以及取消上传、上游失败、渠道重试不会重复提交或错误删除素材。正式上游生成会收费；在预算及测试模型明确后执行一组真实调用，并核对实际内容、计费和服务器日志。未经此步骤不能宣称上游端到端验收完成。

原件未使用时保留 24 小时；被任务使用后，在最后一个关联任务结束 24 小时后进入清理，清理器约每分钟重试；任务运行期间不清理。故障期间清理可能延迟，但过期素材不能继续用于新任务。不要为 `input-media/` 设置固定“一天删除”的 S3 生命周期规则，它会误删仍在运行的任务素材。清理器停机后，重启恢复清理；对象删除失败保留待删除记录继续重试。

发生故障先在代理阻断新上传 POST 和新生成请求，保留正在执行任务所需的来源 GET/HEAD、S3 配置和签名密钥。不要将 MEDIA_ASSET_ENABLED 设为 false 来停止新上传，该开关也会关闭运行任务的来源服务。旧镜像没有 `/source`，直接回退会导致已提交任务取图失败；先等待/处置这些任务，或保留新版本的来源服务。保留新增表、bucket 与密钥，不删除对象和数据卷。恢复旧插件需用备份，不重置渠道价格。按现网流程回退到已记录镜像并验证已有 API；记录未完成任务与后续清理负责人。

## 适配范围

客户提交统一 `media: [{asset_id, role}]`，插件声明 `media-assets@1`，服务端按插件转换为 HTTPS URL、multipart 文件引用或 base64 数据。更换这几种上游传输方式不需要修改画布或客户请求。上游若必须先注册素材、等待审核，再分阶段创建任务，需要另行扩展插件执行流程；当前单次 submit 描述符不能保证覆盖任意多阶段供应商。

资源授权与上传校验参考 OWASP [File Upload](https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html)、[Authorization](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)、[Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) Cheat Sheets。此功能复用现有 Bearer Key 鉴权，不新增登录/会话机制；验证重点是素材所有权、签名有效期、文件格式与大小、私有存储、日志脱敏和任务生命周期。上传服务校验文件大小、后缀、MIME、文件签名及图片尺寸，不执行完整视频解码或恶意软件扫描。

外部应用需支持自定义 HTTP 调用，按上传与生成两步完成。仅支持单次 OpenAI `input_reference` 文件字段的应用需要增加此适配，不能只改 Base URL 与 Key 就假定能够使用视频参考。

## 开发验证记录（2026-11-20）

验证在独立工作树进行，未向生产数据库写入测试数据，未创建 AWS 资源，未进行付费上游生成。

| 验证 | 已观察结果 |
| --- | --- |
| Go 1.25.1 根模块 `go build ./...`、`go vet ./...` | 通过 |
| `cd relaykit && GOWORK=off go build ./...`，Go 1.25.1 | 通过；独立模块未增加宿主依赖 |
| 下方 7 个后端包完整测试，Go 1.25.1 | 通过；连接独立 MySQL、PostgreSQL 和 Moto |
| 素材合同矩阵，SQLite 3.50.4 / MySQL 8.0.43 / PostgreSQL 15.10 | 通过；新建、代表性旧模型升级、迁移两次、已有任务保留、并发配额、所有权、文件校验、租约及清理 |
| 完整 `model.InitDB()` + `model.InitLogDB()`，上述三个数据库 | 通过；当前版本 fresh/restart 两轮，基线版本建库并写代表数据后 upgrade/restart 两轮；41 张旧表到 43 张当前表，已有字段/索引/主键不变，两次迁移 schema 一致，原数据和唯一性约束保留 |
| 私有对象读写，Moto 5.1.22 | 真实 AWS SDK 的 Put / Get / Range / Delete 通过；不等同于真实 AWS IAM / S3 验收 |
| 主站 `bun run typecheck`、定向 lint、`bun run build` | 通过；Node 24.14.0，新增公共文档支持 7 种语言 |
| 主站下方 3 个测试文件 | 25 项通过 |
| 画布 `bun run test`、`bun run typecheck`、`VITE_BASE=/canvas/ bun run build` | 114 项测试通过，505 个断言，类型检查与构建通过；保留既有包体积及动态导入提示 |
| 画布补丁 | 固定提交首次应用、旧三补丁升级、重复执行通过；升级保留无关本地文件，12 个素材补丁文件与已测试源码逐字节一致；冲突拒绝覆盖 |
| 浏览器 | 主站未登录可读 `/docs`，中英文及移动端布局通过；画布使用本地模拟服务验证选择视频、上传、单次生成、轮询、本站下载及历史恢复 |
| AWS 模板 `cfn-lint deploy/media-assets-s3.yaml` | 通过 |

后端实际测试命令如下。`go` 对应 Go 1.25.1；`NIU_MEDIA_TEST_MYSQL_DSN`、`NIU_MEDIA_TEST_POSTGRES_DSN` 指向一次性本地数据库，`NIU_MEDIA_TEST_S3_ENDPOINT=http://127.0.0.1:19000` 指向 Moto。测试数据与凭据不应替换为生产数据。

```sh
GIN_MODE=release go test ./controller ./middleware ./service ./relay ./relay/channel/task/jsplugin ./pkg/jsplugin ./plugins
go test ./controller -run '^TestMediaAssetContracts$' -count=1 -v
```

第二条合同矩阵另外以 Go 1.27.2 执行通过；同一矩阵亦包含在上述 Go 1.25.1 完整包测试中。主站实际定向命令（在 `web/`，Node 24.14.0）为：

```sh
bun run test -- src/features/api-docs/__tests__/api-docs.test.tsx src/hooks/__tests__/top-nav-links.test.tsx src/features/home/__tests__/storefront.test.tsx
bunx oxlint -c .oxlintrc.json src/features/api-docs src/routes/docs src/hooks/use-top-nav-links.ts src/hooks/__tests__/top-nav-links.test.tsx src/features/home/components/sections/hero.tsx src/features/home/__tests__/storefront.test.tsx
```

全局前端格式检查仍报告原有 `web/src/components/ui/titled-card.tsx` 格式问题，该文件未改动。上述本地模拟生成不验证上游是否接受真实素材；真实 S3/IAM、500 MiB 代理链路、生产副本迁移、真实模型与费用验收仍按发布顺序执行。

完整启动迁移的升级基线为已核实的团队 `origin/main`：`b69dc87bd452d7e305b1ba0eda775cd0a27ab751`。核查时团队 GitHub releases 列表和远端 tags 均为空，因此不能将此矩阵描述为某个已发布 tag 或现网数据库副本的升级验收。测试使用 Go 1.27.2 和独立临时数据库，共执行 18 次完整初始化。新增模型仅加入主库迁移列表，独立日志库模型未改变。

完整启动矩阵使用本机临时 `startup.go` 调用实际初始化函数，以 `run.py` 比较全部表结构、索引、主键和代表数据；本次命令记录如下，重复运行需重新建立隔离空库，不可将这些脚本指向业务库：

```sh
# 在任务工作树导出并构建基线与当前版本
mkdir -p /tmp/niu-media-full-migration.YBFgU6/baseline
git archive b69dc87bd452d7e305b1ba0eda775cd0a27ab751 | tar -x -C /tmp/niu-media-full-migration.YBFgU6/baseline
/Users/huangleilei/.local/go/bin/go build -o /tmp/niu-media-full-migration.YBFgU6/current-startup /tmp/niu-media-full-migration.YBFgU6/startup.go
(cd /tmp/niu-media-full-migration.YBFgU6/baseline && /Users/huangleilei/.local/go/bin/go build -o /tmp/niu-media-full-migration.YBFgU6/baseline-startup /tmp/niu-media-full-migration.YBFgU6/startup.go)
python3 /tmp/niu-media-full-migration.YBFgU6/run.py > /tmp/niu-media-full-migration.YBFgU6/results.log 2>&1
```

`results.log` 最终输出 `FULL_MIGRATION_MATRIX=PASS`。MySQL 和 PostgreSQL 专用库名为 `media_full_fresh_ybfgu6`、`media_full_upgrade_ybfgu6`；SQLite 使用临时文件，均未访问生产。

## 真实上游验收（2026-11-21，新加坡时间）

使用用户授权的 Routek Key，在独立本地网关、SQLite 测试库和画布测试配置中验收。上游 Key 只保存在本机受限临时配置中；画布仅保存独立测试客户的本站 Key。未修改生产渠道、价格、数据库或部署，也未创建 AWS 资源。输入存储使用 Moto 5.1.22；公网 HTTPS 临时入口只转发签名素材来源的 GET/HEAD，不公开管理、上传或生成接口。

真实调用使用 `doubao-seedance-2-0-fast`，渠道映射到 `doubao-seedance-2-0-fast-260128`，720p、6 秒、关闭音频。图片输入为自行制作的无敏感内容 PNG；视频参考复用第一段真实生成的视频。

| 路径 | 已观察结果 |
| --- | --- |
| API 上传图片 → 首帧生成 → 查询 → 本站下载 | 通过；上游成功读取签名 PNG，生成 MP4 1,607,104 字节，H.264、1280×720、24 fps、145 帧、约 6.04 秒，无音轨；完整解码通过 |
| 画布上传本地 MP4 → 视频参考生成 → 查询 → 播放/下载 | 通过；上游对本站视频来源的 GET 和 Range 均成功。本站下载及画布下载均为 1,884,077 字节，SHA-256 一致；H.264、1280×720、24 fps、145 帧、约 6.04 秒，无音轨，完整解码通过；浏览器播放器 `readyState=4`，已播放到结尾 |
| 公开查询 | 已修复 Routek `processing` 状态映射；后续真实任务正确进入 `in_progress`，轮询失败计数为 0。结果链接使用本站 `/v1/videos/<公开任务 ID>/content`，不返回上游带访问凭据的链接 |
| 画布刷新恢复 | 已验证重新载入后查询原任务，未重新上传或创建任务 |
| 消费核对 | 恰好 2 个成功任务、2 条消费记录，各 30,000 quota，共 60,000；钱包、Token、渠道一致，无重复扣费或误退款。隔离测试价为 0.01 USD/秒，合计测试账面 0.12 USD；该价格不代表 Routek 实际费用 |
| 原件保留 | 图片和视频的公开元数据到期时间，分别为最后关联任务完成后恰好 86,400 秒；活动任务期间受保护，孤立任务关联为 0 |

视频首次创建尝试在本机 DNS 解析阶段超时，尚未连接上游；恢复网络后重试才取得第二个任务 ID。失败尝试的预扣已恢复，没有额外任务、净扣费或孤立素材关联。不得对已取得任务 ID 的等待、查询或下载异常重新创建任务，应继续查询原 ID。

真实生成等待超过画布既有的单轮约 5 分钟边界。已修复本站素材适配的工作台：等待、查询或下载中断后，持久化记录仍保留 `pending` 和原任务 ID，提供“继续查询”；刷新恢复及按钮操作都只查询原 ID。只有明确收到任务终态失败才进入失败流程。无限画布节点既有 `videoTaskId` 保留和“查询任务”机制已提供相同保护；此次付费浏览器验收使用视频创作台，未额外创建第三个节点生成任务。

本轮修复后，Go 1.25.1 构建网关及 `go test ./plugins ./pkg/jsplugin ./relay/channel/task/jsplugin ./controller ./service ./middleware ./model` 通过，插件相关三个包的 `go vet` 通过。真实 AWS IAM/S3、生产代理的 500 MiB 上传链路、现网数据库副本迁移仍属于发布前验收。

画布完整测试 115 项、520 个断言、类型检查及 `/canvas/` 构建通过；最后修正文案命名空间后，追加实际 `i18n.t` 的中英文解析断言，定向 8 项、48 个断言和类型检查通过。14 个补丁文件通过首次应用、旧补丁升级、重复应用与逐字节比对。真实结果保存在本机验收产物中，未将视频、数据库、凭据或带签名的来源链接加入仓库。
