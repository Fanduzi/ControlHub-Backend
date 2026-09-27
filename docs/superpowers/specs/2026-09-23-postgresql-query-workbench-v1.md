# PostgreSQL 查询工作台首批实施规格（正式规格候选）

状态：Specification Candidate / ready-for-human，技术门禁已逐项定稿，待评审。日期：2026-09-23（定稿同日；外部评审修订 2026-09-26）。代码核查基线：后端 b222f29，前端 49a99ec；组件试验 `advisor-plans/023-postgresql-feasibility/lab` **25/25 PASS**（2026-09-26 实测）。

评审议题：[PostgreSQL 查询工作台首批接入规格草案](https://github.com/Fanduzi/ControlHub-Backend/issues/108)。议题正文保存完整草案；评审副本在分支 `spec/postgresql-query-workbench-v1`（仅含本文件）。

产品决策与测试边界已接受；本文汇总合同与验收，不表示所有技术方案已接受。技术待定项关闭、正式规格接受前，不标记 ready-for-agent，不提前开放 PostgreSQL 执行。

## Problem Statement

研发已能使用 MySQL/TiDB 查询工作台，但 PostgreSQL 目前仅被识别为目标类型，缺少真实执行、schema 浏览及治理适配。直接复用 MySQL 方言与对象身份会造成查询拒绝、同名对象混淆或错误披露。

目标是让研发完成一次受治理的 PostgreSQL 查数与复用流程，并保留现有 MySQL/TiDB 能力。组件试验为“有条件可行”，没有证明脱敏、历史、审计和前端闭环已经通过。

## Solution

管理员为 Inventory 数据库实例配置固定 database 的 Query Connection；研发在连接内选择 schema、浏览结构、运行只读查询并查看有界结果。查询支持选中/当前语句执行、基础补全、取消、历史恢复、保存模板及受控当前页 CSV。

一个实例可关联多个连接，不复制 CI。对象身份包含连接、database、schema、对象和列。精确数值和复杂类型以明确合同展示；查询窗口保留用户 LIMIT/OFFSET，平台预算只收紧范围。

## User Stories

1. 作为管理员，我能为同一实例配置不同数据库连接，无需创建重复 CI。
2. 作为管理员，我能配置默认 schema、服务端凭据、启用状态及生产策略。
3. 作为管理员，我能测试、轮换和停用连接，停用后拒绝新查询并保留证据。
4. 作为研发，我能选择已开放连接与 schema，并辨认当前查询上下文。
5. 作为研发，我能搜索表/视图、查看字段、索引及适用的外键结构。
6. 作为研发，我能获得当前对象上下文的基础表名和字段补全。
7. 作为研发，我能执行选中 SQL 或光标所在单条语句，字符串和注释中的分号不会误拆。
8. 作为研发，我能分页查看原 SQL 结果，用户窗口不会被平台扩大。
9. 作为研发，我能取消正在执行的查询，并看到与服务端证据一致的终态。
10. 作为研发，我能查看大整数、小数、JSON、数组、二进制和时间，不发生静默精度损失。
11. 作为研发，我能识别截断值，并了解为什么该页不能导出。
12. 作为研发，我能按披露权限复制值、导出当前页，而不能绕过脱敏。
13. 作为研发，我能保存 SQL 和完整上下文，恢复时不会悄悄切到同名对象。
14. 作为研发，我能使用 :name 参数模板，临时值不进入持久化历史。
15. 作为研发，我能看到空结果、不可用上下文和不支持定义 SQL 的明确提示。
16. 作为现有 MySQL/TiDB 用户，我的既有类型、分页与治理能力不因 PostgreSQL 接入而暗改。

## Implementation Decisions

以下为已接受的行为约束；具体存储/JSON 字段的建议形态仍受后文技术门禁约束。

### 身份、连接与授权

- 管理员集中接入；研发共享具备查询资格的开放目标。不增加个人连接、接入申请或用户/团队/项目白名单。
- Query Connection 固定 database，关联一个 Inventory 实例。连接、database、schema 分层表示，不用 database 字段承载 schema。
- 凭据地址、端口、database 必须与连接绑定一致，缺失或不匹配拒绝执行；生产需显式启用。
- 未限定对象名使用工作表当前 schema，不静默回退。显式 schema 名按实际对象校验。补全、策略匹配与执行解析必须一致。
- 保存 SQL、历史恢复和工作表上下文须携带相应身份；恢复重新校验，失效时明确报错或要求选择。上下文变化清除旧结果，隔离元数据与异步响应。
- 连接身份模型与现有一实例目标/凭据模型之间的迁移必须明确，不得借新增连接扩大旧权限。

### 查询、模板与治理

- 复用现有查询服务编排、历史和审计证据边界。引擎相关解析、元数据、执行、凭据绑定、模板编译和脱敏投影独立适配，避免通用插件框架。
- PostgreSQL 只读保护必须同时覆盖应用校验、数据库权限及执行边界。有限函数拒绝清单不是完整安全证明。
- 前端选中/光标执行只选择一条语句，服务端仍验证完整收到的语句。中文、emoji、美元引用和注释必须有边界测试。
- 模板沿用 :name 与字符串/整数/小数/布尔类型，后端生成位置参数并绑定。:: 类型转换、字符串、注释及美元引用不能被替换；临时值不进日志、审计、历史或持久化存储。
- 保留服务端披露策略。JSON 查看器、复制和 CSV 仅消费允许披露的值；无策略或不支持的投影不得偷偷放行。

### 预算、分页与取消

- 沿用当前 MySQL 预算基线：默认最大浏览 100 行；非生产硬上限 500、生产 100；页大小 10/25/50/100；查询超时非生产 5 秒、生产 3 秒。实施前复核基线漂移并报告，不自行放宽。
- 页窗口受用户原始 LIMIT/OFFSET 和平台预算共同约束，内部子查询窗口保留；翻页不能重获预算。每页重新执行，无快照和总数；达到预算明确提示。
- 取消传递至数据库层；仅客户端中断不得宣称远端已停。完成/取消竞争按实际结果分类，保持既有证据持久化规则。连接必须释放或可恢复，不留下无限 loading。

### 结果与导出

| 类型 | PostgreSQL 合同 |
|---|---|
| bigint/numeric | 字符串传输、数据库类型标记；不经浏览器浮点转换 |
| JSON/JSONB | 保留数据库返回文本；查看与格式化不损失内嵌大数字 |
| 数组 | 保留数据库数组文本及类型，保持元素 NULL/维度含义 |
| bytea | 十六进制文本，不按 UTF-8 强解码、不提供文件预览 |
| 时间 | 保留数据库精度；带时区明确 UTC，无时区不擅自转换 |
| SQL NULL | 界面区分空字符串与文本 NULL；CSV 沿用空字段，不承诺可逆备份 |

- 单格 8 KiB，UTF-8 边界安全、显式逐格截断标记；截断 JSON 展示为文本，不自动取全文。
- CSV 仅导出当前已加载页，不重新查询、不遍历后续页；任一单元格内容被截断则拒绝。保留既有脱敏与公式防护。
- PostgreSQL 类型合同不自动改变 MySQL/TiDB 类型返回；逐格截断与 CSV 拦截属于已接受的共用修复。
- PostgreSQL 首批完整建表/视图定义 SQL、DDL 导出后置；能力不支持必须受控呈现，其他结构信息仍可用。

### 技术门禁定稿（G1–G13）

以下每项均为已定稿的工程合同，标注「证据」的条目由隔离试验直接验证（详见文末验证记录）；标注「决定」的为基于证据的设计裁决。

#### G1 连接模型与兼容迁移

**数据模型（migration `00029_pg_query_connections.sql` 草案）。** 复用现有「目标 = 受管资源 + `query_target_credentials` 启用行」模型，不为 PostgreSQL 新建 CI：

```sql
ALTER TABLE query_target_credentials
  ADD COLUMN database_name  VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN default_schema VARCHAR(128) NOT NULL DEFAULT '';
-- UNIQUE (resource_id) -> UNIQUE (resource_id, database_name)

ALTER TABLE query_executions
  ADD COLUMN database_name       VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN schema_name         VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN backend_pid         INT NULL,
  ADD COLUMN remote_state        VARCHAR(16) NOT NULL DEFAULT '', -- ''|stopped|unknown|completed
  ADD COLUMN client_execution_id VARCHAR(64)  NULL;
-- UNIQUE (target_resource_id, client_execution_id) — NULL=该行不承载占用身份
--   （旧无 key 路径 + G9 占用前终态对），MySQL 唯一索引内多 NULL 不冲突

CREATE TABLE query_execution_claims (            -- 占用记录：非历史行、非审计事件
  target_resource_id         BIGINT UNSIGNED NOT NULL,
  client_execution_id        VARCHAR(64)     NOT NULL,
  actor_user_id              BIGINT UNSIGNED NULL,   -- 带类型主体身份（沿用 QueryExecutionActor）
  actor_machine_principal_id BIGINT UNSIGNED NULL,   -- 恰一列非空：user XOR machine，应用层保证
  database_name              VARCHAR(128)    NOT NULL DEFAULT '',
  schema_name                VARCHAR(128)    NOT NULL DEFAULT '',
  request_digest             CHAR(64)        NOT NULL,   -- 规范化请求摘要：区分同 key 不同内容
  execution_id               BIGINT UNSIGNED NULL,       -- finalize 同事务关联终态执行；NULL=未完成
  claimed_at                 DATETIME(6)     NOT NULL,
  PRIMARY KEY (target_resource_id, client_execution_id),
  KEY ix_claim_actor (actor_user_id, actor_machine_principal_id, claimed_at)
);

ALTER TABLE query_saved_statements
  ADD COLUMN database_name VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN schema_name   VARCHAR(128) NOT NULL DEFAULT '';

ALTER TABLE query_result_disclosure_policies
  ADD COLUMN schema_name VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_bin;
-- UNIQUE (target_resource_id, database_name, object_name, column_name)
--      -> (target_resource_id, database_name, schema_name, object_name, column_name)
```

- 语义：`database_name=''` 表示单连接/执行期选库（MySQL/TiDB 现状）；`database_name≠''` 表示连接固定库（PostgreSQL 必填，且必须与凭据 DSN 的 dbname 相等——见 G3）。`default_schema` 仅 PostgreSQL 使用，创建连接时必填。
- **连接身份 = 复合键 (target_resource_id, database_name)**：同一 `database_instance` 资源可有多行不同 `database_name` = 多个 Query Connection，各自独立的 `credential_ref`/`enabled`/`environment_policy`；不创建重复 CI（决定，满足议题 #103）。**不引入新代理主键**——下游 `target_resource_id` 列语义不变。
- **复合身份的接口携带方式（评审修正项 1）**：所有面向 PostgreSQL 连接的接口必须携带 `database` 以选中连接行；凭据读取接口由 `GetCredentialByResourceID(resourceID)` 扩展为 `GetCredential(ctx, resourceID, databaseName)`（MySQL 恒传 `''`，行为不变）：
  
  | 接口 | 复合身份携带 |
  |---|---|
  | `POST …/execute` | 请求体 `database`（PG 必填非空；MySQL 必须缺省/空，否则 `validation_failed`） |
  | `GET/PUT/DELETE …/credential` | `?database=`——PG 目标必填（选中连接行）；MySQL/TiDB 必须缺省/空（解析 `(R,'')`，行为不变） |
  | `GET …/schema/*` | 现有 `?database=` 参数——PG 语义为**连接选择器**（须匹配某连接行 database_name）；MySQL 维持浏览范围语义 |
  | `POST …/related-records` | `source` 增 `database`+`schema`（PG 必填） |
  | `GET /query-targets` 列表项 | 响应项增 `connections:[{database,defaultSchema,enabled,environmentPolicy}]`（PG 必填非空；MySQL 恒单元素 `database:''`）——**`GET /query-targets/{id}` 详情路由当前不存在，不新增**；前端经列表项枚举连接 |
  | `GET …/executions` 历史 | `?database=` 可选过滤 + `?clientExecutionId=` 查询（G9 取消状态读取）；记录行带 `database_name`/`schema_name` |
  | 保存语句 CRUD/执行 | `?database=` 范围 + 存储列（G12） |
  | 披露策略 CRUD | 策略体已含 `database_name`/`schema_name`（G1） |
  | 工作区 JSON | worksheet context `{targetId,database,schema}` |
  
  服务端解析顺序（外部评审修正——**仅适用于使用连接的操作**：execute、saved-statements/execute、related-records、schema 浏览、explain）：`(resource_id, request.database)` → 连接行（不存在 → `query_connection_not_found`）→ enabled/环境策略 → `credential_ref` → DSN 绑定（dbname 必须 = `database_name`，否则 `dsn_binding_mismatch`）。**不适用**：凭据 CRUD 以 `(resource,database)` 寻址连接行并沿用既有 upsert/幂等删除语义（PUT 创建/修复连接行本身，不得要求先满足执行资格；GET/DELETE 仅选中行）；历史与保存语句读取继承既有读取语义（可读已停用连接的历史），仅增加复合身份范围过滤。
- MySQL/TiDB 兼容：全部旧行回填 `''`；新唯一键 (resource,'') 与旧 (resource) 等价；披露查询对 `schema_name=''` 恒等；`database` 字段缺省解析 `(R,'')`——无行为变化、无身份变化、无回填语义（决定）。凭据端点 `?database=` 缺省同上。
- 停用按行独立：`enabled=false` 停 (resource,database) 一个连接，其余不受影响；历史/披露/保存语句保留不删。引用完整性沿用应用层（仓库约定无 FK）。
- 迁移顺序：加列 → 回填 `''` → 重建唯一键。部署窗口：迁移先行（旧代码写 `''` 默认值行为等价）；Down 在存在 `database_name≠''` 行时用守护过程拒绝（沿用 00028 模式）。唯一键重建锁表风险：两张小表，可接受。
- 验收场景：同实例 `(R,'db_a')`+`(R,'db_b')` 并存独立启停；披露键 `(R,'labdb','app','orders',…)` 与 `(R,'labdb','analytics','orders',…)` 互不碰撞（证据 1.4：同名对象跨 schema 独立解析）。

#### G2 HTTP 合同（字段级）

`POST /query-targets/{id}/execute` 请求体（严格 JSON，未知字段 400）：

```json
{
  "statement": "SELECT …",                       // required, 1..65536 chars
  "database": "labdb",                           // PG 必填：连接选择器（G1 复合身份）；MySQL 必须缺省/空
  "schema": "app",                               // optional；PG 缺省=连接 default_schema；MySQL 发送非空→validation_failed
  "maxRows": 100,                                // optional，沿用
  "pagination": { "page": 1, "pageSize": 25 },   // optional，沿用
  "clientExecutionId": "uuid-v4",                // PG 三入口必填（非空≤64，服务端强制，G9）；MySQL 可选：提供→claim 协议生效，缺省→旧路径
  "capabilities": ["cellTruncated"]              // optional：客户端声明的结果合同能力（G2 截断门）
}
```

`database` 即连接身份的一部分（不是可选浏览范围）：服务端按 `(target_resource_id, database)` 定位连接行后执行 G3 绑定校验。前端经 `GET /query-targets` 列表项的 `connections` 数组枚举连接（详情路由不存在，不新增）。

响应（**纯附加字段，无破坏变更**——评审修正项 2）：

```json
{
  "executionId": 123,
  "status": "success|rejected|failed|timeout|cancelled",   // execute 响应恒终态；executions 列表另可含派生 running（G9）
  "targetResourceId": 1,
  "engine": "postgresql",
  "columns": [{"name":"id","databaseType":"INT8","nullable":false,
               "displayMode":"raw_copy_allowed","copyAllowed":true}],
  "rows": [["9007199254740993", "…", null]],
  "cellTruncated": [[false, true, false]],
  "rowCount": 25, "truncated": false, "durationMs": 12,
  "limitApplied": 26, "executedAt": "…",
  "pagination": {"page":1,"pageSize":25,"hasPreviousPage":false,"hasNextPage":true},
  "context": {"database":"labdb","schema":"app"}
}
```

- `rows` 保持现有 `scalar[][]` 形态与类型联合（`string|number|boolean|null`）——**不破坏既有字段**；PG 单元格恒为 `string|null`（`bool→"t"/"f"`、`bigint→十进制文本`，见 G7），MySQL 维持既有映射。
- 新增 `cellTruncated?: boolean[][]`：与 `rows` 行列对齐的逐格截断标记（已接受的共用修复）；**两个引擎在修复落地后均发送**。字段缺省 = 该页无逐格截断信息。
- **旧客户端安全门（评审修正项 3）**：「字段可忽略」不等于「导出安全」——旧前端收到含截断格的页仍会按完整值导出 CSV，违反已接受的拦截规则。因此请求体 `capabilities` 声明合同能力：页面含任一截断格且客户端未声明 `cellTruncated` → **不返回 rows**，受控错误 `result_contract_upgrade_required`（fail loud，无静默降级）；无截断格的页对旧客户端完全兼容。
- **能力与终态/证据的顺序（外部评审修正）**：能力门是**交付决定**，在 finalize 之后——执行链先如实落终态对（执行成功即 `success`），再于结果信封构造处判定交付：无能力+截断页 → 响应 `result_contract_upgrade_required` 且不返回 rows。**终态描述治理执行事实，响应错误描述交付失败**——不写 `rejected`（执行确实发生），也不为交付拒绝追加第二条历史或事后改写终态。截断标志仅存于本次结果信封与交付判定——`QueryExecutionRecord` 不新增逐格矩阵持久化；按 key 读到 `success` 行返回既有元数据，缺能力仍不返回 rows（历史本就不回结果行）。**优先级**：证据持久化失败严于交付拒绝——finalize 写失败时对外报 `query_backend_error`/502，不得以能力错误掩盖证据失败。三路径同规则。
- **门位于共享结果边界，非单端点（评审修正项 4）**：判定在统一的执行结果信封构造处（`QueryExecuteResponse` 序列化前），覆盖**全部**返回受治理结果页的路径——`POST …/execute`、`POST …/saved-statements/{id}/execute`、`POST …/related-records`。`RelatedRecordNavigationResponse` 内嵌同一组结果字段（`columns`/`rows`/`truncated`），同步增 `cellTruncated` 矩阵；其请求体同样增 `capabilities`/`clientExecutionId`（它也写 `query_executions` 行、同样可被中止）。新结果端点默认继承该门。发布处理：T8 后端能力与 T13 前端消费同一发布窗口落地；旧前端在截断页上看到明确错误而非不安全导出。
- `truncated` 顶层语义不变（行/预算级截断）。
- `status` 枚举新增 `cancelled`（附加值，前端类型扩展；兼容说明见 G9）。
- `context` 回显服务端实际执行上下文；PG 恒含 `{database,schema}`。
- 新 controlled error codes：`query_connection_not_found`、`result_contract_upgrade_required`、`execution_already_exists`、`client_execution_id_conflict`、`schema_not_found`、`query_object_not_found`、`query_schema_not_usable`（pinned schema 存在但对执行账号无 USAGE，连接配置错误）、`page_out_of_range`、`unsupported_limit_offset_form`、`query_object_definition_unsupported`、`query_connection_disabled`、`dsn_binding_mismatch`、`pg_version_unsupported`（决定；HTTP 映射沿用现有 handler 表）。证据持久化失败沿用既有 `ErrQueryBackendFailure` → 502 `query_backend_error`，**不新增**专用码（外部评审修正）。

Schema 元数据端点（PG）：

| 端点 | PG 行为 |
|---|---|
| `GET …/schema/databases` | 返回连接固定库单项数组 |
| `GET …/schema/schemas`（新增） | 用户 schema 列表（排除 `pg_catalog`/`pg_toast`/`information_schema`） |
| `GET …/schema/objects?schema=` | 该 schema 下 `pg_class relkind∈{r,p,v}` 表/视图 |
| `GET …/schema/object-details?schema=&object=` | 列(`pg_attribute`+`format_type`+nullable)、索引(`pg_indexes`)、外键(`pg_constraint contype='f'`) |
| `GET …/schema/relationship-map?schema=` | 外键边（含跨 schema 引用） |
| `GET …/schema/table-definition` | `query_object_definition_unsupported`（首批不支持，与连接失败明确区分） |
| `POST …/explain` | `query_explain_not_supported`（首批不支持） |
| `POST …/related-records` | 支持；`RelatedRecordNavigationSource` 增 `database`+`schema` 字段（PG 必填） |

PG 下所有 schema 端点携带 `?database=`（G1 连接选择器，须匹配某连接行 `database_name`）；不匹配 → `query_connection_not_found`。

#### G3 凭据与 DSN 绑定（证据 7.1）

单一校验入口 `validatePGDSNBinding(dsn, host, port, database) → *pgx.ConnConfig`：**返回的解析配置即执行配置**（parse-once-use-everywhere，不再二次派生）。

- 接受形式：`postgres://`/`postgresql://` URI、libpq keyword/value（含引号/转义）。
- 原文显式必填：`user` `password` `host` `port` `dbname`——**先于** pgx 解析在原文上检查，环境/驱动默认值补全按缺失拒绝（证据：`pgx.ParseConfig` 单独会把 `PGUSER` 补成 `fan`）。
- 拒绝矩阵（全部实测）：host/port/dbname 与目标不符；缺任一必填；URI authority 或 keyword host/port 含 `,`；pgx `Fallbacks` 中出现**不同** endpoint（同 endpoint 的 TLS fallback 属 sslmode prefer 正常重试，允许）；unix socket；allowlist 外任何 key；不可解析文本。
- Key allowlist：`user password host port dbname sslmode sslcert sslkey sslrootcert sslcrl sslcrldir sslsni ssl_min/max_protocol_version connect_timeout application_name target_session_attrs keepalives* tcp_user_timeout gssencmode krbsrvname requiressl`；显式拒绝例：`options`（可改会话 GUC）、`service`/`passfile`（重引配置/秘密）。
- 存储：沿用 `credential_ref` 不透明引用 → 环境解析 DSN；DSN 不持久化、不进日志/审计/响应。

#### G4 连接生命周期与只读执行边界

每连接一个 `pgxpool.Pool`（由 G3 返回的 ConnConfig 构建；池参数为运维可调，非产品合同）。每次受治理执行的固定序列（证据 1.4/6.1/2.2）：

1. `Acquire` → `SELECT pg_backend_pid()`，记 `query_executions.backend_pid`；
2. `BEGIN READ ONLY`；
3. `SELECT set_config('search_path', $1, true)`——绑定值 = `<pinned_schema>,pg_catalog`（pg_catalog **显式排在 pinned 之后**，消除其未列名时的隐式最先解析；G10）。pinned schema 是**服务端解析后的本次执行上下文**：请求显式携带时经连接校验后使用请求值，缺省取连接 `default_schema`——执行序列/解析门/披露/digest/context 回显共用该解析结果；值经绑定注入免疫（证据：`SET LOCAL` 在 RO tx 内生效）；
4. `SET LOCAL TimeZone = 'UTC'`——timestamptz 文本恒带 `+00`；
5. `SET LOCAL statement_timeout = <预算>`（非生产 5000ms/生产 3000ms）——**必须设在解析门之前**：`statement_timeout` 是逐语句上限且覆盖锁等待，门内探针与 touch 的等待由此受 DB 侧约束（9.3 实测：并发持锁下 touch 等锁被 57014 终止）；实现按已消耗时间折算剩余预算，不为每个 touch 重新发完整预算；执行总时长由执行 ctx deadline + 既有 CancelRequest 链兜底（服务端发起，不依赖客户端存活）。**门/改写阶段一律终止整个尝试、回滚、落占用者唯一 keyed 终态对——终态按实际原因分类（G9）：57014/ctx deadline → `timeout`；客户端取消 → `cancelled`（remote_state 规则）；明确拒绝（守卫/USAGE/对象/归属/依赖闭包）→ `rejected`**；
6. **关系名解析门 + 身份钉住**（G10，tx 内）：输入为**作用域解析后的实体关系引用**（守卫收集 RangeVar 时按 WITH 位置可见性区分并记录字节偏移——非递归定义体只见先前同级+外层、自身不可见；`WITH RECURSIVE` 同级互见含自身；内层遮蔽外层；限定名恒为实体引用）：未限定名命中当前位置可见的 CTE 名 → CTE 引用（不做 catalog 核对，不参与改写），其定义内实体照常收集。（a）存在未限定实体引用时先核 `pg_catalog.has_schema_privilege(<pinned>,'USAGE')`——为假则 pinned schema 会被 search_path 静默跳过 → `query_schema_not_usable`；（b）实体引用逐一经 `to_regclass('<ns>.<name>')` 解析为 **canonical OID**（未限定名的 ns = pinned；任一不在其 schema → `query_object_not_found`）；（c）**身份钉住**：对每个实体执行 `SELECT 1 FROM "<ns>"."<name>" LIMIT 0`——touch 对解析出的对象取 ACCESS SHARE 并持有至 tx 结束，冻结 **(namespace,relname)→OID 映射**：改名/删除/重建被阻塞（先提交则 touch 得 42P01 → 受控拒绝），视图定义亦不可被 `CREATE OR REPLACE` 偷换；随后**重解析 OID 必须等于门内 OID**（捕获 touch 前已完成的替换，9.3 实测 OID 漂移可检出）；（d）**视图依赖闭包**（relkind v/m）：touch 后查 `pg_rewrite`⨝`pg_depend` 取基表 namespace 集——须 ⊆ {pinned ∪ 视图自身 schema}，越界（含 dep 在 secret/catalog）→ 受控拒绝（披露身份无法键到越界来源；9.3 实测 `payroll_leak`/`cross_analytics` 被拒、`order_summary` 放行）；
7. **canonical 限定改写（绑定构造保证，G10 机制 4）**：对每个**未限定实体** RangeVar，在其解析器给出的**字节偏移**处插入 `"<pinned>".` 前缀——只插入、不改动用户原字节（digest/审计记用户原文，改写后执行文本一并入证据）；CTE 引用与已限定名保持原样。执行文本中每个实体引用因而都是 schema 限定名：**绑定必然落在该 (namespace,relname) 上或响亮失败**（42P01/42501，9.3 实测 schema 改名→42P01、ACL 撤销→42501、表改名→42P01、view `pg_locks` 反例构造性失效）——不存在可漂移的回退位置；
8. 对改写文本再过 G6 顶层分页改写 → `PgConn().ExecParams(结果 SQL, 参数值, nil, nil, []int16{0})`——服务端绑定 + 全文本结果，读 ≤pageSize+1 行；
9. `ROLLBACK`，释放连接。

只读三层：应用守卫（G5）→ `BEGIN READ ONLY`（写 → SQLSTATE 25006）→ 账号最小权限（SELECT only，DDL/写 → 42501）（证据 2.2/2.3）。

#### G5 方言守卫、解析器与版本矩阵

- 解析器：`github.com/wasilibs/go-pgquery v0.0.0-20260721025817-45baeffb0133`（wazero 纯 Go 运行时编译的 libpg_query；提供 Parse/ParseToJSON/Deparse/Scan/Normalize/Fingerprint）+ `github.com/pganalyze/pg_query_go/v6 v6.2.2` **仅作 protobuf 类型来源**（证据：该包 `CgoFiles=0`，全 lab `CGO_ENABLED=0 go build` 通过）。部署约束不变：纯 Go，无 build tag。
- 驱动：`github.com/jackc/pgx/v5 v5.11.0`（2026-09-07 发布，当前最新）。安全下限：**GO-2026-5004 修复版为 v5.9.2**（pkg.go.dev/vuln 核实；该漏洞仅影响 simple-protocol 的客户端 `Query.Sanitize` 插值路径——本规格走 `ExecParams` 服务端绑定，不触该路径；仍以下限 + 最新双约束锁定）。
- 集成测试：`github.com/testcontainers/testcontainers-go/modules/postgres v0.44.0`。
- 守卫（证据 2.1）：解析成功 + 恰好一条语句 + 顶层 `SelectStmt` + 递归校验所有 WithClause/子查询同为 SelectStmt + 无 `IntoClause`（SELECT INTO）+ 无 `LockingClause`（FOR UPDATE/SHARE/KEY SHARE）+ FuncCall 递归拒绝清单：
  `pg_sleep, nextval, setval, currval, pg_terminate_backend, pg_cancel_backend, pg_reload_conf, pg_rotate_logfile, pg_create_restore_point, pg_switch_wal, pg_backup_start/stop, pg_promote, pg_read_file, pg_read_binary_file, pg_stat_file, pg_ls_dir, pg_ls_logdir, pg_ls_waldir, pg_logdir_ls, pg_ls_archive_statusdir, pg_ls_tmpdir, pg_advisory_lock(_shared), pg_advisory_xact_lock(_shared), pg_advisory_unlock(_all), pg_notify, setseed, pg_stat_get_backend_pid, pg_column_size`。守卫同时收集**全部** RangeVar（顶层及 CTE 体/子查询/集合分支内层），按 **WITH 位置可见性**分类为**实体引用**与 **CTE 引用**（G10 机制 1 完整规则：非递归定义体只见先前同级+外层、自身不可见；`WITH RECURSIVE` 同级互见；内层遮蔽外层；限定名恒为实体引用；**归属无法确定 → 直接受控拒绝**（守卫拒绝路径）——不得以猜测的实体身份继续执行或披露，同名实体恰好存在不构成归属证明）同时记录每个实体引用的**字节偏移**（供 G4 步骤 7 改写）后交给 G4 步骤 6 的解析门。
- 首批可执行语句：单条 SELECT（含 JOIN/子查询/只读 CTE/集合运算/窗口函数/引号标识符/`$n` 占位符/顶层 LIMIT|OFFSET|FETCH FIRST）。其余一律受控拒绝。声明：拒绝清单是**纵深一层**而非完备安全证明，真正边界由 RO tx + 最小权限账号保证（证据 2.2/2.3）。
- PG 服务器版本门禁：连接校验 `SHOW server_version_num` ∈ `[140000, 180000)` = **PG 14–17**；之外拒绝启用（决定：解析器内嵌 PG18 文法无缺口，但组件验证与集成测试仅覆盖 PG 17 服务端；PG 18 行为未认证）。
- 更新/安全策略：go-pgquery 跟随 libpg_query 上游；pgx 锁 `v5.11.0` 精确版本（≥v5.9.2 为安全下限），升级走依赖评审（govulncheck + CHANGELOG）。

#### G6 分页语义（证据 4.1，议题 #105 新语义）

- `browsable = min(用户顶层 LIMIT 或 ∞, 平台预算)`——用户窗口内可浏览总行数；`userOffset` 是用户 SQL 的起点，**不参与 browsable 扣减**。
- 页窗口（与 lab `windowForPage` 同一公式，评审修正项 3）：

  ```
  pageStart   = (page − 1) · pageSize            // 已浏览行数（用户窗口内）
  fetchOffset = userOffset + pageStart           // 用户 SQL 起点 + 已浏览行数
  fetchCount  = min(pageSize + 1, browsable − pageStart)   // 剩余>pageSize 含哨兵
  约束：page ≥ 1；pageStart < browsable（否则 page_out_of_range）；
        browsable = 0 时仅允许 page 1，执行 LIMIT 0
  ```

  验证：`LIMIT 30 OFFSET 10` 页1 → `pageStart=0` → `LIMIT min(26,30)=26 OFFSET 10`（25+哨兵）；页2 → `pageStart=25` → `LIMIT 5 OFFSET 35`。`browsable` 只减 `pageStart`——`userOffset` 已体现在 fetchOffset 中，不会被二次扣减。
- 实测：`LIMIT 3`→`LIMIT 3`；`LIMIT 30 OFFSET 10` 页1 `LIMIT 26 OFFSET 10`（25+哨兵）、页2 `LIMIT 5 OFFSET 35`；`LIMIT 200` 预算100 页4 `LIMIT 25 OFFSET 75`（无哨兵，预算边界 `hasNextPage=false`）；`LIMIT 0`→`LIMIT 0` 空页，page>1 → `page_out_of_range`；无 LIMIT 页4 ids 76..100。
- 接受：`LIMIT <非负整数字面>`、`OFFSET <非负整数字面>`、`LIMIT ALL`、`LIMIT NULL`（=无限制，browsable=预算）、`FETCH FIRST n ROWS ONLY`（deparse 规范化为 `LIMIT n`）。
- 拒绝：`LIMIT|OFFSET $n`、字符串/浮点/负数字面、表达式 `(2+3)`、`FETCH FIRST … WITH TIES`（n 不可静态定界）→ `unsupported_limit_offset_form`。
- 内层窗口（子查询/CTE/括弧集合分支）**永不重写**——只动顶层；顶层集合运算根 LIMIT 视为用户窗口（证据：`UNION ALL … LIMIT 5`→browsable=5）。
- 每页独立执行（无快照、无全 COUNT）；`browsable > 0` 且 `page > ceil(browsable/pageSize)` → `page_out_of_range`（`browsable=0` 按上条仅允许 page 1，不受本式约束）。
- MySQL/TiDB **不套用**本语义（#106 独立跟踪）；`GuardPGPaginated` 旧「替换用户 LIMIT」实现已废弃。

#### G7 结果与类型合同（证据 6.1/6.2，议题 #104）

- 执行层用 `ExecParams`/`ExecPrepared` + `resultFormats={0}`（全文本）：**数据库原生文本即 wire 值**——这是决定性机制；`database/sql` 通用扫描被证不可用作合同（pgx 把 bytea 渲染成原始字节、timestamp 伪造 `Z` 后缀、timestamptz 带会话时区）。
- 实测 wire 形态：

| 类型 | wire 值（文本） |
|---|---|
| bigint | `"9007199254740993"` |
| numeric | `"123456789.123456789123456789"` 全精度 |
| timestamptz | `"2024-01-02 03:04:05.123456+00"`（`SET LOCAL TimeZone='UTC'`） |
| timestamp | `"2024-01-02 03:04:05.123456"`（无时区后缀） |
| date / time | `"2024-01-02"` / `"03:04:05.123456"` |
| interval | `"1 day 02:03:04.5"` |
| bytea | `"\xdeadbeef"`（PG 十六进制文本原样） |
| int[] | `"{1,2,NULL,4}"`（元素 NULL 保留） |
| json/jsonb | 数据库原文，如 `{"a": 9007199254740993}` |
| boolean | `"t"/"f"` |
| uuid / float | 数据库文本 |
| SQL NULL | `null`，与 `""`、文本 `"NULL"` 三者可区分（证据） |

- `columns[].databaseType` = FieldDescription OID 经 pgx TypeMap 名（`INT8`/`NUMERIC`/`TIMESTAMPTZ`/`JSONB`/`_TEXT`/`BYTEA`…）。
- 逐格截断经 `cellTruncated` 矩阵传输（G2 附加字段）；8 KiB **rune-safe** 截断（实测中文 8190 字节切点、UTF-8 恒有效；8192 不切、8193 切）。
- 截断 JSON 按文本展示不补全；页内任一格 truncated → CSV 拒绝（实测拦截）；clean 页可导出（`encoding/csv`，引号/逗号/换行往返保真）；沿用公式防护 + NULL→空字段。

#### G8 模板编译（证据 3.4，议题 #107）

- `:name` 单次扫描器（非全局替换）：`::` 原子输出；普通/`E''`/`U&''`字符串、行注释、**嵌套块注释**、`$tag$`美元引用、双引号标识符内部不识别占位符；`[]` 内 `:` 恒为切片/下标字面（`arr[a:b]`、`arr[:x]` 保 PG 原义）；括号外 `:[a-z][a-z0-9_]*` → `$k`；原文 `$n` → 拒绝。
- 编译产物再过完整 GuardPG；声明校验：**每个声明参数必须恰好出现一次**（缺失/多余/重复出现/非法名 → 受控拒绝）。
- `LIMIT :n` 编译为 `LIMIT $k` → 执行/保存均经分页层判 `unsupported_limit_offset_form`（证据：声明校验通过但分页层拒绝）。
- 切片内参数（如 `arr[:x]`）无占位符语义 → 声明未用 → 拒绝（文档化限制）。
- 参数名 `^[a-z][a-z0-9_]*$`；类型 string/integer/decimal/boolean；值以文本经 `ExecParams` 服务端绑定（`paramOIDs` 空 → 由上下文推断；类型错误 → 受控拒绝）；值仅存在于临时 Template Value Session，不进历史/审计/持久化。

#### G9 取消机制（证据 5.1/5.2，议题 #105 取消条款）

- 链路（统一为 G4 原生执行链，外部评审修正）：浏览器 `AbortController` → fetch abort → HTTP 连接关闭 → request ctx 取消 → `PgConn().ExecParams` 所在 pgx 连接的 **ctx-cancel 监听**触发 → 驱动经**新独立连接**向服务器发 CancelRequest，同时判定本连接协议不可再同步、直接销毁，pgxpool 弃用该 conn（证据：5.3 原生链实测"conn is dead by design"；池后续 Acquire 新连接正常）。说明：此前证据 5.1/5.2 在 `pgx/stdlib`（`database/sql`）链上取得——取消语义同源（同一 pgconn ctx-watch + CancelRequest 机制），但原生链上的实证由 5.3 补齐，不再以 stdlib 路径描述生产链。
- 证据链：执行开始记 `backend_pid`（G4 步骤1）；取消后以有界（≤2s）独立只读连接查 `pg_stat_activity` 该 pid → 消失/idle ⇒ `remote_state=stopped`；仍 active/探测失败 ⇒ `remote_state=unknown`；查询已完成 ⇒ `completed`。
- 终态：`success`/`rejected`（守卫·绑定·校验）/`failed`（驱动·服务端错）/`timeout`（statement_timeout 57014 或 ctx deadline）/`cancelled`（ctx canceled + remote_state）。
- **合同变更说明（评审修正项 4）**：议题要求「取消结果准确呈现、未确认远端停止不得宣称已停」。现有四态无法区分「用户取消」与「驱动失败/超时」——若压入 `failed`+`error_code`，终态过滤、审计查询与 UI 终态文案都无法准确表达（备选被拒）。因此：
  - `status` 枚举**附加** `cancelled`（存储终态）与派生展示值 `running`/`unknown`（来自未关联终态的 claim，见下）（不改既有值语义）——对 JSON 消费者向后兼容；唯一 API 消费方是本仓前端，类型同窗口扩展（`QueryExecutionStatus`）；历史筛选/审计按新值可查。
  - `query_executions` **附加列** `remote_state`（`''|stopped|unknown|completed`）与 `backend_pid`——纯加列、无回填语义；旧代码忽略。
  - **MySQL 不新增 `cancelled`**：其取消维持现有记录方式（范围条款「既有能力不暗改」）；该值仅由 PG 路径产生。若后续产品决议要求对齐，另行立项。
- **DB 侧兜底恒设**：`SET LOCAL statement_timeout`（G4 步骤6）——客户端消失后服务端仍在预算时刻终止查询，不依赖网络取消成功（证据：paused-db 探测超时后 conn 死亡、恢复后可复用）。
- **不新增独立取消 API**（决定）：沿用 AbortSignal/HTTP 关闭路径（前端已用 AbortController 模式）；UI 按 `remote_state` 区分「已停止」与「已请求取消，远端状态未知」——未确认远端停止不得宣称已停（议题条款）。
- 完成/取消竞争以实际结果分类（证据：先完成 → 42 返回；取消先生效 → context-canceled + 后端进程消失；发出前取消 → 查询从未到达服务端）。
- 证据写入与 request ctx 分离（`context.WithoutCancel` + 有界超时）——客户端断开后终态证据仍可持久化（推断，沿用现有证据写模式）。
- **执行标识、原子占用与状态读取（评审修正项 2）**：中止的 execute 请求收不到响应，客户端拿不到 `executionId`，无从读取 `remote_state`；且仅终态写入无法防重——同 key 并发两请求可能都先执行 SQL。因此引入**独立占用记录 `query_execution_claims`**（非 `query_executions` 历史行、非审计事件——证据对模型字面不变）：
  - **`clientExecutionId` 服务端强制（外部评审修正）**：PG 三条执行入口（execute、saved-statements/execute、related-records）均要求**非空、≤64 字符**的客户端 key；缺失/空/超长 → `validation_failed`（请求形状校验，沿用既有拒绝语义：目标未解析不落执行证据）。前端约定生成 UUID v4（格式建议非强制）。MySQL/TiDB 旧调用 key 缺省 → 维持既有终态单写路径；**若提供则同协议生效**（占用/防重/状态读取一致——claim 机制引擎无关，不给 MySQL 留出"看似带 key 实则不防重"的假承诺）。**主体身份带类型**：claim 归属沿用既有 `QueryExecutionActor{Kind:user|machine}`——`actor_user_id`/`actor_machine_principal_id` 恰一列非空，不得把 machine ID 填入 user 列（`user:7`≠`machine:7`）；机器主体带 key 调用同样走占用协议，不新增机器权限或读取路由。
  - **占用时点与证据边界（外部评审修正）**：完整顺序 = 请求形状校验 → 目标/访问解析（既有 `access.Resolve`：目标存在+引擎+凭据+启用/策略+DSN 绑定）→ **`INSERT claims`** → 守卫+解析门 → 披露 preflight → 执行 SQL → finalize。占用是守卫与执行前的唯一原子门，同时也是**证据消耗 key 的分界**——各阶段归属：

    | 阶段 | 算新执行尝试 | 写证据对 | 消耗 key |
    |---|---|---|---|
    | 请求形状校验失败（含缺/非法 key）、鉴权失败、目标未解析 | 否 | 否（沿用既有：目标未解析不落执行证据） | 否 |
    | 目标已解析但访问拒绝（停用/生产未开放/凭据解析或绑定失败） | 是 | 是——既有 `reject` 路径 `rejected` 对，`client_execution_id=NULL` | **否**——未达执行门，修复后同 key 可重试 |
    | claim INSERT 非重复键持久化失败 | 是 | 是——`failed`/`query_backend_error` 终态对，key=NULL | 否 |
    | claim PK 冲突（digest 同 / 异） | **否——准入拒绝，非新尝试** | **否**——占用行本身即持久记录；再写历史会伪造成另一次执行（且撞 `(target,key)` 唯一键） | —（拒绝者非占用者） |
    | claim 成功（占用者）→ 守卫/解析门/披露拒绝、超时、取消、失败、成功 | 是 | 是——**keyed 终态对 + claim 关联**，finalize 单点 | 是——key 归属该尝试，终态后不释放 |

    规则：`query_executions.client_execution_id` 仅由**占用成功者的终态对**携带；占用前终态对恒写 NULL（该列语义 = 占用者执行身份），故 `(target,key)` 唯一索引只兜底占用者，不与占用前证据冲突，同 key 修复重试畅通。
  - **占用（执行 SQL 前，唯一原子门）**：`INSERT claims(target, key, actor(带类型：user/machine 恰一列), database, schema, request_digest, claimed_at, execution_id=NULL)`。PK `(target_resource_id, client_execution_id)` 唯一约束原子完成——**只有 INSERT 成功的一方可以执行 SQL**；不做"先查历史再插"（避免检查-写入竞争窗口）。
  - **`request_digest` 等价定义（外部评审修正）**：SHA-256 十六进制，输入为按固定顺序长度前缀编码的**执行影响字段**（默认值先解析再入摘要——schema 缺省→default_schema、page/pageSize/maxRows 缺省→解析后值）：
    - `execute`：`[database, schema, statement(原文逐字节), page, pageSize, maxRows]`；
    - `saved-statements/execute`：`[database, schema, statementId, 参数名值对(按名排序), page, pageSize, maxRows]`；
    - `related-records`：`[database, schema, source{schema,object,列组}, 关系/方向标识, 定位值(保序), page, pageSize, maxRows]`。
    **排除**：`capabilities`（交付合同非执行语义）、`clientExecutionId`（其本身即 key）。statement 原文逐字节，不做空白/大小写规范化（避免歧义）。临时参数值仅入摘要计算，仍不落库。target 已在 PK 内，不必入摘要。
  - **占用冲突的受控结果**：PK 冲突 → 读取占用行比较 `request_digest`：相同 → `execution_already_exists`（客户端转读 `?clientExecutionId=`，**不再执行 SQL**）；不同 → `client_execution_id_conflict`（key 被另一请求占用）。无论占用行处于执行中、已终态、取消后或结果未知，同 key 重试都**在执行 SQL 前**被拒绝——**终态后 key 不释放**。
  - **finalize（终态，唯一历史+审计写入点）**：沿用既有 `InsertExecutionWithAudit` 缝——`INSERT executions(终态字段, client_execution_id)` + `INSERT audit('query.executed'|'related_record_navigation')` + `UPDATE claims SET execution_id=<新执行 ID>`，三者同一事务。**claim 不删除**，终态后经 `execution_id` 关联真实执行行；历史行仍恒为终态、恒一审计事件——**与现有 Execution Evidence Pair 语义逐字一致**；`(target, client_execution_id)` 唯一索引兜底防双 finalize。
  - **孤儿规则（2026-09-26 用户已确认，非待裁决）**：超过 `statement_timeout+60s` 未关联终态的 claim 展示为"**结果未知／执行记录未完成**"——过期仅是"未观察到终态"的依据，**不得推断进程死亡、查询失败或远端已停止**；不为孤儿补造终态历史或审计事件；不自动重试 SQL；后台清扫/主动回收/保留期限**后置**（本轮不实现）；清理不得释放仍受防重承诺约束的 key。
  - **状态派生与读取合同（只读纯函数，无分类写回）**：单条一致查询 `claims LEFT JOIN executions ON executions.id=claims.execution_id`（同一快照，非两条独立查询）——`execution_id` 非空 → 返回真实终态（status/remote_state/executionId）；为空且未过期 → 派生 `running`；为空且过期 → 派生 `unknown`（结果未知）。executions 列表含未完成 claim 条目：`{clientExecutionId, executionId:null, status:running|unknown, claimedAt, actor, target, database, schema}`——**executionId 绝不伪造**，派生条目不得被称为已落库终态历史；按活动时间与真实执行混排分页；沿用既有历史授权过滤（非管理读者按**带类型主体**匹配——user 只见本 user 行、machine 只见本 principal 行，`user:7` 读不到 `machine:7` 的 claim/执行；同 key 不能读到他人执行或跨连接）。
  - **并发与失败**：同 key 并发仅一方占用执行；finalize 与读取无竞争撕裂（单一一致快照读，读见占用前态或完整终态关联）；并发读取幂等零写入；占用 INSERT 失败 → 无执行受控错误；**finalize 事务失败（含审计写失败）→ 终态历史与 claim 终态关联整笔回滚**，claim 保留为可发现的未完成占用；持久化失败的对外呈现沿用既有 `ErrQueryBackendFailure` → **502 `query_backend_error`** + 无维度计数器+固定日志（不新增错误码）；并发 finalize 不存在（占用唯一胜者）；同 key 不同内容 → `client_execution_id_conflict`。
  - `running`/`unknown` 为**派生展示值**：`query_executions.status` 存储值仍恒为终态枚举（`cancelled` 为新增存储值），派生值仅出现于 API 响应（前端类型扩展）。

#### G10 名称解析与披露一致性（证据 1.4/9.1/9.2/9.3，议题 #103；外部评审硬冲突修正）

承诺语义：**未限定实体关系名仅在 pinned schema 解析**；缺失 → 响亮失败（42P01 等效语义，受控拒绝 `query_object_not_found`），**无静默回退**。关键事实（评审修正）：PostgreSQL 将未列名于 `search_path` 的 `pg_catalog` **隐式最先搜索**，且**静默跳过**执行账号无 `USAGE` 的 schema——单独 `SET search_path='app'` 无法兑现该承诺：`SELECT count(*) FROM pg_class` 会解析到 `pg_catalog.pg_class` 而非报错；pinned 自建的 `app.pg_class` 遮蔽表也被隐式 pg_catalog 遮蔽；`search_path='nogranted,pg_catalog'` 且执行账号无 `nogranted` USAGE 时（即便持表级 SELECT），未限定 `pg_class` 仍落 `pg_catalog`（9.2 实测）；且 Read Committed 下**并发 DDL 可在检查与执行之间改名**，使已通过存在性核对的未限定名在绑定时落到 pg_catalog（9.3 实测：无锁定时 `app.pg_class` 改名后 `SELECT marker FROM pg_class` 漂到 catalog）。因此承诺由以下机制联合兑现：

1. **作用域分类（守卫侧）**：全部 RangeVar 按 **PostgreSQL 位置可见性**解析——未限定名命中**当前位置已可见**的最近一层 CTE 名 → **CTE 引用**，不占存在性核对位；否则为实体引用（限定名恒为实体引用，`schema.object` 不解析到 CTE）。位置可见性规则：
   - 非递归 `WITH` 内，**CTE 定义体只见先前同级 CTE 与可见的外层 CTE**——自身及后续同级名在定义体内尚不可见，命中它们的内层引用仍是实体引用，必须接受 pinned 存在性核对（9.2：`WITH pg_class AS (SELECT oid FROM pg_class)` 内层 `pg_class` 分类为实体）；
   - 主查询与下层子查询可见本级**全部** CTE；`WITH RECURSIVE` 同级互见（含自身——递归自引用是合法 CTE 引用），非法递归结构仍由守卫/数据库受控拒绝；
   - 内层 CTE 名遮蔽外层同名（9.2 实测外层 CTE 在子查询内可见）；CTE 遮蔽同名实体表（`WITH orders AS (SELECT -1 id)` 返回 -1 而非 `app.orders`）。
   CTE 定义内的实体引用照常收集核对；若实现无法确定某引用的作用域归属，**直接受控拒绝**——不得以猜测的实体身份继续执行或披露（同名实体恰好存在不构成归属证明；误分类为有歧义归属时继续，会使 CTE 引用被当作实体核对通过、而实际 SQL 仍取 CTE 输出，披露身份与实际来源不一致）。
2. **解析门 + 身份钉住（G4 步骤 6，执行前，tx 内）**：（a）存在未限定实体引用时先核 `pg_catalog.has_schema_privilege(<pinned>,'USAGE')`——为假则 pinned schema 被静默跳过 → `query_schema_not_usable` 受控拒绝（连接配置错误，修复连接后可重试）；（b）实体引用逐一以 `to_regclass('<ns>.<name>')` 解析为 canonical OID（未限定名 ns=pinned；限定名 ns=其显式 schema）——任一不存在 → `query_object_not_found`（包括仅在 `pg_catalog`/`information_schema` 存在的未限定名）；（c）**身份钉住**：对每个实体 `SELECT 1 FROM "<ns>"."<name>" LIMIT 0`——ACCESS SHARE 持有至 tx 结束，冻结 (namespace,relname)→OID 映射（改名/删除/重建被锁阻塞；先提交则 touch 得 42P01 受控拒绝）；touch 后重解析 OID 必须等于门内 OID（捕获 touch 前已完成的同名替换——9.3 实测 OID 变化可检出）。**pin 的准确角色**：它钉住的是**名字到对象的映射**，不是名称解析条件——`ALTER SCHEMA RENAME`/`REVOKE USAGE` 只改 `pg_namespace` 元组、不与表锁冲突，RO tx 也无法锁 catalog（FOR SHARE/LOCK TABLE → 42501），RR 快照同样不冻结 search_path 解析（9.3 全部实测证伪）；这些向量由机制 4 的限定改写结构性闭合；（d）**视图依赖闭包**：实体为视图/物化视图时（relkind v/m），touch 后查 `pg_rewrite`⨝`pg_depend` 取其基表 namespace 集——必须 ⊆ {pinned ∪ 视图自身 schema}；越界 → 受控拒绝（输出列的实际来源脱离治理面，披露身份无法键到它；9.3 实测 `secret`/`analytics` dep 均拒、同 schema 视图放行）。touch 先行使 `CREATE OR REPLACE VIEW` 无法在本 tx 内偷换定义（需 ACCESS EXCLUSIVE，被 AS 锁阻塞）。
3. **search_path 显式排序（G4 步骤 3）**：绑定值 `<pinned_schema>,pg_catalog`——pg_catalog 显式排在 pinned 之后。机制 4 之后它不再承载关系名绑定安全（实体引用已全部限定、CTE 引用不经 search_path），保留为函数/操作符等非关系名的解析语境与纵深一层。
4. **canonical 限定改写（G4 步骤 7，绑定构造保证）**：对每个未限定实体 RangeVar，在其字节偏移处插入 `"<pinned>".` 前缀——**只插入不改写用户原字节**（digest 与审计记用户原文，改写后执行文本入证据）。执行文本中每个实体引用都是 schema 限定名 → 绑定必然落在机制 2 钉住的 (namespace,relname)→OID 上，或响亮失败（42P01/3F000/42501）——**schema 改名、ACL 撤销、表改名、同名替换对绑定安全全部归约为受控失败，无静默回退**（9.3 实测全族）。被否决的前任机制记录在案：`pg_locks` 执行前后锁集差分**不是**绑定证明——观测基线自身锁 `pg_locks`（f1：`app.pg_locks` 漂移绑 catalog 视图时该 OID 已在基线、差集为空、错误结果将交付）、合法执行会锁索引等依赖（f2：显式 `analytics.orders` 锁 `orders_pkey` 被误拒）——锁集只回答"持有哪些锁"，不回答"哪个引用绑到哪个对象"（9.3 f 实测证伪）。由此「分类+USAGE+OID 解析+钉住映射+限定改写+排序」联合兑现承诺语义：结果要么来自钉住对象、要么响亮失败，**无静默错误披露**。

- **适用范围**：承诺仅覆盖**关系引用**（表/视图/物化视图/分区表 = RangeVar；CTE 引用按作用域规则另行处理）。函数名解析保持 PG 原生语义（含隐式 pg_catalog 回退）——函数由守卫按名拒绝清单约束（G5），披露不治理函数；`FROM` 中的集返回函数（RangeFunction，如 `generate_series`）同此规则。
- **显式限定名不变**：`schema.object` 直达目标、按实际对象校验（实测 220/40 行各归各 schema）；`pg_catalog.x`/`information_schema.x` 显式查询允许，canonical 身份照常进披露匹配。
- `pg_temp` 隐式最优先规则在 RO tx 内不可利用（CREATE TEMP → 25006，证据 2.2）。
- **披露匹配**以守卫+解析门得到的 canonical `(database,schema,object,column)` 为准（未限定实体名 = pinned schema；限定名 = 其显式 schema）——执行与脱敏同源；**CTE 引用的披露身份沿其定义中的实体引用追溯**（同既有披露对子查询的溯源处理），不得把 CTE 名补成 `pinned_schema.name` 充当实体身份；`query_result_disclosure_policies` 键含 `schema_name`（G1）。
- 上下文恢复校验 = `information_schema.schemata` 存在性探针（一次查询，恢复时执行）。
- 元数据列表排除 `pg_catalog`/`pg_toast`/`information_schema`（浏览面不变）。

#### G11 元数据合同

- `databases` → 固定库单项；`schemas` → `pg_namespace` 用户 schema；`objects` → `relkind∈{r,p,v}`；`object-details` → `pg_attribute`+`format_type`+`pg_indexes`+`pg_constraint contype='f'`；`relationship-map` → FK 边（schema→schema）。
- 定义 SQL（建表/视图定义/DDL 导出）首批不支持：`query_object_definition_unsupported`，与连接失败明确区分；其他结构信息正常（议题 #107）。

#### G12 保存/恢复/历史上下文

- `query_executions`/`query_saved_statements` 增 `database_name`/`schema_name`（G1）；worksheet JSON context 增 `{database,schema}`。
- `POST …/saved-statements/{id}/execute` 请求体同样增 `capabilities`/`clientExecutionId`——与 `POST …/execute` 共享：截断安全门（G2 共享结果边界）、原子占用与 `?clientExecutionId=` 状态读取（G9）。两条执行路径行为完全一致，由共享信封与共享执行服务保证。
- 恢复三重校验：连接存在且 enabled + 凭据绑定仍通过 + schema 存在 → 任一失败受控报错或要求重选，**不静默切换**。
- 历史按 `(target,actor)` 不变；PG 行携带 database/schema 用于展示与恢复。
- 上下文切换丢弃旧结果/元数据（沿用前端 generation guard 模式）。

#### G13 部署与发布门禁

- PostgreSQL 目标在治理链未全通前一律不可执行：连接校验/守卫/绑定任一未就绪 → `query_target_not_ready` 或拒绝（执行门关闭）。
- 本轮不改正式运行链、不开放 readiness、不执行生产迁移、不提交代码。
- 发布前置：迁移合入 → 后端单测/集成（testcontainers PG HTTP 治理链）→ 前端 lint/build/unit → 浏览器 E2E → MySQL/TiDB 回归全绿 → 逐格截断共用修复回归确认。



## Testing Decisions

用户于 2026-09-23 明确接受：以现有后端 HTTP 接口加真实临时 PostgreSQL 为主验收边界；复用现有鉴权/披露/证据集成测试模式，浏览器自动化覆盖首批用户流程，少量单元测试覆盖解析、类型与窗口边界。该确认不代表测试已经执行，也不代表其余技术待定项已获接受。

| 场景 | 可观察通过条件 |
|---|---|
| 身份隔离 | 同实例不同 database、同库不同 schema 的同名表，浏览/补全/执行/脱敏均不串用；恢复缺失上下文明确失败 |
| 权限与绑定 | 未登录、停用、生产未开放、数据库绑定错误均拒绝；正确配置通过；秘密不出现在响应和证据 |
| 原始窗口 | 预算100/页25：LIMIT3仅3行；LIMIT30 OFFSET10为25+5行；LIMIT200最多100；LIMIT0为空；内层窗口不变 |
| 语句边界 | 选中/当前语句包含中文、emoji、注释、美元引用、::时正确；多语句仍受服务端拒绝规则约束 |
| 参数模板 | 正确值绑定；缺失/额外/错误类型拒绝；重复出现的命名参数按 G8 声明校验受控拒绝；保存恢复不保留值 |
| 取消与超时 | 完成先、执行中取消、发出前取消分别断言结果；执行中先观察数据库 active 再取消；核对数据库停止证据和连接恢复 |
| 证据生命周期 | 并发同 key 仅一方执行 SQL（执行次数=1）；**已终态/取消后/finalize 失败后同 key 重试执行次数仍=1**；占用后进程退出且零轮询→claim 存留、无未配对历史行、读取派生 `unknown`（不伪造终态历史/审计）；两并发读取派生一致且零写入；finalize 与读取仅见未完成占用或真实终态关联（无撕裂/无伪造 ID）；审计写失败→终态历史+claim 关联整笔回滚、持久化失败计数+1；未授权主体按 key 查询返回空（含 user/machine 跨类型：user:7 读不到 machine:7）；机器主体带 key 调用走同一 claim 协议且归属 machine 列；**边界（G9 表）**：占用前终态对（访问拒绝/claim 持久化失败）写 NULL key 且不阻塞同 key 重试；重复 key 拒绝零证据写；缺失/非法 `clientExecutionId` 在目标解析前 `validation_failed` 拒绝 |
| 名称解析门 | 未限定实体名不在 pinned schema → `query_object_not_found`（含仅在 pg_catalog 存在的名，无隐式回退）；pinned/pg_catalog 同名遮蔽对象由 pinned 胜出；**CTE 引用按位置可见性分类**（定义体内自身不可见、RECURSIVE 同级互见、嵌套遮蔽、CTE 遮蔽同名实体表）；**归属无法确定 → 受控拒绝**（同名实体存在不构成归属证明）；pinned schema 无 USAGE → `query_schema_not_usable`；**未限定实体引用经 canonical 限定改写执行——并发表改名 → touch/执行 42P01、schema 改名 → 42P01/3F000、ACL 撤销 → 42501，全部受控失败无 catalog 回退**；touch 后 OID 重解析捕获同名替换；**视图依赖闭包 ⊆ {pinned∪视图 schema}，越界拒绝**；门/钉住锁等待受 `statement_timeout` 约束（门前已设，超时终态=timeout）；显式 `pg_catalog.x` 允许且披露身份匹配；跨 schema 显式查询的索引等合法依赖不误拒；CTE 输出列披露身份沿定义实体追溯 |
| 类型保真 | 超安全整数、小数、内嵌大数字、数组 NULL、二进制与微秒时间跨响应/UI/复制验证 |
| 截断与 CSV | 8192/8193 字节和中文边界；截断值明确标记且导出拒绝，完整值可导出；引号/逗号/换行保真 |
| 治理闭环 | 成功、拒绝、失败、超时、取消均走现有披露/历史/审计规则；不能拿孤立组件输出替代 HTTP 集成证据 |
| 回归 | MySQL/TiDB 既有行为不回退；共用截断修复单列差异；浏览器上下文切换时旧响应不覆盖新工作表 |

检查必须能因业务规则失效而失败。跳过记未验证，不能算全绿；生产数据不用于测试。最终门禁沿用仓库当前单测、集成、OpenAPI、前端类型检查与浏览器验收命令，在实施票中核对准确命令。

## Out of Scope

其他引擎实现、写入/DDL、数据库账号管理、个人连接、逐用户/团队授权、SSH/Kubernetes 隧道、多语句批执行、复杂补全、分屏、交互式结果排序过滤、图形化 EXPLAIN/ER、Notebook、AI、全量导出和数据库备份恢复。

MySQL/TiDB 保留用户窗口的修正由独立议题跟踪，不阻塞 PostgreSQL 首次发布。

## Further Notes

### 设计验证记录

**环境**：macOS 本地，Go 1.26.2；`pgx/v5 v5.11.0`、`wasilibs/go-pgquery v0.0.0-20260721025817-45baeffb0133`、`pg_query_go/v6 v6.2.2`（仅类型）；Docker `postgres:17` 隔离容器（`chub-pg-lab`，`labdb`/`labdb_other`，`ro_user` 只读账号，schema `app`/`analytics`/`secret`，合成数据含同名 `orders` 及 `app.pg_class` 遮蔽表）。运行命令：`cd advisor-plans/023-postgresql-feasibility/lab && ./run.sh`。**结果：25/25 PASS，0 PARTIAL**（2026-09-26 实测；MySQL `chub-mysql-lab` 承载 8.x 协议检查）。

**事实（试验或源码直接证据）**

- `SET LOCAL search_path`/`set_config('search_path',$1,true)`/`SET LOCAL TimeZone`/`statement_timeout` 均在 `BEGIN READ ONLY` 内生效；显式限定名恒达目标；RO tx 内不可建临时表（pg_temp 无影）（1.4/2.2）。**范围修正（外部评审两轮）**：早期"未限定名缺失报 42P01"的实测只覆盖了**各处皆不存在**的名；仅在 `pg_catalog` 存在的未限定名（如 `pg_class`）在 `search_path='app'` 下会解析到 pg_catalog（隐式最先），pinned 与 pg_catalog 同名对象亦被遮蔽（9.1）；执行账号无 pinned schema `USAGE` 时该 schema 被静默跳过——存在性核对与排序均无法约束执行绑定（9.2）；CTE 引用遮蔽同名实体表、不占存在性核对位（9.2）；**检查与绑定之间并发改名可使未限定名漂移**——无锁定时 `app.pg_class` 改名后 `SELECT marker FROM pg_class` 落到 catalog（9.3）；**表级 ACCESS SHARE pin 无法阻止 schema 级变更**（`ALTER SCHEMA RENAME`/`REVOKE USAGE` 只改 `pg_namespace` 元组——表 pin 下 schema 改名仍使未限定绑定漂移；RO tx 内 `FOR SHARE`/`LOCK TABLE` catalog 均 42501；REPEATABLE READ 快照也不冻结 search_path 命名空间解析——9.3 全部实测）；**`pg_locks` 锁集差分也不是绑定证明**——观测基线自身锁住 `pg_catalog.pg_locks`，漂移绑定命中已在基线的 OID 时差集为空（9.3f1 实测：错误结果将交付）；合法执行锁索引依赖被差分规则误拒（9.3f2 实测 `analytics.orders_pkey`）。最终机制（位置可见性分类+USAGE 核对+`to_regclass` OID 解析+实体 pin 冻结名→OID 映射+**canonical 限定改写**——未限定实体 RangeVar 字节偏移处插入 `"<pinned>".`，执行文本中实体引用全部 schema 限定，绑定由构造保证或响亮失败+排序兜底）由 9.1/9.2/9.3 覆盖；视图依赖闭包越界拒绝同测。
- 分页新语义全部实测（4.1）：用户窗口保真、哨兵、预算边界、LIMIT 0、内层不动、参数化/非整数窗口拒绝、UNION 根窗口、deparse 不改对象名（3.5）。
- `pgconn.ExecParams(resultFormats={0})` 返回数据库原生文本：bytea→`\x`十六进制、timestamptz 在 UTC 会话下带 `+00`、timestamp 无后缀、数组 `{1,2,NULL,4}`、jsonb 原文、NULL 与 `''` 可区分；同调用服务端绑定参数（6.1）。`database/sql` 通用扫描被证不可用作合同（bytea 出原始字节、timestamp 伪造 Z、timestamptz 带会话时区）。
- 取消三方向实测（5.1/5.2 在 `pgx/stdlib` 链、5.3 在原生 `pgxpool`+`PgConn().ExecParams` 链）：ctx 取消 → pgconn ctx-watch → 独立连接发 CancelRequest → 后端进程消失；本连接销毁不可复用、池恢复正常；完成/取消竞争按实际分类；取消前从未到达服务端；statement_timeout 为 DB 侧兜底。
- DSN 绑定全矩阵实测（7.1）：URI/keyword/IPv6/引号值接受；全部缺失/错配/多主机/socket/非 allowlist key/不可解析拒绝；`pgx.ParseConfig` 环境补全被独立拒绝；同 endpoint TLS fallback 与多主机正确区分；返回 config 实际连接成功且 `current_database()` 一致。
- 模板编译全边界实测（3.4）：字符串/E''/嵌套块注释/美元引用/双引号标识符/`::`/数组切片原义保留；`:name`→`$k`；`$n` 字面拒绝；重复名、缺/多参数拒绝；`LIMIT :n` 编译后被分页层拒绝；切片内 `:x` 保字面。
- UTF-16 光标：编辑器 UTF-16 偏移经转换函数映射到解析器字节偏移；中文/emoji 语句提取正确；语句间空白 → 无语句（3.2）。
- `pg_query_go` 导入包 `CgoFiles=0`、全 lab `CGO_ENABLED=0 go build` 通过 → 无 cgo 部署约束。
- 依赖版本（官方包页 2026-09-23 核实）：pgx 最新 v5.11.0（2026-09-07）；GO-2026-5004 修复版为 v5.9.2（仅影响 simple-protocol `Query.Sanitize` 插值）；pg_query_go 最新 v6.2.2（2026-01-28）；go-pgquery 最新为 2026 伪版本；testcontainers postgres 模块最新 v0.44.0（2026-08-07）。
- 后端治理链源码事实：`query_target_credentials` UNIQUE(resource_id) 1:1、凭据接口 `GetCredentialByResourceID` 仅按 resource 查询（→ 复合身份需扩展为 (resource,database)）；DSN 经 credential_ref 不持久化；披露键 (target,db,obj,col) 无 schema；execute 请求无 database/clientExecutionId/capabilities 字段；schema 端点已有 `?database=` 参数；**`GET /query-targets/{id}` 详情路由不存在**（只有 list）；`GET/PUT/DELETE …/credential` 无 `?database=`；executions 仅 list+statement 端点；无取消 API；`query_executions` 无 database/schema/backend_pid/remote_state/client_execution_id 列；worksheet 为 JSON blob；执行响应 `rows` 为 `scalar[][]`。

**推断/设计决定（基于证据的裁决，非直接验证）**

- 连接身份选复合键 `(target_resource_id, database_name)` 而非新代理主键/新表（blast radius 最小；唯一键等价旧语义）；各接口以 `database` 字段/参数携带复合身份（G1 表）。
- 逐格截断经附加字段 `cellTruncated` 矩阵传输而非 `rows[].cells` 破坏式形态（纯附加，零兼容破坏；首轮评审后由双发/切换方案改为矩阵）。
- `ExecParams`+`SET LOCAL TimeZone='UTC'` 作为 wire 机制（对比 NullString/简单协议后选定）。
- PG 版本门禁 [140000,180000)；拒绝清单为纵深一层而非完备证明。
- 不新增取消 API；`cancelled`+`remote_state` 持久化终态。
- 执行占用选独立 `query_execution_claims` 表而非历史行 `running` 态——历史行保持「终态+单一审计事件」字面语义；claim 终态后保留并经 `execution_id` 关联（防重 key 不释放）；`running`/`unknown` 为读取派生展示值，无存储非终态、无后台清扫。
- 占用时点定于**目标/访问解析之后、守卫与执行之前**（外部评审修正）：占用前终态对写 NULL key（不消耗 key，修复后可重试）；重复 key 拒绝属准入拒绝不写证据对；`request_digest` 覆盖三入口全部执行影响字段（排除 `capabilities`/key 本身）；PG 三入口服务端强制非空 key。
- 名称解析承诺由「WITH 位置可见性分类（CTE 引用不占核对位，定义体内自身/前向同级仍为实体引用，**归属不确定即拒**）+ pinned `USAGE` 核对 + `to_regclass` canonical OID 解析 + 实体 pin touch（ACCESS SHARE 冻结 (ns,name)→OID 映射并阻断视图重定义，锁等待受 `statement_timeout` 约束）+ touch 后 OID 重解析 + 视图依赖闭包核对 + **`canonical 限定改写`（未限定实体 RangeVar 偏移处插入 `"<pinned>".`，绑定由构造保证——schema 改名/ACL 撤销/表改名全部归约为受控失败）** + `search_path='<pinned>,pg_catalog'` 排序」联合兑现（外部评审五轮修正：`pg_locks` 锁集差分被实测证伪——基线自污染漏检 + 索引依赖误拒，锁集没有引用归属信息，不能承担绑定证明；digest/审计记用户原文、改写执行文本入证据）。
- `pgxpool` 每连接一池（池参数运维可调）。
- `RelKind∈{r,p,v}` 计为可浏览对象。

**未验证/边界声明（不记 PASS）**

- 后端 HTTP 治理链端到端：未实施——组件实验证据不能替代（议题 #108 主验收边界待实施票完成）。
- 取消的 `remote_state` 探测在生产网络分区/高延迟下行为：仅 paused-container 单点模拟。
- PG 14/15/16 服务器行为与解析器覆盖：仅 PG17 容器实测；矩阵其余版本未跑。
- `pgxpool` 并发/池参数、`pg_stat_activity` 权限要求（普通只读账号查他者 backend 是否受限——待实施票核实）。
- 前端所有交互、worksheet 持久化迁移、CSV 端到端。
- saved-statement 恢复流、disclosure canonical 匹配的完整实现正确性。
- 性能/负载、长查询内存边界、8KiB 截断在大结果集下的成本。

### 实施任务拆分草案（T1–T14）

仅为草案，不直接建实施票；执行门保持关闭至 G13 发布门禁全部通过（含 T12 治理链集成全绿）。

| # | 范围 | 依赖 | 验收 | 回归要求 | 涉及面 |
|---|---|---|---|---|---|
| T1 | 共享身份/模型合同落地：migration 00029（G1 列+唯一键+`query_execution_claims` 表含 request_digest/execution_id 关联）+ model 常量 + OpenAPI 字段 | — | 迁移 Up/Down 守护实测；旧 MySQL 行行为不变 | `make test`；迁移集成测试 | 后端+迁移 |
| T2 | 凭据元数据 + 复合键读取接口 `GetCredential(resource,database)` + `validatePGDSNBinding` 移植（G3）+ credential seed 校验扩展（database/default_schema） | T1 | 绑定全矩阵单测过；env 补全拒绝；(R,'') 旧行读取不变 | 现有 credential 测试不变红 | 后端 |
| T3 | PostgreSQL 连接工厂：ConnConfig→pgxpool；`SHOW server_version_num` 门禁 [14,18) | T2 | 版本外拒绝实测；连接成功路径通 | — | 后端 |
| T4 | go-pgquery 守卫（G5 规则+拒绝清单+RangeVar **位置可见性分类**：实体引用 vs CTE 引用，**含字节偏移**）+ canonical 限定改写器（实体引用偏移插入 `"<pinned>".`，只插入不改字节）+ 单测矩阵 | T1 | 守卫矩阵用例全过（含 CTE/集合/锁定/INTO/函数）+ 分类覆盖：CTE 不误拒、CTE 遮蔽同名实体、**定义体内自身/前向同级为实体引用**、RECURSIVE 同级互见、嵌套遮蔽、**归属无法确定 → 受控拒绝**（非实体假设放行）+ **改写正确性**：实体引用偏移处插入 schema 前缀、CTE/已限定引用原样、嵌套/子查询位置正确、改写后语句等价可执行 | vitess 路径不受影响 | 后端+依赖 |
| T5 | 分页层 `paginatePG` 移植（G6 新语义）+ 窗口形态单测 | T4 | 4.1 全部用例在后端单测重现 | MySQL 分页不动 | 后端 |
| T6 | schema 元数据服务（G11：`/schemas` 新端点+objects/details/fk schema 参数） | T3 | testcontainers PG 元数据用例过；`table-definition`→unsupported code | MySQL schema 端点回归 | 后端+OpenAPI |
| T7 | 执行器（G4 序列含 `statement_timeout` 前置、解析门、**实体 pin touch + OID 重解析 + 视图依赖闭包**、**canonical 限定改写**）+ `ExecParams` 文本 wire + 类型映射（G7） | T3,T4,T5 | 隔离 PG 执行实测：类型表全对、UTC、bytea hex、NULL 区分 + **改写后执行对漂移构造免疫：门后 schema 改名→42P01/3F000、ACL 撤销→42501、表改名→42P01、同名替换由 OID 重解析检出——全部受控失败不交付**（9.3 场景重现）+ 跨 schema 显式查询（含索引依赖）不误拒 + DDL 持锁下门内语句等待被 statement_timeout 终止 | — | 后端 |
| T8 | 逐格截断 + `cellTruncated` 矩阵（双引擎，含 RelatedRecordNavigationResponse）+ `capabilities` 声明与 `result_contract_upgrade_required` 门（共享结果边界，**post-finalize 交付决定**）+ CSV 门（G2/G7） | T7 | 8192/8193、rune 边界、CSV 拒绝/往返用例过；**execute、saved-statements/execute、related-records 三路径**：未声明能力+截断页→受控错误不返回 rows **且历史如实记 `success`**（非 rejected、无第二条历史）；能力错误不得掩盖证据持久化失败（502 优先） | MySQL 响应回归（字段附加不破坏） | 后端+OpenAPI+前端 |
| T9 | 披露投影 schema 维度 + canonical 匹配（G10，含 CTE 输出列沿定义实体溯源、**视图依赖闭包越界拒绝不进入披露路径**）+ 策略 CRUD/迁移 | T1,T6,T7 | 同库两 schema 同名表各自策略命中/封堵；CTE 列溯源到定义实体而非 CTE 名；**归属无法确认的引用被拒绝而非按同名实体披露**；dep 越界视图被拒不披露 | 旧策略 `schema=''` 回归 | 后端 |
| T10 | 取消与终态证据（G9）：backend_pid、remote_state 探测、cancelled 存储态、`query_execution_claims` 执行前原子占用（终态后保留+execution_id 关联+request_digest 冲突）+`?clientExecutionId=` 过滤+一致视图派生 running/unknown、evidence 分离 ctx、CONTEXT.md 领域术语同步；**证据边界表全行落地**（占用前终态 NULL-key 对、claim 持久化失败 NULL-key failed 对、dup 拒绝零写入、缺 key 校验拒绝） | T7 | 三方向集成用例 + paused/不可达用例 + 中止后轮询读到终态行 + **并发同 key 仅一方执行 SQL** + **已终态后同 key 重试仍不执行** + 占用后进程退出且零轮询→claim 存留派生 unknown + 并发读取派生一致零写入 + finalize 写失败→整笔回滚 claim 可发现 + 未授权 key 查询为空 + **访问拒绝 pair 不消耗 key（同 key 修复后可执行）** + **重复 key 拒绝无新增执行行** + 机器主体 keyed claim 归属 machine 列、跨类型不可读 + 取消链在**原生 pgxpool/PgConn** 路径实测（非 stdlib） + 解析门含 pinned `USAGE` 核对 + **门/钉住阶段终态按实际原因分类：57014/deadline → `timeout`、ctx 取消 → `cancelled`、明确拒绝 → `rejected`**（statement_timeout 已在门前设置，均由占用者唯一 keyed finalize） | 既有终态分类回归 | 后端 |
| T11 | 模板编译器移植（G8）+ 声明校验 + 保存语句 database/schema 持久化与恢复校验（G12） | T4,T5 | 3.4 用例重现；恢复三重校验拒绝路径过 | MySQL 模板回归 | 后端 |
| T12 | 后端 HTTP 治理链集成测试（testcontainers PG）：鉴权→目标→凭据→守卫→执行→披露→审计→结果序列化 | T1–T11 | #108 主验收边界全绿（成功/拒绝/失败/超时/取消/截断/CSV 拒绝） | 现有集成套件全绿 | 后端测试 |
| T13 | 前端首批：连接枚举（列表项 connections[]）、执行请求 `database`+`schema`+`clientExecutionId`+`capabilities`（execute/保存语句执行/关联记录三处）、schema 浏览器、`cellTruncated` 消费、`cancelled`/`running`/`unknown` 态+中止后轮询、worksheet context | T8 合同,T12 | lint/build/unit 过；schema 浏览与执行走通；中止后状态经 clientExecutionId 读到 | MySQL 工作表回归 | 前端 |
| T14 | 浏览器 E2E（议题清单：身份隔离/绑定拒绝/窗口/模板/取消/类型/截断 CSV/恢复失效） | T13 | e2e 全绿；MySQL e2e 不回退 | release-e2e 全量 | 前端+后端 |

### 决策来源

- [决策地图](https://github.com/Fanduzi/ControlHub-Backend/issues/98)
- [首批范围](https://github.com/Fanduzi/ControlHub-Backend/issues/99#issuecomment-5762782782)
- [结果边界](https://github.com/Fanduzi/ControlHub-Backend/issues/100#issuecomment-5762837731)
- [接入权限](https://github.com/Fanduzi/ControlHub-Backend/issues/101#issuecomment-5762967192)
- [连接与命名空间](https://github.com/Fanduzi/ControlHub-Backend/issues/103#issuecomment-5776867523)
- [结果类型](https://github.com/Fanduzi/ControlHub-Backend/issues/104#issuecomment-5776931861)
- [分页语义](https://github.com/Fanduzi/ControlHub-Backend/issues/105#issuecomment-5777185197)
- [模板及定义范围](https://github.com/Fanduzi/ControlHub-Backend/issues/107#issuecomment-5778471472)
- [独立 MySQL/TiDB 修正](https://github.com/Fanduzi/ControlHub-Backend/issues/106)

本地组件试验（`advisor-plans/023-postgresql-feasibility/lab`，gitignore 忽略目录）已按本规格语义扩展并 25/25 通过，覆盖新分页语义、schema 一致性、名称解析门（9.1 遮蔽/泄漏 + 9.2 CTE 位置可见性/USAGE 跳失/不确定归属拒绝 + 9.3 全类检查-绑定漂移、pin 边界、RR 证伪、`pg_locks` 审计证伪与 canonical 限定改写闭环）、DSN 绑定、UTF-16 光标、模板编译、类型 wire、取消证据（5.1/5.2 stdlib 链 + 5.3 原生链）、claim 协议与证据边界（8.x，真实 MySQL scratch 库，含带类型主体）。组件证据不等于后端 HTTP 治理链验证——远程实施者必须取得可复现资产并按 T12 完成集成验收，不能仅依据本文件宣称端到端证据。
