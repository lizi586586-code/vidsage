# OpenMAIC API Reference

OpenMAIC 服务 API 接口规范。所有 API 基础路径为 `{base_url}/api`，默认 `http://localhost:3000/api`。

## 认证机制

- 如果 OpenMAIC 未设置 `ACCESS_CODE` 环境变量：所有接口开放
- 如果设置了 `ACCESS_CODE`：需先通过 `/api/access-code/verify` 获取 cookie
- `/api/health` 始终无需认证

### 获取认证 Cookie

```
POST {base_url}/api/access-code/verify
Content-Type: application/json

{ "code": "<ACCESS_CODE>" }
```

成功返回 `{ "success": true, "valid": true }` 并设置 `openmaic_access` cookie（7 天有效）。

## 标准响应格式

**成功**:
```json
{ "success": true, ...additionalFields }
```

**错误**:
```json
{ "success": false, "errorCode": "<Code>", "error": "<message>", "details?": "<detail>" }
```

---

## WeKnora 学习状态回写

OpenMAIC 服务端 bridge 在课堂完成事件得到可信验证后，向 WeKnora 写入状态。浏览器不得调用此接口。

```http
POST {weknora_custom_base_url}/api/custom/learning/openmaic/status
Authorization: Bearer <OPENMAIC_BRIDGE_SECRET>
Content-Type: application/json
```

请求体：

```json
{
  "external_learner_id": "learner-365271855",
  "classroom_id": "classroom-id",
  "classroom_url": "https://open.maic.chat/classroom/classroom-id",
  "status": "completed",
  "completed_at": "2026-10-08T08:00:00Z",
  "completion_source": "openmaic-server",
  "evidence_version": "completion-event-v1",
  "idempotency_key": "classroom-id:completed"
}
```

`OPENMAIC_BRIDGE_SECRET` 必须在 OpenMAIC bridge 与 WeKnora custom-backend 两端配置为同一高熵密钥。WeKnora 未配置密钥时返回 `503 status_sync_unavailable`，密钥错误时返回 `401 unauthorized`。

## OpenMAIC 核心端点

### 1. 健康检查

```
GET {base_url}/api/health
```

响应：
```json
{
  "success": true,
  "status": "ok",
  "version": "0.1.0",
  "capabilities": {
    "webSearch": true,
    "imageGeneration": false,
    "videoGeneration": false,
    "tts": true
  }
}
```

> `capabilities` 用于功能检测，只在返回此字段时才启用对应的可选功能。

---

### 2. 生成课程（异步任务）

#### 2a. 创建生成任务

```
POST {base_url}/api/generate-classroom
Content-Type: application/json
```

请求体：
```json
{
  "requirement": "教学主题描述",
  "materialIds": ["mat_01..."]
}
```

新版协议使用 `requirement` 和可选的 `materialIds`，材料先上传。不能以源码版本推断托管服务协议。

2026-10-09 实测 `open.maic.chat` 为旧版：能力查询返回 404；无材料、空要求的校验探测 `{requirement:"",pdfContent:""}` 返回 400，明确要求 `{ text: string; images: string[] }`。Bridge 仅在这两个信号均匹配时使用旧版协议：

```json
{"requirement":"视频主题、教学要求与学员偏好","pdfContent":{"text":"完整视频转写","images":[]}}
```

视频转写按 UTF-8 读取，非空，最大 1 MiB，不截断；旧版遇到原始 PDF 返回 `legacy_material_unsupported`，不得忽略材料创建课程。认证或网络失败不触发降级；其他能力响应不符返回 `material_protocol_unavailable`。

成功响应 (202):
```json
{
  "success": true,
  "jobId": "abc123xyz",
  "status": "queued",
  "step": "queued",
  "message": "Classroom generation job queued",
  "pollUrl": "{base_url}/api/generate-classroom/abc123xyz",
  "pollIntervalMs": 5000
}
```

错误：
- `400`: 请求字段、模型配置或材料不可用
- `413`: 请求体或材料超限
- `429`: 当前所有者已有运行中的生成任务
- `500`: 内部错误

#### 2c. 上传材料（PDF/文档）

```http
POST {base_url}/api/materials
Content-Type: application/pdf
X-Material-Filename: lecture-notes.pdf

<原始文件字节>
```

仅新版协议：成功返回 `201` 和 `materialId` 后，使用同一所有者身份将 ID 放入 `materialIds`。生成服务负责启动或等待材料解析；单材料 GET 为可选查询能力，当前托管服务返回 404，不能作为创建课程的必经步骤。上传明确返回解析失败时停止。生成完成后可调用 `DELETE /api/materials/{materialId}` 清理上传材料。

#### 2d. 查询服务能力

```http
GET {base_url}/api/generate-classroom/capabilities
```

该接口返回服务端已配置的可选能力和材料限制；能力由服务端配置决定，不通过生成请求字段覆盖。

