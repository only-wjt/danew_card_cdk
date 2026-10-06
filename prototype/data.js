// 示例数据，全部是假的。原型里所有页面共用这一份状态。
const S = {
  group: 'ops',
  page: 'ops-platforms',

  egressIp: '203.0.113.24',

  // ── 卡台：一家供应商一行，下面挂若干「连接」（协议 + 凭证 + 能力） ──
  platforms: [
    {
      id: 'spacex', name: 'SpaceX', site: 'https://spacexcard.example', enabled: true,
      gptRole: 'primary', // GPT 主台
      conns: [
        { id: 11, proto: 'spacexcard-legacy', protoLabel: '旧 OpenAPI · X-API-Key', caps: ['gpt', 'x_spacex'], cred: 'sk_live_****8f2c', state: 'ok', lastOk: '1 分钟前' },
      ],
      gpt: { spendable: 1204.0, reserve: 20, fee: '$1 / $5 / $10', feeNote: 'Plus / Pro 5x / Pro 20x', circuit: 'closed', fail: 0, unused: 132, ok24: 86, fail24: 2 },
      x: { wallet: 410.5, unused: 38, ok24: 21, fail24: 0 },
      webhook: { secret: true, url: 'https://danew.cc/api/v1/webhooks/card/spacex', last: '3 分钟前 · order.completed' },
      cards: [
        { code: 'VISA-US-01', label: 'Visa 美区 01', online: true, order: 1 },
        { code: 'MC-PH-02', label: 'Master 菲律宾 02', online: true, order: 2 },
        { code: 'VISA-JP-03', label: 'Visa 日本 03', online: false, order: 3 },
      ],
      blocked: [{ id: 88123, reason: '连续 3 次 card_declined' }],
      forceNewCard: false,
    },
    {
      id: 'avan', name: 'Avanfinity', site: 'https://avanfinity.example', enabled: true,
      gptRole: 'off', // off | backup
      conns: [
        { id: 21, proto: 'avanfinity-2026-08', protoLabel: 'OpenAI · /api/v1 · AppId/Secret', caps: ['gpt'], cred: 'app_7731 / ****', state: 'idle', lastOk: '未启用' },
        { id: 22, proto: 'avanfinity-api-v1', protoLabel: 'X 会员 · /api/v1 · AppId/Secret', caps: ['x_cdk'], cred: 'app_7731 / ****', state: 'warn', lastOk: '2 小时前', error: '买 CDK 被拒：出口 IP 203.0.113.24 不在这个 App 的白名单' },
      ],
      gpt: { spendable: 300.0, reserve: 0, fee: '$1.2 / $5.5 / $11', feeNote: 'Plus / Pro 5x / Pro 20x', circuit: 'closed', fail: 0, unused: 0, ok24: 0, fail24: 0 },
      x: { wallet: 820.4, liability: 96.0, unusedCdk: 12, ok24: 9, fail24: 1 },
      webhook: { secret: false, url: 'https://danew.cc/api/v1/webhooks/card/avan', last: '—' },
      cards: [
        { code: 'AV-VISA-01', label: 'Avan Visa 01', online: true, order: 1 },
      ],
      blocked: [],
      forceNewCard: false,
      calls: [
        { at: '10:42:11', m: 'POST', p: '/api/v1/x/redemptions', s: 403, d: 'ip_not_whitelisted' },
        { at: '10:41:58', m: 'GET', p: '/api/v1/x/quote', s: 200, d: 'premium_monthly $8.10' },
        { at: '09:12:03', m: 'GET', p: '/api/v1/wallet', s: 200, d: '$820.40' },
      ],
    },
  ],

  // ── GPT 发码策略 ──
  gptPolicy: { backupOn: false, allowSingle: true },

  // ── X：统一套餐 + 供货来源 ──
  // 供货来源：spacex = SpaceX（客户填 X Cookie）；avan_cdk = Avanfinity CDK（客户填 X 用户名）
  // Avan 直充已决定不用：后端代码保留，界面隐藏
  xSources: {
    spacex: { label: 'SpaceX', sub: '客户填 X Cookie', platform: 'spacex', state: 'ok' },
    avan_cdk: { label: 'Avanfinity', sub: '客户填 X 用户名', platform: 'avan', state: 'err' },
  },
  xPlans: [
    { key: 'x_basic_1m', name: 'Basic 1 个月', cost: { spacex: null, avan_cdk: 3.4 }, source: 'avan_cdk', on: true, cap: 4.5 },
    { key: 'x_premium_1m', name: 'Premium 1 个月', cost: { spacex: 8.2, avan_cdk: 8.1 }, source: 'avan_cdk', on: true, cap: 9.5 },
    { key: 'x_premium_3m', name: 'Premium 3 个月', cost: { spacex: 22.0, avan_cdk: 23.5 }, source: 'spacex', on: true, cap: 26 },
    { key: 'x_premium_12m', name: 'Premium 12 个月', cost: { spacex: 82.0, avan_cdk: 79.0 }, source: 'avan_cdk', on: true, cap: 90 },
    { key: 'x_plus_1m', name: 'Premium+ 1 个月', cost: { spacex: 15.0, avan_cdk: null }, source: 'spacex', on: true, cap: 18 },
    { key: 'x_plus_12m', name: 'Premium+ 12 个月', cost: { spacex: 150.0, avan_cdk: 148.0 }, source: 'spacex', on: false, cap: 170 },
  ],
  xAlerts: { wallet: 50, card: 10, stuck: 30 },

  // ── GPT 套餐 ──
  gptPlans: [
    { key: 'plus', name: 'Plus 1 个月', price: 1 },
    { key: 'pro_5x', name: 'Pro 5x', price: 5 },
    { key: 'pro_20x', name: 'Pro 20x', price: 10 },
    { key: 'go', name: 'Go 1 个月', price: 0.5 },
    { key: 'credit_1000', name: 'Credit 1000', price: 2 },
  ],
  regions: ['菲律宾 PHP', '美国 USD', '日本 JPY', '土耳其 TRY'],

  // ── 本站码（GPT） ──
  gptCodes: [
    { id: 1201, code: 'DN-PLUS-7K2M-QX9A', plan: 'plus', region: '菲律宾 PHP', status: 'unused', bind: { spacex: true, avan: false }, ful: '—', note: '淘宝 0728', at: '07-28 10:20' },
    { id: 1200, code: 'DN-PRO5-H3TT-W1ZC', plan: 'pro_5x', region: '美国 USD', status: 'used', bind: { spacex: true, avan: false }, ful: 'SpaceX', note: '', at: '07-28 09:02' },
    { id: 1199, code: 'DN-PLUS-0PQ8-LL2D', plan: 'plus', region: '菲律宾 PHP', status: 'failed', bind: { spacex: true, avan: false }, ful: 'SpaceX', note: '客服 #331', at: '07-27 22:15' },
    { id: 1198, code: 'DN-GO01-ZZ71-MN0K', plan: 'go', region: '日本 JPY', status: 'unused', bind: { spacex: true, avan: true }, ful: '—', note: '双绑测试', at: '07-27 18:40' },
    { id: 1150, code: 'LEG-8812-KQ', plan: 'plus', region: '菲律宾 PHP', status: 'used', bind: null, ful: 'SpaceX', note: '老码', at: '07-20 11:00' },
  ],
  localStock: [
    { id: 1, email: 'white01@example.com', plan: 'plus', status: '可用', at: '07-25' },
    { id: 2, email: 'white02@example.com', plan: 'plus', status: '已出', at: '07-25' },
  ],

  // ── X 码 ──
  xCodes: [
    { id: 501, code: 'DNX-P1M-7HQ2-AC', plan: 'x_premium_1m', source: 'avan_cdk', status: 'running', group: 'running', user: 'alice_dev', msg: 'CDK 已提交，等待 Avanfinity 开通', usd: 8.1, at: '07-28 10:31', ev: ['10:31 客户提交 @alice_dev', '10:31 用 CDK 兑换（花费 $8.10，上限 $9.50）', '10:32 等待开通'] },
    { id: 500, code: 'DNX-P3M-K1LM-SX', plan: 'x_premium_3m', source: 'spacex', status: 'done', group: 'done', user: 'bob_x', msg: '已开通', usd: 22.0, at: '07-28 09:50', ev: ['09:50 客户提交 Cookie', '09:51 SpaceX 订单完成'] },
    { id: 499, code: 'DNX-P12-QQ90-AC', plan: 'x_premium_12m', source: 'avan_cdk', status: 'todo', group: 'todo', user: 'carol', msg: '上游返回不确定，需要人工确认是否已开通', usd: 79.0, at: '07-28 08:12', ev: ['08:12 客户提交 @carol', '08:13 上游超时', '08:43 查询 6 次仍不确定'] },
    { id: 498, code: 'DNX-B1M-PO0A-AC', plan: 'x_basic_1m', source: 'avan_cdk', status: 'unused', group: 'unused', user: '', msg: '未兑换', usd: 0, at: '07-27 20:00', ev: [] },
    { id: 497, code: 'DNX-PL1-8U7Y-SX', plan: 'x_plus_1m', source: 'spacex', status: 'failed', group: 'failed', user: 'dave', msg: 'Cookie 已过期，客户可重新提交', usd: 0, at: '07-27 19:30', ev: ['19:30 客户提交 Cookie', '19:30 SpaceX 返回 cookie_invalid'] },
  ],
  xBatches: [
    { id: 31, at: '07-28 09:00', plan: 'x_premium_1m', source: 'avan_cdk', qty: 20, used: 6, note: '闲鱼' },
    { id: 30, at: '07-27 15:00', plan: 'x_premium_3m', source: 'spacex', qty: 10, used: 9, note: '' },
  ],

  // ── 兑换对账 ──
  orders: [
    { id: 9021, product: 'gpt', code: 'DN-PRO5-H3TT', plan: 'Pro 5x', who: 'u1@example.com', plat: 'SpaceX', card: 'Visa 美区 01', amt: '$5.00', st: 'completed', at: '07-28 09:03' },
    { id: 9020, product: 'gpt', code: 'DN-PLUS-0PQ8', plan: 'Plus', who: 'u2@example.com', plat: 'SpaceX', card: 'Master 菲律宾 02', amt: '$0.00', st: 'failed', at: '07-27 22:16' },
    { id: 9019, product: 'x', code: 'DNX-P3M-K1LM', plan: 'Premium 3 个月', who: '@bob_x', plat: 'SpaceX', card: '—', amt: '$22.00', st: 'completed', at: '07-28 09:51' },
    { id: 9018, product: 'x', code: 'DNX-P12-QQ90', plan: 'Premium 12 个月', who: '@carol', plat: 'Avanfinity · CDK', card: '—', amt: '$79.00', st: 'uncertain', at: '07-28 08:13' },
  ],

  batches: [
    { id: 71, at: '07-28 08:00', total: 40, ok: 38, fail: 2, st: '已完成' },
    { id: 70, at: '07-27 21:00', total: 120, ok: 80, fail: 0, st: '进行中' },
  ],

  orphans: [
    { at: '07-28 07:12', path: '/api/v1/webhooks/card/avan-old', ev: 'order.completed' },
    { at: '07-27 23:40', path: '/api/v1/webhooks/card/test', ev: 'ping' },
    { at: '07-27 20:01', path: '/api/v1/webhooks/card/avan-old', ev: 'order.failed' },
  ],

  audit: [
    { at: '07-28 10:05', who: 'admin', act: 'X 供货 · Premium 1 个月：SpaceX → Avanfinity', ip: '198.51.100.7' },
    { at: '07-28 09:00', who: 'admin', act: '发码 X · Premium 1 个月 × 20', ip: '198.51.100.7' },
    { at: '07-27 18:40', who: 'admin', act: 'GPT 发码 · Go 1 个月 × 1（双绑）', ip: '198.51.100.7' },
  ],

  // ── 页面局部状态 ──
  ui: {
    plat: 'spacex', platTab: 'overview', platSide: 'spacex',
    xTab: 'supply', xIssue: { plan: 'x_premium_1m', qty: 10, note: '' }, xIssued: [], xGroup: 'todo', xSel: 499, xQ: '',
    cdkTab: 'site', cdkIssue: { plan: 'plus', region: '菲律宾 PHP', qty: 10, note: '', dual: false }, cdkIssued: [], cdkFilter: { status: '', kind: '' },
    ordFilter: { product: '', plat: '', st: '' }, ordSel: null,
    redeem: { step: 1, code: '', info: null, cred: '', err: '' },
    skin: 'default',
  },
}
