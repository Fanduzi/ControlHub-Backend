# BIGINT 自增主键重设计方案

**日期:** 2026-04-22
**状态:** Approved
**审核:** User approved design direction; final implementation must pass parallel multi-role agent review

---

## 背景

当前后端所有核心表都使用 `CHAR(36)` UUID 风格字符串作为主键，并在写入路径通过 MySQL `UUID()` 生成新 ID。这个方案可以工作，但对 InnoDB 不友好：

1. InnoDB 聚簇索引按主键组织数据，字符串主键会放大页占用与二级索引体积
2. 所有二级索引叶子节点都携带主键值，`CHAR(36)` 会把每个二级索引都变胖
3. 当前写入路径没有保证严格递增或严格右扩展，不适合作为长期主键策略
4. API、OpenAPI、前端、测试目前都默认 ID 是字符串 UUID，越晚改成本越高
5. 项目明确不接受数据库级 `FOREIGN KEY`，引用完整性必须由应用层处理

本次重设计的目标是一次性把数据库、后端、OpenAPI、前端、测试统一到 **`BIGINT UNSIGNED AUTO_INCREMENT` 单列代理主键** 模型，消除 UUID 主键技术债。

---

## 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| 主键策略 | 单列 `BIGINT UNSIGNED AUTO_INCREMENT` | 符合 InnoDB 最常见最稳妥的聚簇索引实践 |
| 对外 ID 形式 | 直接暴露数字 ID | 与数据库/Go/API/前端统一，不保留双轨兼容 |
| URL 参数 | 全部改数字 ID | 避免路径层继续沿用字符串语义 |
| 外键约束 | 完全禁用 | 避免 `FOREIGN KEY` 带来的死锁与演进耦合 |
| 引用完整性 | 应用层校验 | 由 service/repository 保证存在性与删除顺序 |
| profile 表主键 | 独立 `id` 主键 + `resource_id` 唯一索引 | 遵循单表单列代理主键规范，保留一资源一 profile 约束 |
| JSON 列 | 保留现状 | 本次聚焦主键与关系建模，不扩大为整体数据模型重写 |
| 迁移策略 | 删库重建 | 不保留 UUID 兼容层，不做在线双写迁移 |
| 持久层方案 | 继续 `database/sql` + repository | 本次不引入 ORM，避免扩大风险面 |
| Seed 策略 | 业务键回查真实 ID | 避免依赖固定自增值，保证重建稳定 |
| 最终验证 | 自动化验证 + 并行多角色 agent review | 不仅验证能跑，还要验证契约、UX、证据与边界 |

---

## 一、数据模型与迁移边界

### 1.1 全量主键数字化

以下表全部统一为：

