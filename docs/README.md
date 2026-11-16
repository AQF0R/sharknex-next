# 文档索引

按「现行 / 历史 / 拟议」三类组织，避免重复维护同一主题。

## 现行（反映 main 分支代码现状）

| 文档 | 内容 |
|---|---|
| [plans/architecture-baseline.md](plans/architecture-baseline.md) | 现状架构基线、五类能力边界、双入口策略、已知偏差 |
| [plans/phase-delivery-map.md](plans/phase-delivery-map.md) | 六迭代 + 四增量的完成度映射、永久测试资产现状、明确未实现清单 |
| ../README.md | 开发启动、运行边界、接口契约 |

## 拟议（dev 分支计划，未实施）

| 文档 | 内容 |
|---|---|
| [plans/next-phase-execution.md](plans/next-phase-execution.md) | 下一阶段「能力契约与阶段验收可信化」：工作项、验收标准、顺序、非目标、回滚、待确认项 |
| [plans/skill-library-integration.md](plans/skill-library-integration.md) | 技能库选型（ctf-skills 主战 + ACSS 定向摘取）与 TSecBench 闭卷/平台/工具面改造清单、六域路由表、M0-M5 里程碑 |

## 历史（保留原文，仅代表当时状态，不作为现状依据）

| 文档 | 历史结论的现代更正 |
|---|---|
| ../设计文档.md | 原始总设计与六迭代验收标准；「下一步初始化工程」已过时 |
| implementation.md / verification.md | 「不建设 UI/LLM」「无已有测试」仅适用当时 |
| console-design.md / console-verification.md | 「仅事件刷新快照」已被活跃运行持续刷新覆盖 |
| pi-backend-design.md / pi-backend-verification.md | 保留一次真实提供方执行成功的历史记录 |
| asset-discovery-design.md / asset-discovery-verification.md | 「默认 12 端口」「用户输入端口」属旧入口，现为 Planner 选定 |
| unified-workflow-design.md / unified-workflow-verification.md | 模型规划验收使用确定性夹具，真实提供方验收见 next-phase W4 |
| ../web/pi-console-notes.md | local/Pi 模式切换描述已被统一表单替代 |

## 阅读顺序建议

新协作者：architecture-baseline → phase-delivery-map → next-phase-execution → 按需查历史文档。
实施下一阶段者：next-phase-execution.md 是唯一执行入口，其验收标准冻结后再动代码。
