# Proofroom 主题连接器：发布版本原文回读

KBase 保持知识包、来源采集、结构化分析、质量门和不可变 Release 的权威。Proofroom 从现有 `/api/knowledge/feed` 增量消费，向现有 receipts 接口提交幂等回执。

新增接口：

`GET /api/knowledge/releases/{release_id}/evidence?content_hash={hash}&citation_id={citation_id}`

复用现有 KBase HTTP 鉴权。客户端必须提供发布版本的内容哈希与引用 ID；不能回退到最新版。响应 `release_evidence.v1` 包括 release_id、content_hash、citation_id、book_id、chapter_id、chunk_id、usage_policy、status、excerpt、truncated。无本地文件路径、凭证或其他来源正文。

- `available`：standard 发布版本，原文快照已核验包哈希。最多 4000 个字符，截断明确标记。
- `metadata_only`：evidence_only，仅引用元数据。
- `unavailable`：原文不再可用，或者旧数据的包哈希不能核验。仍不替换为当前知识包原文。
- 错误版本哈希/篡改发布身份返回 409；不存在的引用返回 404。

新发布版本在发布清单写入之前保存摘录 sidecar。摘录文件位于已有 releases 存储目录，使用原子写入。原始发布 JSON 与 release_id 计算规则保持兼容。旧发布版本没有 sidecar 时，只在当前包重算哈希等于发布哈希时回读。已有历史/测试数据允许自定义哈希；这种数据保留原发布行为，但不作为原文核验成功。

本接口是版本回读能力，不增加通用连接器、消息数据库或新的采集调度。真实生产服务发布及 Proofroom 端 token 配置不包含在本次代码验收中。
