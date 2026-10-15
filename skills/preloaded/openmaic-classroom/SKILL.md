---
name: openmaic-classroom
description: 将现有课程、视频、知识库内容、RAG 检索结果、文档块或知识图谱概念转换为 OpenMAIC 互动课程。当用户要求复习、温习、巩固学习、查漏补缺或重新学习已有课程/视频/知识库内容，或要求把这些材料转换为教学课件、互动课堂时使用此技能。支持 WeKnora 巩固学习入口、纯需求生成、基于 PDF 内容的课程生成和基于概念图遍历的批量课堂生成。
---

# OpenMAIC Classroom Generator

将 WeKnora 知识库中的 RAG 检索结果或文档内容转换为 OpenMAIC 互动课程。

## WeKnora「巩固知识」入口（优先分支）

请求包含后台登记的 `request_id` 时，执行此分支后结束，跳过下方通用生成流程。

1. 在工具列表中找到名称以 `openmaic_start_learning_course` 结尾的已部署 MCP 工具。不可用时明确告知课程入口未配置，结束任务。
2. 调用一次，参数仅为 `{"request_id":"逐字复制请求中的任务 ID"}`。后台已经使用当前登录学员权限读取课程资料、原始 PDF（若来源是 PDF）、有效教学偏好和课程要求；不要重新编造输入、邮箱或身份。
3. 工具返回成功后，告知「课程正在后台生成，链接准备好后会自动显示」。后台持续查询生成任务，不依赖本次对话继续运行。
4. 工具返回失败时，告知任务未能提交；保留稳定错误码供排查。同一任务不重新创建远程课程；需要重新发起时由用户再次点击入口。

