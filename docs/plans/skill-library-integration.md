# 技能库选型与比赛模式兼容改造方案

> 状态标签：**拟议**（待实施）。本文回答两个问题：外部技能库选哪个；**选中的技能内容本身要改什么**才能兼容 sharknex 架构与 TSecBench 本地 Agent 比赛模式。
> 关联：[architecture-baseline.md](architecture-baseline.md)（现状能力边界）、[next-phase-execution.md](next-phase-execution.md)（W1 能力目录 / W4 真实验收）。

## 1. 选型结论

| 库 | 角色 | 理由 |
|---|---|---|
| **ljagiello/ctf-skills**（MIT，12 技能/约 8 万行） | **主战库**，全量接入 | 六域中 Web/二进制/利用三域主力 Flag 盘子全覆盖；solve-challenge 编排范式（分诊/路由/止损）可直接翻译进 Planner；自带 promptfoo 行为评测；工具链预装器天然闭卷友好 |
| **mukul975/Anthropic-Cybersecurity-Skills**（Apache 2.0，818 技能） | **定向摘取 ~50 个进攻技能** | 仅取云攻击（pacu/IAM/ADCS 41 个）与蜜罐识别子集补六域缺口；其余 760+ 防御向技能不装，避免 description 路由稀释 |

## 2. ctf-skills 技能层改造清单（核心）

> 原则：**overlay 补丁不改 upstream 原文**，改动脚本化、可 rebase、可 CI 门禁（见 §4）。

### 2.1 闭卷合规改造（已扫描出 28+ 处联网依赖）

| 冲突点 | 位置 | 改造 |
|---|---|---|
| 联网搜相似题解 | `ctf-similar-search/`（Tavily API） | **整技能移除**，闭卷红线 |
| CTFd 平台检测 | `solve-challenge/SKILL.md` Step 0 | 替换为 TSecBench 接入序列（见 2.2） |
| 联网装工具 | 各类别 SKILL.md 的 Prerequisites（pip/apt/brew/gems/go） | 全部改写为"已预装于镜像"，运行时只执行 `install_ctf_tools.sh --verify` 自检 |
| 联网取上下文 | Step 1 Recon "Fetch links"、`allowed-tools: WebFetch WebSearch` | 移除；URL 访问仅限 VPN 内题目目标（走 `http.request`） |
| MCP 工具服务 | `ctf-reverse/tools-mcp.md` | Claude 生态专属，改写为 sharknex 工具调用说明或整体移除 |
| PAT 载荷库 | pat 模式运行时浅克隆 | 镜像内预置，`ctf-web/payloads/` 固化 |

### 2.2 平台协议改造（CTFd → TSecBench）

1. **Step 0 重写**：CTFd API 检测 → TSecBench Adapter 生命周期（获取凭证 → 10 分钟内接入心跳 → 拉题目清单/详情 → 订阅剩余时间）。
2. **新增 Flag 提交闭环**：原编排器止步于 writeup；改为"候选 Flag 多发校验（保留原有 grep 候选与唯一性检查规则）→ **立即经 Adapter 提交并按平台返回判定**"，提交结果写 Blackboard。
3. **Step 5 writeup 降级**：赛时改为黑盒证据归档（flag + 来源路径 + 关键步骤 → Evidence）；`ctf-writeup` 仅评测结束后复盘使用。
4. **Flag Formats 节**：候选模式保留，最终判定权移交平台 API。

### 2.3 工具面改造（Claude Code → sharknex 四件套）

| 原工具/指令 | 改造后 |
|---|---|
| `allowed-tools: Bash Read Write Edit Glob Grep Task WebFetch WebSearch Skill` | `shell.exec / file.read / file.write / http.request / net.connect / skill.read / task.report`（Pi bridge 新工具面，见 M1） |
| Step 3 "Invoke the Category Skill"（Skill 调用 `/ctf-web`） | `skill.read("ctf-web")` 渐进加载：description 索引 → SKILL.md → references 按需（ctf-pwn 2.2 万行主体永不整载） |
| references 相对路径引用 | 保留路径结构；`file.read` 白名单限定技能目录 + 题目工作区 `/work` |

### 2.4 编排与状态改造（对接 Blackboard / Scheduler）

difficulty-routing 的每题 JSON 状态 → **Blackboard Fact** 结构化落库，字段对齐：

| ctf-skills 状态字段 | sharknex 落点 |
|---|---|
| `declared_difficulty / current_tier / triage_complete` | 题目 Fact：`difficulty`、`tier`、`triaged` |
| `signals`（编码/直接路径/单工件等） | Fact `signals[]`，Planner 路由依据 |
| `metrics.elapsed_minutes / substantive_actions / consecutive_no_progress / independent_attack_families / remaining_unknowns` | 阶段进度 Fact，Scheduler 门禁输入 |
| `solved` | 任务终态 + Flag 提交结果 |

