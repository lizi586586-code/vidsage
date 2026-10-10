# 巩固学习复习提示词

- 提示词单一来源：`internal/custom/handler/prompts/openmaic-review.txt`，后端编译时嵌入。
- 教学以主动回忆、练习、反馈和再检验为主；讲解稿用于引导复习与纠错，不重新讲授完整课程。
- Langfuse 副本：`openmaic-review`，版本 1，标签 `staging`；运行时不依赖 Langfuse。
- 本次包含尚未入库的 OpenMAIC 后端接入依赖、权限边界、状态与数据库迁移及测试；不包含前端或邀请登录改动。
- 验证：`go test ./cmd/custom-backend/... ./internal/custom/...` 通过。本地后端重启后 healthy，固定视频页及巩固学习选课入口正常，运行二进制包含完整提示词。
- 本地镜像：`weknora/custom-backend:review-prompt-20261010`；保留旧镜像 `weknora/custom-backend:pre-review-prompt-20261010`。未发布生产，未验证新生成课堂的题目和配音质量。
- 同步上游时保留独立 OpenMAIC 模块、配置与路由接入、迁移和提示词嵌入文件。
