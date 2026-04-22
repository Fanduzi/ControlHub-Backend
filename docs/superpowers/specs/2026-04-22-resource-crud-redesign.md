# 资源 CRUD 重设计方案

**日期:** 2026-04-22
**状态:** Approved
**审核:** Frontend Developer, UX Architect, UX Researcher, UI Designer

---

## 背景

当前资源创建/编辑功能存在以下问题：

1. 创建表单所有资源类型共用一套固定字段，缺少类型特有字段（Host 缺 hostname/IP，数据库实例缺 port/engine）
2. 资源子类型是自由文本，无校验无约束
3. 编辑表单 environment/owner 下拉框默认显示 UUID（异步数据加载时序问题）
4. name 和 resourceType 创建后完全不可编辑
5. Profile 数据（hostname, IP, port, engine 等）只读不可编辑，无写入 API
6. 无法通过表单完成完整的资源录入

## 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| 子类型策略 | 严格字典（前后端校验） | 避免拼写错误、保证数据质量 |
| Profile 字段 | 创建时填写 + 编辑时修改 | 一步到位完成资源录入 |
| name 可编辑性 | 允许修改（格式+唯一性校验） | UUID 是真正标识符 |
| resourceType 可编辑性 | 不可变 | 改了导致 profile 孤儿数据 + 子类型失效 + 拓扑错乱 |
| 表单布局 | 动态表单（选类型后字段变化） | 不同类型字段不同，无需分步向导 |
| 编辑保存方式 | 单按钮并行保存 | 双按钮增加认知负担和遗漏风险 |
| 创建后行为 | Toast + "继续创建"/"查看详情" | 支持批量录入 |
| 关系管理 | 只在详情页操作 | 创建表单保持简洁 |

---

## 一、后端：子类型字典

### 新端点

`GET /resource-subtypes?resourceType={type}`

返回指定资源类型的合法子类型列表。

### 子类型定义

在 `internal/model/taxonomy.go` 新增 `ResourceSubtypeDictionary(resourceType string)` 函数：

| 资源类型 | 合法子类型 |
|----------|-----------|
| `database_instance` | mysql, postgresql, redis, clickhouse, mongodb, tidb |
| `database_cluster` | mysql, postgresql, redis, clickhouse, mongodb, tidb |
| `database_proxy` | proxysql, chproxy, haproxy, maxscale |
| `host` | vm, physical, container |
| `service` | api, web, job, cron |
| `domain_name` | — (无子类型) |
| `virtual_ip` | — (无子类型) |
| `control_plane_component` | orchestrator, ha_monitor, backup_manager |

### 校验规则

- `validateResourceCreateInput` 和 `validateResourcePatchRequest` 都调用 `ResourceSubtype.Validate(resourceType)`
- 如果该资源类型定义了子类型列表，则 subtype 必须在列表内
- 如果该资源类型无子类型定义，subtype 字段被忽略（不存储）

### 响应格式

```json
{
  "resourceType": "database_instance",
  "subtypes": [
    { "key": "mysql", "label": "MySQL" },
    { "key": "postgresql", "label": "PostgreSQL" },
    { "key": "redis", "label": "Redis" },
    { "key": "clickhouse", "label": "ClickHouse" },
    { "key": "mongodb", "label": "MongoDB" },
    { "key": "tidb", "label": "TiDB" }
  ]
}
```

---

## 二、后端：Profile 写入 API

### 新端点

1. **`PUT /resources/{id}/profile`** — 全量替换 profile（用于创建时一次性写入）
2. **`PATCH /resources/{id}/profile`** — 合并更新 profile 字段（用于编辑时部分修改）

### Profile Schema（按资源类型校验）

| 资源类型 | 字段 | 类型 |
|----------|------|------|
| `host` | hostname (string), ipAddress (string), osName (string) |
| `database_instance` | engine (string), version (string), host (string), port (int), role (string) |
| `database_cluster` | engine (string), topologyMode (string), primaryEndpoint (string) |
| `service` | systemName (string), repositoryUrl (string), runtimeEnv (string) |
| 其他 4 种 | 无 profile 表，不接受 profile 字段 |

### 校验规则

- Resource 必须存在且未归档
- `resourceType` 决定接受哪些字段，未知字段返回 400
- `PUT` 空体清除所有 profile 字段
- `PATCH` 空体为 no-op（返回 200）
- Port 范围 1-65535

### 创建时内嵌 Profile

`POST /resources` 的请求体新增可选 `profile` 字段：

```json
{
  "resourceType": "host",
  "name": "db-prod-01",
  "displayName": "DB Prod 01",
  "resourceSubtype": "vm",
  "profile": {
    "hostname": "db-prod-01",
    "ipAddress": "10.0.1.5",
    "osName": "Ubuntu 22.04"
  },
  "environmentId": "...",
  "ownerId": "...",
  "lifecycleStatus": "provisioning",
  "healthStatus": "unknown",
  "source": "manual"
}
```

