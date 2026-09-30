# forkchain

本地区块链分叉状态内核（Go 库）。单一代币，金额为最小单位整数（`math/big.Int`，
无浮点），创世总量上限 10^27。地址为可信字符串；nonce 与授权初始为 0。
调用串行、状态全在内存，不实现签名、出块与交易池。

## API

- `NewChain(genesisID, balances)`：创建链，创世块永久保留。
- `AddBlock(id, parent, txs)`：按父块历史状态依次验证交易，全部成功才保存
  不可变区块；任一笔失败则整块拒绝且不留效果。不改变当前头。
- `SwitchHead(id)`：把头切到任意已有区块（祖先或另一分支），旧分支保留。
- `Read(accounts, grants)`：返回当前头、指定账户的余额/nonce、指定
  (owner, spender) 授权额；未出现的条目为 0。
- `Stats()` / `ResetStats()`：实际处理的区块数与触及的状态条目数。

交易两类（每块至多 200 笔）：`Approve(sender, spender, amount, nonce)`
直接替换授权；`Batch(sender, owner, outputs, nonce)` 1–500 个收款项，
执行前检查 owner 余额与 owner→sender 授权（sender == owner 不豁免），
nonce 必须等于 sender 当前 nonce，成功后仅 sender 的 nonce 加 1。

## 设计

每个区块只存储自己触及的状态条目增量（旧值/新值）。切换头时，从旧头
撤销到最近公共祖先、再向下重放到新头，工作量只取决于两端之间的区块
及其触及的条目，与公共前缀长度和总账户数无关；不为任何区块复制全量
状态。侧链校验通过临时切换到父块执行、再切回完成，失败时撤销部分增量。

## 测试

`GOCACHE=/private/tmp/gocache go test ./...`（沙箱环境下默认缓存目录不可写时）
