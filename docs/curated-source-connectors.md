# RSS 与明确导出材料的来源适配

`CuratedSourceAdapter` 接入既有 SourceAdapter、source-agent 租约、持久化 outbox、SourceIngest、知识分析和 Release 发布链。没有另建原文数据库，也不会绕过分析与质量门直接发布。

| 来源 | 当前入口 | 覆盖边界 |
|---|---|---|
| 公开 RSS / 小宇宙播客 | 明确配置的 HTTPS RSS 2.0 URL | 只读 feed 中当前条目；说明文字是 feed_excerpt，不是音频转写 |
| X、知乎、得到、小宇宙 | 显式提供的私有 JSON 导出文件 | 只读文件列出的条目；平台、作者和转写标签属于导出者声明 |
| 得到原生内容 | 继续使用既有得到下载/知识包链 | 本适配器不发现登录信息或增加付费内容读取权限 |
| 微信群、Kim 群 | 继续使用既有获准聊天采集及私有引用链 | 不把聊天原文复制进这个导出适配器 |

没有自动登录、网页通用抓取、音频下载、ASR、X/知乎 API 订阅或 Atom 支持。没有公开 RSS 的播客需提供获准的摘录/转写。来源类型为用户配置的分类，不等于已经核验平台身份。

## 本地配置

创建 0600 的配置文件，例如 `.private/curated-sources.json`：

```json
{
  "selected-podcast": {
    "source_type": "xiaoyuzhou_episode",
    "feed_url": "https://feeds.example.org/selected.xml"
  },
  "selected-posts": {
    "source_type": "x_post",
    "export_file": ".private/selected-posts.json"
  }
}
```

每个来源必须且只能配置 feed_url 或 export_file。URL 不能含用户信息、查询参数或 fragment；不跟随重定向。订阅和远端任务不能修改本地 URL/文件路径。导出文件须为私有常规文件；不扫描目录。

导出契约示例：

```json
{
  "schema_version": "curated_source.v1",
  "source_type": "x_post",
  "account_key": "selected-posts",
  "items": [{
    "id": "stable-post-id",
    "title": "Synthetic example",
    "url": "https://example.org/posts/stable-post-id",
    "author": "Example author",
    "published_at": "2026-10-09T00:00:00Z",
    "content": "This is synthetic example material long enough to demonstrate the import contract.",
    "content_kind": "excerpt"
  }]
}
```

source_type 支持 rss_entry、xiaoyuzhou_episode、x_post、zhihu_answer、dedao_article。content_kind 支持 original、excerpt、transcript、manual_transcript、feed_excerpt。稳定 id 不随编辑变化，正文/标题等变化会生成新的幂等键；同一来源 id 不允许在文件内重复。published_at 若提供必须是带时区的 RFC3339。

## 启动与既有 API

通过环境变量显式设置 `KBASE_REMOTE_URL`、`KBASE_SOURCE_AGENT_TOKEN`、`KBASE_SOURCE_AGENT_ID`、`SOURCE_AGENT_STATE_DIR`，沿用现有 agent 令牌配置。不要把真实令牌写进文档或命令历史。

```sh
go run ./cmd/curated-source-agent --config .private/curated-sources.json --once
```

不带 `--once` 则每 30 秒查询既有任务队列；这不表示每 30 秒抓取来源，也不会安装后台服务。配置改变需要重启进程。

通过已有管理 API `POST /api/source-subscriptions` 建立订阅，字段示例：

```json
{
  "source_type": "x_post",
  "source_account_key": "selected-posts",
  "source_account": "Selected research material",
  "agent_id": "example-curated-agent",
  "operation": "sync_curated",
  "schedule": "manual",
  "enabled": true
}
```

再调用 `POST /api/source-subscriptions/{id}/sync`，提交 `{"operation":"sync_curated"}`。来源 type/key 必须匹配本地白名单。返回入库回执后仍需经过既有分析、质量审查、Release 发布；Proofroom 消费的是发布 feed，不是所有入库文件。

## 数据边界与验证

- 单份来源最多 4 MiB、200 条，单条正文最多 200000 字节；超限报错，不截断后冒充完整。
- 整份来源先校验再入 outbox；失败不推进游标，重试保留幂等键。HTTP/登录页/格式失败不冒充空 feed。
- RSS 移出窗口不视为删除。没有完整历史声明，coverage 为 provided_items_only。
- RSS HTML 转纯文本，去掉脚本、样式和媒体标签，不下载页面或音频。少于现有入库最小正文长度的条目明确失败。
- 内容类型与覆盖注记保存在 citation.note，随 Release 传递；下游研究将它们作为来源声明，不能当作原文真实性证明。
- 新适配器保留原文版本哈希用于来源去重，另用 canonical package hash 绑定知识包、分析和 Release 摘录回读。范围注记变化使知识包身份与分析状态失效。旧来源的哈希契约保持不变。
- 测试使用合成导出和本地 TLS RSS 服务；未读取真实账号、付费内容或播客音频。