服务层在同一个事务中：创建资源 → 写入 profile。

### 服务层

新增 `ProfileService`，方法：
- `PutProfile(ctx, resourceID, resourceType, fields) error` — 全量替换
- `PatchProfile(ctx, resourceID, resourceType, fields) error` — 合并更新

Repository 在现有 `ResourceRepository` 上扩展，使用 `INSERT ... ON DUPLICATE KEY UPDATE`（profile 表以 resource_id 为主键）。

---

## 三、后端：name 可编辑

### 变更

`PATCH /resources/{id}` 允许修改 `name` 字段。

### 校验

- 格式：`^[a-z0-9][a-z0-9._-]*$`（与创建时一致）
- 唯一性：同环境内 name 不可重复（已有 `name_per_environment` 唯一索引）
- 资源不可归档

### 错误响应

```json
{
  "error": "name already exists in this environment",
  "field": "name"
}
```

### resourceType 保持不可变

原因：profile 表按类型分表，改类型导致 profile 孤儿数据；子类型依赖类型校验；拓扑角色判定依赖类型。

---

## 四、后端：结构化错误响应

### 当前问题

`api-client.ts` 把后端错误响应简化为 `new Error("Request failed: 400")`，丢弃了字段级错误信息。

### 改造

后端验证错误返回 JSON：

```json
{
  "error": "validation failed",
  "details": {
    "name": "name already exists in this environment",
    "port": "must be between 1 and 65535"
  }
}
```

前端 `api-client` 解析此结构，抛出自定义错误类型 `ApiValidationError`，包含 `details` 字段。动态表单将 `details` 映射到 `react-hook-form` 的 `setError`。

---

## 五、前端：创建资源表单

### 整体结构

Sheet 侧面板，内含三个 DetailPanel 区域：

```
┌──────────────────────────────────────────┐
│ SheetHeader: 创建资源                     │
├──────────────────────────────────────────┤
│                                          │
│  ┌─ A区: 基本信息 ───────────────────┐   │
│  │  资源类型     子类型              │   │  (并排)
│  │  name         displayName         │   │  (并排)
│  └───────────────────────────────────┘   │
│                                          │
│  ┌─ B区: 运行画像 ───────────────────┐   │
│  │  (动态字段，见下方)                │   │
│  │  无 profile 时显示空状态提示       │   │
│  └───────────────────────────────────┘   │
│                                          │
│  ┌─ C区: 环境与属性 ─────────────────┐   │
│  │  environment   owner              │   │  (并排)
│  │  lifecycle     health             │   │  (并排)
│  │  externalId                        │   │
│  │  labels                            │   │
│  └───────────────────────────────────┘   │
│                                          │
│  SheetFooter: [取消] [创建资源]          │
└──────────────────────────────────────────┘
```

### A区：基本信息

| 字段 | 类型 | 说明 |
|------|------|------|
| 资源类型 | Select (必填) | 选后触发 B 区 + 子类型刷新 |
| 资源子类型 | Select (条件必填) | 选项根据资源类型加载；无子类型时隐藏 |
| name | Input (必填) | 格式校验 `^[a-z0-9][a-z0-9._-]*$`，实时提示格式要求 |
| displayName | Input (必填) | 人类可读名称 |
| source | Input (只读) | 默认 "manual"，灰色背景 |

### B区：运行画像（动态）

始终渲染 DetailPanel 容器。无 profile 字段时显示 "此资源类型暂无运行画像字段"。

字段注册表 (`profile-field-registry.ts`)：

| 资源类型 | 字段 | 输入类型 | 校验 |
|----------|------|---------|------|
| host | hostname | text | 必填 |
| host | ipAddress | text | 必填 |
| host | osName | text | 可选 |
| database_instance | engine | text (从子类型联动) | 必填 |
| database_instance | version | text | 可选 |
| database_instance | host | text | 可选 |
| database_instance | port | number | 1-65535 |
| database_instance | role | select (primary/replica) | 可选 |
| database_cluster | engine | text | 必填 |
| database_cluster | topologyMode | select (single-primary/multi-primary) | 可选 |
| database_cluster | primaryEndpoint | text | 可选 |
| service | systemName | text | 可选 |
| service | repositoryUrl | text | 可选 |
| service | runtimeEnv | text | 可选 |

**子类型联动：** 选了 database_instance + 子类型 mysql 后，engine 字段自动填入 mysql，用户可手动改。

**类型切换行为：** resourceType 变化时，整个 B 区 profile 表单组件以 `key={resourceType}` 强制重挂载，所有 profile 字段自动清空。如果用户已填写 profile 数据后切换类型，显示琥珀色警告："切换资源类型将清除运行画像数据"。