止损参数按 **6h / 74 Flag** 校准（全局吞吐 ≥ 12.3 Flag/h）：
- 单题预算：triage 5 分钟；easy 15 分钟 / mid 30 分钟 / hard 45 分钟**硬上限**；
- `consecutive_no_progress` ≥ 10 分钟强制换攻击族或换题（二攻留给 6h 末段统一调度）；
- tier 门控（升档需"观察到结构复杂度"）保留原文规则；模型分级暂降级为预算分级（单 Pi 提供方），多 provider 位预留。

### 2.5 提示词与触发改造

- **description 不改语义**：它是路由唯一信号，保留原触发词（"buffer overflow/ROP"→pwn 等），保证 promptfoo routing 套件可对比。
- 正文 Claude 向措辞（"You're a skilled CTF player"）**先保留**，在 Pi 提供方上跑 A/B 后再决定是否改写（M5 验收门）。
- **新增技能 `tsec-rules`**（闭卷规则卡）：授权边界 = VPN 内题目资产 + 平台 API；禁止题目数据外传；禁止攻击平台基础设施与其余租户；失败重试受预算约束。所有类别技能 SKILL.md 头部加一行引用。

### 2.6 六域路由表（TSecBench domain → 技能映射）

| TSecBench 域 | 主技能 | 辅助 |
|---|---|---|
| Web 漏洞挖掘 | ctf-web | ctf-misc（jail）、ACSS api-security 子集 |
| 二进制漏洞挖掘 | ctf-reverse | ctf-malware |
| 漏洞利用 | ctf-pwn | ctf-reverse |
| 多阶段渗透 | ctf-web（auth-infra）+ ACSS red-teaming 子集 | ctf-misc（K8s/沙箱逃逸） |
| 云攻击 | ACSS：exploiting-aws-with-pacu + IAM/ADCS 41 个 | ctf-web（SSRF→云元数据） |
| 对抗规避 | ctf-web sql-injection（WAF 绕过节）+ ACSS deception 反用 | 薄弱项，M4 专项补强 |

## 3. ACSS 子集处理

- 摘取清单：`exploiting-*` 全部 + `red-teaming` 域 + `deception` 域，约 50 个；改造成 2.3 同款工具面（其 frontmatter 的 ATT&CK/CSF 映射保留，进报告层）。
- 其脚本同样是"编排器"性质（依赖 awscli/pacu 等底座），镜像预装。

## 4. Fork 与补丁管理

```
sharknex-skills（fork ctf-skills）
├── upstream/main        # 原样跟踪
├── tsec/overlay         # 我们的补丁层
│   ├── patches/*.py     # 脚本化改造（2.1-2.5 各项，幂等可重放）
│   ├── removed/         # similar-search 等移除清单
│   └── added/tsec-rules/
└── ci: 闭卷扫描(grep 联网关键词) + promptfoo routing 冒烟
```

- 升级流程：rebase upstream → 重放 patches → CI 门禁（联网残留=0、routing 冒烟通过）→ 更新镜像。
- 两库 LICENSE 文件保留在镜像内。

## 5. 验收标准（并入 next-phase W4/W5）

1. **闭卷扫描**：CI 对全量技能 grep 联网关键词（pip install/apt/brew/tavily/WebFetch/WebSearch/go get），残留 0。
2. **routing 套件**：改造后技能在 Pi bridge 上的类别路由准确率 ≥ 原 Claude 基线 90%（promptfoo provider 换接）。
3. **solve 套件**：自带离线 fixture，有技能 vs 无技能 A/B 增益可量化。
4. **提交闭环**：候选 Flag → Adapter 提交 → Blackboard 记录判定，全链路一次演练通过。

## 6. 里程碑对齐

| 里程碑 | 内容 | 依赖 |
|---|---|---|
| M0 | 闭卷镜像 + 工具链预装 + 技能库预置（含 patches 重放） | 无 |
| M1 | Pi 四件套 + `skill.read` 渐进加载 | M0 |
| M2 | TSecBench Adapter + VPN 接入（并行，纯 Go 侧） | 无 |
| M3 | Planner 六域路由 + 难度状态 Fact + 止损门禁 | M1+M2 |
| M4 | ACSS 云域子集注入 + 对抗规避补强 | M3 |
| M5 | promptfoo A/B 验收 + 提示词定稿 | M3 |
