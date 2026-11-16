# 授权资产探测阶段设计

2026-11-15。沿用已有 Go API + Pi Adapter，新增安全、低速资产发现，不实现利用、爆破、隐蔽扫描或全网发现。
用户明确授权 38.76.196.139：低并发普通 TCP connect，常见端口，开放端口的有界服务/HTTP/TLS 信息采集，不扩展关联 IP、域名或重定向。

## 强制预检
模式：复用既有 Adapter 与保护代理，新增工具函数，不引入插件框架或扫描器子进程。接口参数由 Go 绑定，模型不能给工具自定 IP/端口。
数据：端口小数组（最多16）、Set/map 去重 O(P)，串行探测 O(P)，有界响应与报告空间 O(P×B)。默认12端口，连接启动间隔至少500ms、每次TCP连接最长800ms，单目标，HTTP头与片段有界，不跟随重定向。所有网络操作传播 context，执行仍受 Runtime 45秒和全局Pi并发限制。
授权：Scope 新增 tcp_ports 和 authorization，Task 带 ports；只能 IPv4/IPv6 字面量，无DNS扩展。授权声明与Run同生命周期保存，不作为其他目标的授权。网络保护仍拒绝私有/保留地址，精确本地例外沿用运维配置。
验证：Go vet/build、现有测试、前端 lint/type-check/build；临时本地开放端口/取消/报告检查；最后由真实 Pi 对唯一授权公网 IP 执行，保留实际报告，不能承诺端口开放或发现漏洞。

## 数据流
Web“Pi资产探测” → Scope(精确IP、端口、声明) + asset_discovery Task → Scheduler租约 → Pi read_context / discover_services / finish → Go授权审计 → 串行TCP connect、有限banner、HTTP/TLS → 结构化 services_discovered Fact + Evidence + 主机Asset → Web资产报告。
首次覆盖使用一个有界工具批次保证所有声明端口被检查，模型负责结果解释而不是伪造服务。端口表状态区分 open / closed / timeout / error；超时不等于关闭；端口号猜测单列，不作为确定指纹。
仅对常见HTTP端口探测一次相应协议根路径；端口集合本身明确授权此只读根路径采集。SSH/FTP/SMTP仅等有限欢迎banner，不发送认证命令。其他端口只connect；TLS遵守证书验证，失败记录原因不绕过验证。HTTP返回只保留状态、标题、Server/X-Powered-By与有限正文指纹依据，不执行页面脚本。
现有小对象上限16KB：单端口详细字符串有界，完整报告上线前校验长度。发现资产/端口不等于漏洞，Finding验证闭环留待后续独立实现，不能用此阶段宣称安全评估完成。