### C区：环境与属性

| 字段 | 类型 | 说明 |
|------|------|------|
| environment | Select (必填) | 显示名称，存 ID。**数据加载前显示 Skeleton** |
| owner | Select (必填) | 显示名称，存 ID。**数据加载前显示 Skeleton** |
| lifecycleStatus | Select (必填) | 默认 provisioning |
| healthStatus | Select (必填) | 默认 unknown |
| externalId | Input (可选) | |
| labels | LabelsEditor | 键值编辑器 |

**localStorage 记忆：** 创建成功后记住 environmentId 和 ownerId，下次打开创建表单时自动填入。

### 提交流程

1. 前端校验（react-hook-form + zod）
2. 一个 `POST /resources` 请求，body 含基础字段 + `profile` 对象
3. 成功 → Toast "资源已创建" + 两个按钮："继续创建"（重置表单）/ "查看详情"（跳转 `/resources/{id}`）
4. 失败 → 显示字段级错误

### 技术实现

- 使用 `react-hook-form` + `zod`（已安装但未使用）
- 拆成两个 form context：基础表单 + profile 表单（`key={resourceType}` 隔离）
- 子类型下拉用 AbortController 防竞态
- Profile 字段注册表 (`profile-field-registry.ts`) 集中管理字段定义 + zod schema

---

## 六、前端：编辑资源表单

### 可编辑字段

- name（格式校验 + 唯一性校验，字段下方提示："标识符用于系统引用，修改后相关集成可能需要更新"）
- displayName
- resourceSubtype（下拉，受 resourceType 约束）
- environment / owner（**条件渲染**：数据加载前显示 Skeleton，加载完成后渲染 Select）
- lifecycleStatus / healthStatus
- externalId
- labels

### 只读显示字段

- resourceType（友好名称，灰色禁用样式）
- UUID（可复制）
- source（"manual"，灰色背景）

### Profile 编辑

- 打开编辑 Sheet 时调用 `GET /resources/{id}/profile` 获取**原始类型数据**（`Record<string, ResourceProfileValue>`），不用归一化后的 string 版本
- 按资源类型显示 profile 字段（和创建表单 B 区相同，复用 ProfileFieldRegistry）
- 提交时调 `PATCH /resources/{id}/profile`

### 保存方式

**单按钮并行保存：**

```
点击 "保存" → Promise.allSettled([
    PATCH /resources/{id} (基础信息),
    PATCH /resources/{id}/profile (运行画像)
])
→ 任一失败显示具体错误，已成功的部分不回滚
```

如果只有基础信息变更，跳过 profile 调用。如果只有 profile 变更，跳过基础信息调用。**dirty 检测决定是否发起对应请求。**

### 表单布局

与创建表单相同的 A/B/C 三区结构。SheetFooter 固定底部，一个主"保存"按钮。dirty 状态驱动按钮启用/禁用。

---

## 七、前后端 API 变更汇总

### 后端新增

| 端点 | 方法 | 说明 |
|------|------|------|
| `/resource-subtypes` | GET | 子类型字典（?resourceType=xxx） |
| `/resources/{id}/profile` | PUT | 全量替换 profile |
| `/resources/{id}/profile` | PATCH | 合并更新 profile |

### 后端修改

| 端点 | 变更 |
|------|------|
| `POST /resources` | 请求体新增可选 `profile` 字段 |
| `PATCH /resources/{id}` | 允许修改 `name`；校验 `resourceSubtype` |
| 错误响应 | 验证错误返回 `{ error, details: { field: message } }` |

### 前端修改

| 文件 | 变更 |
|------|------|
| `components/resources/create-resource-sheet.tsx` | 重写为动态表单（A/B/C 三区） |
| `components/resources/edit-resource-sheet.tsx` | 加入 profile 编辑、name 可编辑、UUID 修复 |
| `services/resources.ts` | 新增 createResource (含 profile)、updateProfile、listSubtypes |
| `services/api-client.ts` | 改造错误处理，返回结构化验证错误 |
| `types/resource.ts` | CreateResourceInput 新增 profile 字段 |
| `lib/profile-field-registry.ts` | 新建 — profile 字段注册表 |
| `messages/en.json` | 新增 profile 字段 label、子类型名称、提示文案 |
| `messages/zh-CN.json` | 同上 |

---

## 八、不在本次范围内的功能

以下功能记录在案，但不在本次实现：

- 批量创建（表格模式或 CSV 导入）
- 基于拓扑模板的集群创建向导
- 资源克隆/复制
- 关系类型的上下文过滤（根据资源类型组合过滤可选关系类型）
- "替换关系"一步操作
- 批量状态更新