页面只读取后台任务结果，不解析 AI 文本或结果标记。发布的链接必须来自真实成功任务；生成中展示进度。
学习完成需要 OpenMAIC 开放逐学员授权的整课记录契约。目前托管服务尚未验证此能力，页面显示「学习状态暂不可同步」，不能承诺自动完成回传。
工具与任务契约见 [API 参考](references/api-reference.md#weknora-巩固知识任务契约)。

## 核心能力

1. **RAG → 课程**: 将知识检索结果提炼为教学需求（requirement），通过 OpenMAIC API 生成互动课程
2. **PDF → 课程**: 解析用户上传的 PDF，结合内容生成课程
3. **文档块 → 课程集**: 将多个文档块/知识片段组织为多阶段课程集
4. **概念图遍历 → 批量微课堂**: 遍历知识图谱中所有 concept 页面，每个 concept 生成一个 micro-classroom

## 能力边界

> **通过 WeKnora 注册的 mcp_api_requester MCP 工具，你可以直接调用 OpenMAIC API**（HTTP POST/GET 请求）。
> 该工具在 WeKnora agent 中注册为 `mcp_{service_name}_{tool_name}` 格式（如 `mcp_mcp_api_requester_make_request`）。
> **必须**使用该 MCP 工具调用 API。如果 MCP 工具未配置或不可用，引导用户先部署 mcp-api-requester，不提供 curl 命令。

> **⚠️ MCP 可用性检查（每次读取此文件后必须执行）：**
> 在开始工作前，检查可用的 MCP 工具列表中是否存在名称包含 `mcp_api_requester` 的工具。
> - **如果未找到**：必须提醒用户："未检测到 `mcp_api_requester` MCP 服务。请先从 https://github.com/yryuu/mcp-api-requester 下载代码并部署，然后在 WeKnora 中注册该 MCP 服务。"
> - **如果已找到**：继续后续流程，使用该 MCP 工具调用 API。

## 模式选择

OpenMAIC 有两种使用模式，**根据用户场景选择**：

| 模式 | Base URL | 认证方式 | 适用场景 |
|------|----------|----------|----------|
| 托管模式（推荐快速使用） | `https://open.maic.chat` | `Authorization: Bearer <access-code>` | 用户有 open.maic.chat 访问码，无需本地部署 |
| 本地模式 | 用户提供（见本地模式 Base URL 处理） | 无认证（本地自部署） | 用户自行部署了 OpenMAIC 实例 |

**判断规则**：
- 用户提到"在线服务"、"open.maic.chat"、"访问码" → 使用托管模式
- 用户提到"本地部署"、"自建" → 使用本地模式
- 用户未明确说明时，优先询问用户使用哪个模式

**本地模式 Base URL 处理**：
1. 用户选择本地模式后，必须询问用户："请输入你的 OpenMAIC 本地部署地址（例如 `http://localhost:3000` 或 `http://192.168.1.100:3000`）"
2. 收到用户提供的地址后，进行如下处理：
   - 将地址中的 `127.0.0.1` 替换为 `host.docker.internal`
   - 将地址中的 `localhost` 替换为 `host.docker.internal`
   - 其他地址保持不变

> ⚠️ WeKnora 运行在 Docker 容器内，`localhost` 和 `127.0.0.1` 指向容器自身，无法访问宿主机服务。必须使用 `host.docker.internal` 作为容器访问宿主机的桥接地址。

## 前置条件

| 配置项 | 说明 |
|--------|------|
| 模式 | 托管模式 或 本地模式（见上方判断规则） |
| `accessCode` | 托管模式必需——访问码（以 `sk-` 开头），由用户在 open.maic.chat 获取 |
| 健康检查 | 调用前验证服务可用：`GET <BASE_URL>/api/health` |

## 使用场景

当用户请求涉及以下内容时，使用此技能：
- "把这个文档做成课件"
- "基于检索结果生成课程"
- "为这个知识点创建互动课堂"
- "将知识库内容转换为教学材料"
- "批量生成课程" / "把知识图谱的概念都做成课堂" / "基于概念图生成微课堂"

## 工作流程

### Phase 1: 确认输入源

确认课程生成的输入来源（四选一）：

1. **纯需求生成**: 用户直接描述教学主题，无需额外文档
   → 直接使用用户描述作为 `requirement`，**无需调用脚本**
2. **RAG 检索结果**: 先通过 `knowledge_search` 检索相关知识，再将结果组织为 requirement
   → 使用 `scripts/rag-to-requirement.py` 脚本转换检索结果为结构化 requirement（见 Phase 1.1）
3. **PDF 文件**: 用户提供 PDF 文件路径，先解析再调用生成 API
   → 提取 PDF 文本后构建 requirement，**无需调用脚本**
4. **概念图遍历批量生成**: 遍历知识图谱中所有 concept 页面，每个 concept 生成一个 micro-classroom
   → 使用 `scripts/concept-to-requirement.py` 脚本转换 concept + 关联 entity 为结构化 requirement（见 Phase 1.2）

### Phase 1.1: RAG 结果 → Requirement 转换（仅适用于场景 2）

当场景 2 有 RAG 检索结果时，调用 `scripts/rag-to-requirement.py` 将 chunks 转换为 requirement：

```
execute_skill_script(
  skill_name: "openmaic-classroom",
  script_path: "scripts/rag-to-requirement.py",
  input: '{"chunks": [...检索结果...], "query": "用户查询", "audience": "目标受众"}'
)
```

**input 参数格式（JSON 字符串，必须通过 `input` 参数传入，不可用 `--file`）：**
- `chunks`（必填）: RAG 检索结果数组，每项包含 `document_name`、`content`、`metadata`
- `query`（可选）: 用户原始查询
- `audience`（可选）: 目标受众描述，默认"相关领域的学习者"
- `depth`（可选）: 教学深度 `beginner|intermediate|advanced`，默认 `intermediate`
- `language`（可选）: `zh-CN|en-US`，默认 `zh-CN`
- `focus_areas`（可选）: 重点领域数组

**注意：**
- 必须将 chunks 数据作为 `input` 参数传入（等价于 `echo '{"chunks":...}' | python script.py`）
- **不要**在没有任何参数的情况下调用此脚本，否则会报错退出
- 如果脚本执行失败，可直接根据检索结果手动构建 requirement

### Phase 1.2: Concept Graph → Requirement 转换（仅适用于场景 4）

当场景 4 需要基于知识图谱概念批量生成课堂时，执行以下步骤：

**步骤 1：列出所有 concept 页面**

使用 `wiki_search` 工具搜索所有 concept 类型的页面：

```
wiki_search("^concept/", limit=50)
```

如果 concept 数量超过 50 个，多次调用翻页直到获取全部。

**步骤 2：对每个 concept 获取详情和关联 entity**

对每个 concept 页面：

1. 调用 `wiki_read_page([concept_slug])` 获取页面详情（含 OutLinks 和 InLinks）
2. 从 OutLinks 和 InLinks 中筛选出 `entity/*` 开头的 slug
3. 确定每个 entity 的 link_type：
   - 同时出现在 OutLinks 和 InLinks 中 → `bidirectional`
   - 仅出现在 OutLinks 中 → `outlink`
   - 仅出现在 InLinks 中 → `inlink`
4. 调用 `wiki_read_page([entity_slugs])` 批量读取关联 entity（只取 title + summary，不取完整 content）

**步骤 3：转换为 requirement**

对每个 concept，调用 `scripts/concept-to-requirement.py` 将 concept + 关联 entity 转换为 requirement：

```
execute_skill_script(
  skill_name: "openmaic-classroom",
  script_path: "scripts/concept-to-requirement.py",
  input: '{"concept": {"slug": "...", "title": "...", "summary": "...", "content": "..."}, "entities": [{"slug": "...", "title": "...", "summary": "...", "link_type": "..."}], "language": "zh-CN", "depth": "intermediate"}'
)
```

**input 参数格式（JSON 字符串，必须通过 `input` 参数传入）：**
- `concept`（必填）: concept 页面对象，包含 `slug`、`title`、`summary`、`content`
- `entities`（可选）: 关联 entity 数组，每项包含 `slug`、`title`、`summary`、`link_type`
- `language`（可选）: `zh-CN|en-US`，默认 `zh-CN`
- `depth`（可选）: `beginner|intermediate|advanced`，默认 `intermediate`
- `audience`（可选）: 目标受众描述，默认"相关领域的学习者"

**步骤 4：顺序调用 OpenMAIC API**

对每个 concept 的 requirement，**顺序**调用 OpenMAIC 生成 API（concurrency=1）：

- 每个 concept → 一个 micro-classroom
- requirement 中标注 `micro-classroom`
- 不可并行，避免配额冲突

**步骤 5：生成 manifest（可恢复性）**

生成 manifest JSON，记录每个 concept 的生成状态：

```json
{
  "kb_id": "...",
  "total_concepts": 10,
  "generated": ["concept/rag", "concept/llm"],
  "failed": [{"slug": "concept/embedding", "error": "..."}],
  "pending": ["concept/vector-db"]
}
```

失败时从断点继续：跳过 `generated` 中的 concept，从 `pending` 的第一个开始。

**关键约束**：
- wiki 读取和脚本转换允许 batching
- OpenMAIC API 生成 concurrency=1
- concept.Summary 作为 requirement 核心锚定
- entity 只取 title + summary，不取完整 content
- 无关联 entity 的 concept 仍可生成课堂（缺少实践环节）

### Phase 2: 构建 Generation Request

OpenMAIC 当前服务端生成接口只接受以下字段。不要发送旧版的 `pdfContent`、`language`、`enableWebSearch`、`enableImageGeneration`、`enableVideoGeneration`、`enableTTS` 或 `agentMode`；服务端会拒绝 `pdfContent`，其他未知字段也不能作为功能开关使用。

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `requirement` | string | 是 | 教学主题、目标受众、案例和互动要求 |
| `materialIds` | string[] | 否 | `POST /api/materials` 返回的材料 ID，最多 5 个 |

场景适配：
- **场景 1（纯需求）**：只发送 `requirement`
- **场景 2（RAG 结果）**：把检索结果整理进 `requirement`，必要时只上传有明确来源的材料
- **场景 3（PDF）**：先以原始 PDF 请求 `POST /api/materials`，等待材料抽取状态为 `ready`，再把返回的 `materialId` 放入 `materialIds`；不要发送 `pdfContent`
- **场景 4（概念图遍历）**：每个 concept 单独生成一个 `requirement`，顺序调用接口

### Phase 3: 调用 OpenMAIC API

**优先方式**：通过 WeKnora 注册的 MCP 工具直接调用 API。

**第一步：识别 HTTP 请求工具**
- 在你可用的 MCP 工具中，找到用于 HTTP 请求的工具
- 工具名称格式为 `mcp_{service_name}_{tool_name}`（如 `mcp_mcp_api_requester_make_request`）
- 通过工具描述（description）识别：寻找包含 "HTTP request"、"API"、"GET/POST" 等关键词的工具
- 如果找不到 HTTP 请求类 MCP 工具，则引导用户部署 mcp_api_requester（见 MCP 可用性检查）

**第二步：确定 Base URL 和认证 Header**

| 模式 | Base URL | 认证 Header |
|------|----------|-------------|
| 托管模式 | `https://open.maic.chat` | `Authorization: Bearer <access-code>` |
| 本地模式 | 用户提供的地址（已将 `localhost`/`127.0.0.1` 替换为 `host.docker.internal`） | 无 |

**第三步：Feature Detection（发送可选功能前）**

在发送生成请求前，先查询 `GET <BASE_URL>/api/health`（托管模式需带 auth header），检查返回的 `capabilities` 对象：

```json
{
  "status": "ok",
  "version": "...",
  "capabilities": {
    "webSearch": true,
    "imageGeneration": false,
    "videoGeneration": false,
    "tts": true
  }
}
```

- 只有当 `capabilities` 中某项为 `true` 时，才能在生成请求中将对应 feature flag 设为 `true`
- `capabilities` 只用于向用户说明服务端已配置的能力，不转换为请求字段；生成请求始终只发送 `requirement` 和可选的 `materialIds`

**第四步：发送 POST 请求**

使用识别到的 HTTP 请求工具发送请求。根据上面确定的模式和 URL 构造请求：

**托管模式**：
```json
{
  "url": "https://open.maic.chat/api/generate-classroom",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json",
    "Authorization": "Bearer <access-code>"
  },
  "body": {
    "requirement": "...",
    "materialIds": ["mat_..."]
  }
}
```

**本地模式**：
```json
{
  "url": "<BASE_URL>/api/generate-classroom",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "requirement": "..."
  }
}
```

**MCP 工具不可用的处理**：

告知用户：

> 未检测到 `mcp_api_requester` MCP 服务。请先从 https://github.com/yryuu/mcp-api-requester 下载代码并部署，然后在 WeKnora 中注册该 MCP 服务。

### Phase 4: 查询任务进度

API 返回 `jobId` 和 `pollUrl` 后，执行以下流程：

**第 1 次查询（提交后立即执行）**：
1. 调用 HTTP 请求工具 `GET {pollUrl}` 获取当前状态
2. 检查 `status`：
   - 如果 `succeeded` → 进入 Phase 5
   - 如果 `failed` → 报告错误并停止
   - 如果 `queued` 或 `running` → **停止查询，告知用户**：

     > 课程正在生成中，预计需要 2-10 分钟。请稍后询问我查询进度。
     > Job ID: {jobId}

**用户询问进度时（第 2 次查询）**：
1. 再次调用 `GET {pollUrl}`
2. 检查 `status`：
   - 如果 `succeeded` → 进入 Phase 5
   - 如果 `failed` → 报告错误并停止
   - 如果仍在 `queued` 或 `running` → **停止查询，告知用户继续等待**：

     > 课程仍在生成中，请稍后再试。
     > Job ID: {jobId}

**重要规则**：
- 提交后只查询 **1 次**，不要连续轮询
- 用户询问进度时只查询 **1 次**，不要连续轮询
- 仅在 `status` 为 `succeeded` 或 `failed` 时才继续下一步——否则必须停止并告知用户等待
- 不要尝试重新提交 job——保持查询同一个 `pollUrl`

### Phase 5: 返回结果

生成成功后，返回：

```
Classroom ID: <classroomId>
Classroom URL:
<BASE_URL>/classroom/<classroomId>
```

托管模式的 URL 格式：`https://open.maic.chat/classroom/<classroomId>`

> URL 必须以纯文本独占一行输出，不加粗、不加代码格式、不加 Markdown 链接。

通用生成结果可在对话展示，但不会被页面解析为已发布课程或完成证据。需要任务追踪时，使用上方「巩固知识」入口。

## 错误处理

| 错误 | 含义 | 处理方式 |
|------|------|----------|
| 连接失败 | 网络不通或服务未启动 | 检查 Base URL 是否正确，服务是否启动 |
| 401 | 访问码无效（托管模式） | 告知用户到 open.maic.chat 检查或重新生成访问码 |
| 403 | 每日配额用尽（托管模式） | 告知每日 10 次限制，次日零点重置 |
| 500 | 服务器错误 | 建议稍后重试或切换到本地模式 |
| Provider 配置错误 | 模型/Provider/认证问题 | 引导用户检查 配置或联系管理员 |

## 多文档 → 课程集

当用户需要将多个文档/知识片段生成课程集时：

1. 收集所有文档内容
2. 为每个文档/主题分别生成 requirement
3. 通过 MCP 工具依次调用生成 API（不可并行，避免配额冲突）
4. 如果 MCP 工具不可用，告知用户先部署 mcp_api_requester（见 MCP 可用性检查）
5. 汇总返回所有 Classroom URL

## 概念图遍历 → 批量微课堂

当用户需要基于知识图谱概念批量生成课堂时（场景 4），遵循 Phase 1.2 的完整流程。

**MVP 课程编排策略**：one concept → one micro-classroom

**课程类型标注**：requirement 中标注 `micro-classroom`

**批处理可恢复性**：生成 manifest JSON，记录每个 concept 的生成状态，失败时可从断点继续。

**关键约束**：
- wiki 读取和脚本转换允许 batching
- OpenMAIC API 生成 concurrency=1
- concept.Summary 作为 requirement 核心锚定
- entity 只取 title + summary，不取完整 content
- 无关联 entity 的 concept 仍可生成课堂（缺少实践环节）

## 注意事项

- 脚本在 Docker 沙箱中执行，**沙箱默认禁用网络访问**
- **必须通过 WeKnora MCP 工具调用 OpenMAIC API**——不提供 curl 命令作为降级方案
- MCP 工具名称格式为 `mcp_{service_name}_{tool_name}`，根据描述识别 HTTP 请求工具
- 如果 MCP 工具未启用或不可用，告知用户先从 https://github.com/yryuu/mcp-api-requester 下载代码并部署，然后在 WeKnora 中注册该 MCP 服务
- 单次生成任务预计 2-10 分钟，取决于内容复杂度和服务端模型配置
- 托管模式（open.maic.chat）每天最多 10 次生成配额，独立于 Web UI 配额
- 如果用户在同一个 job 仍在运行时要求生成新课程，不要重复提交——先检查已有 job 状态