#### 2b. 轮询任务状态

```
GET {pollUrl}
```

响应：
```json
{
  "success": true,
  "jobId": "abc123xyz",
  "status": "running",
  "step": "generating_outlines",
  "progress": 35,
  "message": "Generating scene outlines...",
  "pollUrl": "...",
  "pollIntervalMs": 5000,
  "scenesGenerated": 2,
  "totalScenes": 6,
  "result": null,
  "error": null,
  "done": false
}
```

最终成功响应：
```json
{
  "success": true,
  "status": "succeeded",
  "result": {
    "classroomId": "Uyh82Y32ZK",
    "url": "{base_url}/classroom/Uyh82Y32ZK",
    "scenesCount": 6
  },
  "done": true
}
```

最终失败响应：
```json
{
  "success": true,
  "status": "failed",
  "error": "具体错误信息",
  "done": true
}
```

---

### 3. 解析 PDF

```
POST {base_url}/api/parse-pdf
Content-Type: multipart/form-data
```

表单字段：
- `pdf`（文件，必填）: PDF 文件
- `providerId`（可选）: PDF 提供商，默认 `"unpdf"`
- `apiKey`（可选）: 提供商 API Key
- `baseUrl`（可选）: 提供商基础 URL

响应：
```json
{
  "success": true,
  "data": {
    "text": "提取的文本内容",
    "images": [],
    "metadata": {
      "pageCount": 10,
      "fileName": "document.pdf",
      "fileSize": 1024000
    }
  }
}
```

---

> 新版使用材料编号；旧版文本协议见 2a。当前 Bridge 不自动解析原始 PDF，避免未验证的解析结果进入课程。

### 4. 课程存储

#### 4a. 获取课程

```
GET {base_url}/api/classroom?id=<classroomId>
```

#### 4b. 持久化课程

```
POST {base_url}/api/classroom
Content-Type: application/json

{ "stage": {...}, "scenes": [...] }
```

---

### 5. Web 搜索

```
POST {base_url}/api/web-search
Content-Type: application/json

{ "query": "搜索关键词", "pdfText": "PDF上下文", "apiKey": "Tavily Key" }
```

---

### 6. 验证 LLM 连接

```
POST {base_url}/api/verify-model
Content-Type: application/json

{ "model": "openai:gpt-4o", "apiKey": "...", "baseUrl": "..." }
```

---

## 生成管线（逐步调用）

如果需要更细粒度的控制，可以使用以下逐步生成端点代替 `/api/generate-classroom`：

### A. 生成场景大纲（SSE 流）

```
POST {base_url}/api/generate/scene-outlines-stream
```

请求体：
```json
{
  "requirements": { "requirement": "主题", "userNickname": "用户昵称" },
  "pdfText": "PDF文本",
  "pdfImages": [],
  "researchContext": "网络搜索结果",
  "agents": []
}
```

Headers:
- `x-image-generation-enabled`: `"true"` 或 `"false"`
- `x-video-generation-enabled`: `"true"` 或 `"false"`

SSE 事件类型：`languageDirective`、`outline`、`done`、`error`、`retry`

### B. 生成场景内容

```
POST {base_url}/api/generate/scene-content
Content-Type: application/json

{
  "outline": {...},
  "allOutlines": [...],
  "stageId": "stage-1",
  "pdfImages": [],
  "agents": [],
  "languageDirective": "使用简体中文"
}
```

### C. 生成场景动作

```
POST {base_url}/api/generate/scene-actions
Content-Type: application/json

{
  "outline": {...},
  "allOutlines": [...],
  "content": {...},
  "stageId": "stage-1",
  "agents": [],
  "languageDirective": "使用简体中文"
}
```

### D. 生成 Agent 画像

```
POST {base_url}/api/generate/agent-profiles
Content-Type: application/json

{
  "stageInfo": { "name": "入门阶段", "description": "..." },
  "sceneOutlines": [{ "title": "...", "description": "..." }],
  "languageDirective": "使用简体中文",
  "availableAvatars": [],
  "avatarDescriptions": []
}
```
# WeKnora 巩固知识任务契约