```sql
`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'Primary key ID',
PRIMARY KEY (`id`)
```

涉及表：

- `roles`
- `users`
- `environments`
- `owners`
- `resources`
- `resource_relations`
- `resource_profiles_host`
- `resource_profiles_database_instance`
- `resource_profiles_database_cluster`
- `resource_profiles_service`
- `audit_events`

### 1.2 禁用数据库外键

本次 schema **不允许出现任何**：

- `FOREIGN KEY`
- `REFERENCES`
- `ON DELETE CASCADE`
- `ON UPDATE CASCADE`

所有关联仅以普通列存在，并通过普通索引支持查询。

### 1.3 逻辑关联列统一为 `BIGINT UNSIGNED`

包括但不限于：

- `users.role_id`
- `resources.environment_id`
- `resources.owner_id`
- `resource_relations.from_resource_id`
- `resource_relations.to_resource_id`
- `resource_profiles_*.resource_id`
- `audit_events.actor_user_id`
- `audit_events.target_resource_id`

这些列全部改为：

```sql
BIGINT UNSIGNED NOT NULL
```

对于可选关联（如 `target_resource_id`），使用：

```sql
BIGINT UNSIGNED DEFAULT NULL
```

### 1.4 profile 表结构修正

profile 表不再以 `resource_id` 直接作为主键，而改为：

- 独立 `id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY`
- `resource_id BIGINT UNSIGNED NOT NULL`
- `UNIQUE KEY uniq_resource_id (resource_id)`

原因：

1. 遵循“每表单列自增主键”规范
2. 保持 profile 表未来扩展空间
3. 继续确保“一条资源最多一个 profile”

### 1.5 关系表约束保留方式

`resource_relations` 仍保留：

- 自增主键 `id`
- 业务唯一约束：`(from_resource_id, to_resource_id, relation_type)`

不使用复合主键。

### 1.6 JSON 列作为受控例外保留

以下 JSON 列继续保留：

- `resources.labels`
- `resource_profiles_*.spec`（若保留）

原因：

- 这些字段已经深度进入当前代码与接口
- 本次目标是主键/关联/ID 契约重构，不是移除半结构化数据
- 只要不扩大 JSON 使用面，这种保留是可接受的受控例外

### 1.7 DDL 统一标准

所有新 DDL 统一要求：

- `ENGINE=InnoDB`
- `DEFAULT CHARSET=utf8mb4`
- 表级 `COMMENT`
- 每个业务列都有 `COMMENT`
- 审计列明确：
  - `created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`
  - `updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP`
- 索引命名统一：
  - 普通索引：`idx_*`
  - 唯一索引：`uniq_*`

### 1.8 迁移边界

本次不做在线迁移，不保留兼容逻辑：

- 不保留 UUID 列
- 不保留 UUID → BIGINT 映射表
- 不保留字符串 ID 兼容解析
- 不做双写
- 直接依赖删库重建

---

## 二、后端代码结构改造

### 2.1 `model` 层统一改为 `uint64`

所有领域实体、DTO、拓扑节点、关系、审计、鉴权、设置项中的 ID 字段统一改为 `uint64`。

目标：

- 代码内部只有一套 ID 语义
- 不再混用 `string` 与 `uint64`
- 不保留临时桥接字段

### 2.2 `repository/mysql` 层取消 UUID 生成

当前的：

```go
SELECT UUID()
```

全部删除，改为：

1. `INSERT` 写入不再显式写主键
2. 使用 `ExecContext()` 返回结果
3. 调用 `LastInsertId()` 获取新主键
4. 转成 `uint64` 返回上层

同时：

- 所有 `Scan()` 目标类型改为整数 ID
- 所有 where/in 参数改为整数
- profile / relation / audit 等写路径统一改造

### 2.3 `service` 层收口引用完整性

因为没有 FK，以下约束由 service/repository 显式保证：

- 创建资源时 `environment_id` / `owner_id` 必须存在
- 创建用户时 `role_id` 必须存在
- 创建关系时两端资源必须存在且未归档（按当前业务约束）
- 写 profile 时资源必须存在且类型匹配
- 删除/归档时，若有依赖关系，需要显式处理或拒绝

### 2.4 `api` 层统一解析正整数 ID

所有 handler 中的 path/query/body ID 字段改为正整数语义：

- `/resources/{id}`
- `/resources/{id}/profile`
- `/resources/{id}/relations`
- `/topology/{id}`
- `/audit?...targetResourceId=`
- request body 中的 `environmentId`、`ownerId`、`toResourceId` 等

规则：

- 非数字或非正整数 → 400
- 不再接受 UUID 字符串
- 错误消息明确说明字段应为正整数 ID

### 2.5 无 ORM 约束

本次不引入 ORM，不重写为 GORM / Ent / Bun / XORM。

保留：

- `database/sql`
- repository pattern
- 显式 SQL

原因：

- 当前任务本质是 schema 与 ID 契约重构
- 引入 ORM 会把问题扩大成持久层范式迁移
- 现有代码已经是清晰的 repository + raw SQL 模式

---

## 三、Migration 与 Seed 方案

### 3.1 `0001_initial_schema.sql` 重写

重写目标：

- 所有表切换到 `BIGINT UNSIGNED AUTO_INCREMENT`
- 无 FK
- 保留必要业务唯一索引
- 为所有逻辑关联列补二级索引
- 保留 JSON 列
- 补全 table/column comments

### 3.2 `0002_seed_reference_data.sql` 重写

不再显式插入固定 ID，如：

- `00000000-...`
- `10000000-...`
- `30000000-...`

新 seed 原则：

1. 先插入 `roles` / `environments` / `owners`
2. 再按稳定业务键查询真实 ID：
   - `roles.name`
   - `environments.slug`
   - `owners.email`
3. 用查回来的 ID 插入 `users`

### 3.3 `0004_seed_demo_data.sql` 重写

demo 资源与关系数据不再依赖固定 UUID。

新策略：

1. 插入资源时以稳定业务键识别资源，例如 `resources.name`
2. 通过子查询或分步插入回查真实 `resource.id`
3. 再插入 profile、relation、audit 数据

原则：

- 允许 seed 按业务键引用，不允许依赖固定自增值
- 确保删库重建后结果稳定
- 不假设某张表第一条一定是 `id=1`

---

## 四、OpenAPI、前端与测试联动

### 4.1 OpenAPI 全量数字化

`internal/openapi/openapi.yaml` 中所有 ID 字段统一改为数字语义：

- schema `type: integer`
- `format: int64`
- example 改为数字
- path/query/body/response 中的 ID 保持一致

不再出现 UUID 示例值作为主键。

### 4.2 前端同步改造

前端需要同步修改的内容包括：

- 所有 `id: string` / `resourceId: string` / `environmentId: string` 类型声明
- 请求参数与响应解析
- 路由参数传递
- 表单提交
- 关系创建/删除调用
- 拓扑页与详情页跳转
- 缓存 key / query key 中对 ID 的假设
- 测试 fixture

前端这次不应保留字符串 ID 兼容层。

### 4.3 Handler / Service / Integration Tests 全量数字化

所有测试中的：

- UUID fixture
- `res-1` / `env-prod` 这类临时字符串 ID
- 假仓储中的 string ID map

都需要重写为数字 ID 世界。

---

## 五、实施顺序与风险控制

### 5.1 推荐顺序

1. **先改 migration + seed**
   - 让数据库事实先变正确
2. **再改 model + repository**
   - 把持久层和领域层切到数字 ID
3. **再改 service + api**
   - 收口业务约束与参数解析
4. **再改 OpenAPI + 测试 + 前端**
   - 保证契约与实现一致
5. **最后做自动化验证 + agent review**

### 5.2 风险控制原则

- 不做字符串/数字双轨兼容
- 不引入 ORM
- 不在本次顺手重构 JSON 模型
- 不保留旧 UUID migration 逻辑
- 所有存在性约束必须显式在应用层体现

---

## 六、验收标准

### 6.1 数据库验收

- 所有业务表主键均为 `BIGINT UNSIGNED AUTO_INCREMENT`
- schema 中不出现任何 `FOREIGN KEY` / `REFERENCES`
- 所有关联列存在必要普通索引或唯一索引
- `make migrate-reset-dev` 能成功重建数据库并完成 seed

### 6.2 后端验收

- 代码中不再生成或依赖 UUID 主键
- 所有模型与仓储接口使用 `uint64`
- 所有新增写入使用 `LastInsertId()` 回填主键
- 非法 ID 输入统一返回 400

### 6.3 契约验收

- OpenAPI 中所有主键/关联 ID 字段都是数字语义
- 示例值、请求、响应、路径参数、查询参数一致
- `make openapi-validate` 通过

### 6.4 前端验收

- 前端类型、页面参数、请求、状态管理不再假设 UUID 字符串
- 资源详情、编辑、关系操作、拓扑页都能使用数字 ID 正常工作

### 6.5 自动化测试验收

必须通过：

- `make test`
- `make test-integration`
- `make openapi-validate`
- 前端类型检查/单测（如存在）
- **E2E 测试**

E2E 至少覆盖：

1. 创建资源
2. 打开资源详情
3. 编辑资源
4. 新增关系
5. 打开拓扑/关联视图

E2E 必须验证数字 ID 路径与真实前后端联动，而非只验证静态页面。

### 6.6 最终并行多角色 Agent Review

实现完成后，不直接宣布完成，必须再执行并行多角色 review。

**前端 review agents：**

- `Frontend Developer`
- `UX Architect`
- `UX Researcher`
- `UI Designer`
- `Evidence Collector`
- `API Tester`

**后端 review agents：**

至少包括：

- `Backend Architect`
- `Code Reviewer`
- `Security Engineer`
- `API Tester`
- `Database Optimizer`（如 SQL/index 设计需要进一步确认）

review 目标：

- 类型与契约一致性
- SQL/索引/仓储实现正确性
- API 边界条件与错误处理
- 页面行为与 UX 回归
- 真实证据（截图/接口结果）
- 安全性与维护性

只有在：

1. 自动化验证通过
2. E2E 通过
3. 并行 agent review 的关键问题已处理

之后，才视为本次主键重构完成。

---

## 七、非目标

以下内容不在本次范围内：

- 引入 ORM
- 在线无损迁移 UUID 旧数据
- 保留 UUID 兼容 API
- 去除现有 JSON 列
- 顺手重构业务领域模型
- 通过数据库 FK 实现级联删除/存在性校验

---

## 结论

本次改造不是简单的 DDL 调整，而是一次 **数据库主键模型、应用层 ID 类型、API 契约、前端类型、测试体系** 的统一纠偏。

设计的核心原则只有四条：

1. **主键统一为 `BIGINT UNSIGNED AUTO_INCREMENT`**
2. **绝不使用 `FOREIGN KEY`**
3. **数据库、Go、API、前端、测试只保留一套数字 ID 语义**
4. **通过自动化验证 + E2E + 并行 agent review 收尾**

只要严格执行这四条，这次重构就能一次把后续最难改的主键问题解决掉。