package public

const apiVDFPrincipleDetails = `<details id="api-vdf-principle" class="api-details api-principle-details">
  <summary data-i18n="api.vdfSummary">展开了解 RSA repeated-squaring 的计算原理</summary>
  <div class="api-details-actions">
    <button type="button" class="button-link api-copy-markdown" data-copy-markdown="api-vdf-principle-markdown" aria-describedby="api-vdf-copy-status" data-i18n="api.copyButton">复制原理Markdown</button>
    <span id="api-vdf-copy-status" class="api-copy-status" role="status" aria-live="polite"></span>
  </div>
  <div class="api-principle-content">
    <p data-i18n="api.vdfIntro">这一步计算的是顺序工作量证明，不是普通密码哈希。挑战给出 RSA 模数 N、起始值 base 和最终迭代数 iterations；客户端只能按顺序使用前一次结果继续模平方。</p>
    <h4 data-i18n="api.vdfProcess">计算过程</h4>
    <pre><code>y = decode_unsigned_big_endian(base)
重复 iterations 次：
    y = (y × y) mod N
solution = base64url_no_padding(unsigned_big_endian_384(y))</code></pre>
    <p data-i18n="api.vdfEncoding">modulus、base 和 solution 都必须是恰好 384 字节的无符号大端整数，再编码成无填充 base64url，因此线上字符串长度固定为 512。即使结果前面是零，也不能删掉前导零字节。</p>
    <h4 data-i18n="api.vdfSequentialTitle">为什么必须顺序执行</h4>
    <p data-i18n="api.vdfSequential">第 i+1 次平方依赖第 i 次的完整结果。数学上最终值等于 base^(2^iterations) mod N，但客户端不知道 RSA 模数的陷门，不能把指数按欧拉函数化简；直接构造 2^iterations 也不能绕过这些依赖。多线程拆分不同区间后无法独立合并，所以应使用单条顺序循环。</p>
    <h4 data-i18n="api.vdfServerTitle">服务端如何确认结果</h4>
    <p data-i18n="api.vdfServer">主节点持有只存在于内存中的 RSA 陷门，可以快速得到同一最终值，并在创建挑战时保存定长结果的摘要。授权时服务端先检查挑战版本、算法、来源、资产、客户端前缀、有效期和一次性状态，再校验 0 &lt; solution &lt; N 以及结果摘要。RSA 陷门和预期答案不会发送给客户端或写入 PoW 遥测。</p>
    <h4 data-i18n="api.vdfPitfallsTitle">实现时最容易出错的地方</h4>
    <ul class="api-principle-points">
      <li data-i18n="api.vdfPitfallIterations">循环次数必须正好等于响应里的最终 iterations，不能使用本地默认值。</li>
      <li data-i18n="api.vdfPitfallSquare">每轮都必须先平方再对 N 取模，不能改成哈希、乘以 base 或并行 nonce 搜索。</li>
      <li data-i18n="api.vdfPitfallEncoding">解码和编码都使用无符号大端；输出必须左侧补零到 384 字节。</li>
      <li data-i18n="api.vdfPitfallBase64">base64url 使用 -、_ 且不带 = padding。</li>
      <li data-i18n="api.vdfPitfallBinding">挑战有有效期并绑定资产和客户端来源；失败后应重新创建挑战，不要跨资产或跨 API 版本复用。</li>
    </ul>
  </div>
  <pre id="api-vdf-principle-markdown" hidden># Mirror Server API V2 repeated-squaring 原理

## 用途

程序下载在调用 &#96;POST /api/public/v2/api/challenges&#96; 后，需要根据响应计算 &#96;solution&#96;，再提交到 &#96;POST /api/public/v2/api/authorizations&#96;。这是顺序工作量证明，不是 SHA 哈希 nonce 搜索。

## 挑战输入

- &#96;algorithm&#96; 必须是 &#96;rsa-repeated-squaring-v1&#96;。
- &#96;encoding&#96; 必须是 &#96;base64url-uint-be-384&#96;。
- &#96;modulus&#96;：RSA 模数 N，384 字节无符号大端整数的无填充 base64url，字符串长度 512。
- &#96;base&#96;：起始值，编码规则与 modulus 相同。
- &#96;iterations&#96;：服务端给出的最终迭代数，已经包含文件大小分档和风控倍率。
- &#96;challenge_id&#96; 和 &#96;asset_id&#96; 必须原样用于后续授权请求。

## 顺序算法

&#96;&#96;&#96;text
N = decode_base64url_unsigned_big_endian_384(modulus)
y = decode_base64url_unsigned_big_endian_384(base)

repeat exactly iterations times:
    y = (y * y) mod N

solution_bytes = unsigned_big_endian(y, exactly 384 bytes, left padded with zeroes)
solution = base64url_without_padding(solution_bytes)
&#96;&#96;&#96;

最终值在数学上是 &#96;base^(2^iterations) mod N&#96;。但客户端不知道 RSA 模数的陷门，无法化简指数；每轮又依赖前一轮结果，因此不能把迭代区间拆给多个线程后再合并。正确实现是一条顺序模平方循环。

## 编码边界

1. modulus、base、solution 在线上都固定为 384 字节、512 个 base64url 字符。
2. 整数按无符号大端解释，不能使用十进制、十六进制或可变长字节串。
3. solution 前导零必须保留到 384 字节。
4. base64url 使用 &#96;-&#96; 和 &#96;_&#96;，不得包含 &#96;=&#96; padding。
5. solution 必须满足 &#96;0 &lt; solution &lt; N&#96;。

## 授权请求

&#96;&#96;&#96;json
{
  "challenge_id": "挑战响应中的 challenge_id",
  "asset_id": "创建挑战时使用的 asset_id",
  "solution": "512 字符定长 base64url 结果"
}
&#96;&#96;&#96;

挑战会绑定 API V2、算法、资产、客户端网络前缀和有效期，并且成功授权后只能消费一次。不要跨资产、跨 Web/API 或跨 V1/V2 复用挑战；挑战过期或失败后应重新创建。

## 服务端校验原理

主节点持有仅存在于内存中的 RSA 陷门，可以快速计算相同最终值。创建挑战时保存的是定长预期结果的摘要；授权时先检查挑战绑定关系和生命周期，再检查 solution 的定长编码、数值范围和摘要。RSA 陷门、预期答案和 solution 都不会写入独立 PoW 遥测日志。</pre>
</details>`