- 浏览器 `POST /api/custom/learning/openmaic/prepare`：当前学员 Bearer 身份；严格 JSON `{video_id?:string,knowledge_ids?:string[],requirement:string}`；资料最多 5 个指定引用，后台可读取当前视频的已登记转写引用。逐个通过 WeKnora 权限校验，PDF 必须读取原文件。
- 后台读取当前学员记忆设置及 active/preference；关闭记忆时明确使用零条偏好，不冒充已读取有效偏好。输入只暂存内存 15 分钟，自研库保存任务与课程引用。
- 原生 Skill 调用 `openmaic_start_learning_course`：严格 JSON `{request_id:string}`，必填，无未知字段。只负责领取该任务；重复领取不创建第二课堂。
- bridge `POST /api/custom/learning/openmaic/:requestID/claim`：仅后台密钥可调用；领取后进入 submitting，资料只向 bridge 返回一次。
- bridge 自动上传资料，校验上传结果后创建生成任务，由生成服务负责材料解析。创建请求结果不明确时失败关闭，禁止重复提交。远程任务引用先保存到受限日志卷，再回写后台。
- bridge 每 15 秒查询生成进度并回写；重启后从日志卷和 `GET /api/custom/learning/openmaic/pending` 恢复。日志仅包含引用与安全错误码，无资料、偏好、提示词和凭据。
- 浏览器 `GET /api/custom/learning/openmaic/:requestID/status`：只读本人任务，后台结果是页面唯一来源；5 秒刷新生成进度，成功后呈现真实 HTTPS 课堂链接。
- generation_status：queued/submitting/running/succeeded/failed/cancelled。本人取消接口、严格请求体、最终状态竞争和迟到回写保护统一见[暂停生成规格](../../../../../output/development/specs/review-material-pause/spec.md)；远程计算未承诺停止。学习 status：unknown/not_started/in_progress/completed。当前范围不做学习完成回传；成功课堂学习状态为 unknown，不返回学习同步错误。
- 回写只接受 running/succeeded/failed，按 request_id 定位既有任务；学员、远程任务和课堂绑定不可覆盖。未知字段、非法 JSON、截断和尾随内容拒绝；不存在任务拒绝；URL 仅接受 `https://open.maic.chat/classroom/{classroom_id}` 且禁止 pending。
- 当前托管服务学习身份接口无授权探测返回 404；需要开放逐学员授权、整课必需场景清单和完整学习证据，才能增加完成发布。单测验 session.completed、历史 PBL completed、打开链接和口述均不足。

- 一小时后先查询远程生成结果：成功仍发布；queued/running 记 generation_timeout_confirmed；查询不可用记 generation_timeout_unverified。两种新失败均终止恢复；历史 generation_timeout 且保留 job_id 的任务允许一次恢复查询及同一绑定的状态回写。

## 教学计划与配音验收契约

准备与领取上下文新增 `teaching_plan`，仅保存在短期内存；现有工作器尚未消费，不代表已加载或已配音。服务端确定性生成，无模型纠偏。

| 字段 | 类型与允许值 |
|---|---|
| version | 必填字符串，teaching-v1 |
| recommended_skills | 必填去重非空字符串数组；当前恰好一个 feynman-learning 或 workshop-style |
| ability | 必填字符串，not_assessed；不得从偏好推断能力 |
| narration_required | 必填布尔，true |
| language | 必填字符串，zh-CN |
| pacing | 必填字符串，normal 或 slow；只按本人有效偏好选择 |

所有字段必填，不接受 null、Markdown、围栏或尾随文本。读取边界需拒绝未知计划字段；当前计划由服务端构造，原生传输接入时必须落实接收校验。来源和身份沿用既有准备契约，不生成新证据引用。

### 原生入口与实际验证

- GET `/api/agent/runtime`：实际 enabled/runtimeEnabled 均为 true。
- GET `/api/agent/skills`：实际返回 24 个内置 Skill，白名单须与当次查询交集。
- POST `/api/agent/sessions`：传 prompt 与明确 skill；实际返回 202、会话 id、skillId，查询对应会话及 events。
- 只接受持久 `message_end` 中成功 `toolResult/read`：目标 SKILL.md 路径匹配、offset=1、totalLines 为正整数、lines 完整覆盖、sourceHash 为有效版本指纹。助手正文、工具调用和失败读取均无效。
- GET `/api/stages/{id}` 为源码支持的完整文档读取路径，正式接入前仍需线上课程验证；不将旧 `/api/classroom` 的文档格式假设套用新入口。
- POST `/api/stages/{id}/publish` 实际返回 401 login_required；访问码不等于发布权限。公开发布不在当前范围内；私有课堂仍须验证实际学员访问权限。

### 课堂链接返回门槛（待工作器接入）

- 全场景所有 speech 讲稿合法非空，并具备同一课堂 `/api/classroom-media/{id}/...` 音频引用；跨课堂、异域、带查询凭据引用拒绝。
- 引用模块只提取待验路径，不证明音频存在。工作器须逐项确认响应为真实完整音频，固定地址浏览器实际播放通过后才能标记 verified。
- 首次返回课堂链接必须同时满足实际 Skill 版本证据、完整配音及真实学员可访问。未通过时不发布链接、不覆盖已有成果。
- 安全错误：invalid_teaching_plan、skill_loading_unverified、invalid_generation_result、narration_incomplete、invalid_audio_reference；尚未接入的音频传输失败与超时契约须在正式实现前补齐。纠偏预算为零；失败不猜测。
