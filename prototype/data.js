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
        { code: '537872', label: 'one · PS3750X', bin: '537872', area: '美国', online: true, enabled: true, order: 1 },
        { code: '555659', label: 'one · P5556VX', bin: '555659', area: '美国', online: true, enabled: true, order: 2 },
        { code: '486695', label: 'four · 11107407P', bin: '486695', area: '美国', online: true, enabled: true, order: 3 },
        { code: '446602', label: 'four · 11106307P', bin: '446602', area: '美国', online: true, enabled: false, order: 4 },
        { code: '558325', label: 'four · PP5583RC', bin: '558325', area: '香港', online: true, enabled: false, order: 5 },
        { code: '531711', label: 'three · 11106407', bin: '531711', area: '美国', online: false, enabled: true, order: 6 },
      ],
      policy: {
        localOn: true,
        strict: true,
        switchOnFail: false,
        autoOpen: true,
        maxNew: 4,
        maxCards: 3,
        cooldown: 24,
        area: 'United States',
        holderFirst: 'GPT',
        holderLast: 'Direct',
      },
      health: { enabled: true, threshold: 2, freeze: false, requireEmail: true },
      blocked: [{ id: 88123, last4: '1904', reason: '不同邮箱，判成坏卡', emails: 3, fails: 2, freeze: '未冻结' }],
      failEvents: [
        { at: '10-08 09:12', card: '88123', order: 'up_9188', email: 'a@example.com', verdict: '卡的问题', status: '失败' },
        { at: '10-08 09:40', card: '88123', order: 'up_9201', email: 'b@example.com', verdict: '卡的问题', status: '失败' },
        { at: '10-08 11:02', card: '77210', order: 'up_9304', email: 'same@example.com', verdict: '号的问题', status: '失败' },
      ],
    },
    {
      id: 'avan', name: 'Avanfinity', site: 'https://avanfinity.example', enabled: true,
      gptRole: 'off', // off | backup
      conns: [
        { id: 21, proto: 'avanfinity-2026-08', protoLabel: 'OpenAI · /api/v1 · AppId/Secret', caps: ['gpt'], cred: 'app_7731 / ****', state: 'idle', lastOk: '未启用' },
        { id: 22, proto: 'avanfinity-api-v1', protoLabel: '会员 CDK · /api/v1 · AppId/Secret', caps: ['x_cdk', 'tg_cdk'], cred: 'app_7731 / ****', state: 'warn', lastOk: '2 小时前', error: '买 CDK 被拒：出口 IP 203.0.113.24 不在这个 App 的白名单。X 和 TG 共用这套凭证，会一起失败' },
      ],
      gpt: { spendable: 300.0, reserve: 0, fee: '$1.2 / $5.5 / $11', feeNote: 'Plus / Pro 5x / Pro 20x', circuit: 'closed', fail: 0, unused: 0, ok24: 0, fail24: 0 },
      x: { wallet: 820.4, liability: 96.0, unusedCdk: 12, ok24: 9, fail24: 1 },
      tg: { wallet: 820.4, unused: 4, ok24: 1, fail24: 0 },
      webhook: { secret: false, url: 'https://danew.cc/api/v1/webhooks/card/avan', last: '—' },
      cards: [],
      // X 和 TG 共用这一池已开出的卡。默认从池子里按顺序自动选，不每笔开新卡，也不钉死一张。
      payMode: 'existing',
      payFallbackNew: false,
      fixedCardId: 4585,
      payOpen: { product: 'P5556XV', first: 'X', last: 'Member' },
      payCards: [
        { id: 4585, mask: '4585', product: 'XL537872', balance: 28.75, status: '正常', enabled: false, order: 1 },
        { id: 4572, mask: '4572', product: 'XL537872', balance: 28.19, status: '正常', enabled: false, order: 2 },
        { id: 2438, mask: '2438', product: 'XL537872', balance: 0.10, status: '正常', enabled: false, order: 3 },
        { id: 1260, mask: '1260', product: 'P5556XV', balance: 20.10, status: '正常', enabled: true, order: 4 },
        { id: 7933, mask: '7933', product: 'P5556XV', balance: 0.10, status: '正常', enabled: false, order: 5 },
        { id: 7987, mask: '7987', product: 'P5556XV', balance: 0.10, status: '正常', enabled: true, order: 6 },
        { id: 5710, mask: '5710', product: 'P5556XV', balance: 0.10, status: '正常', enabled: true, order: 7 },
        { id: 1698, mask: '1698', product: 'P5378OX', balance: 0.10, status: '正常', enabled: true, order: 8 },
        { id: 9049, mask: '9049', product: 'P5556XV', balance: 0, status: '删除', enabled: false, order: 9 },
        { id: 6330, mask: '6330', product: 'P5378OX', balance: 0, status: '删除', enabled: false, order: 10 },
      ],
      payBlocked: [],
      calls: [
        { at: '10-09 15:21:28', prod: 'x', m: 'POST', p: '/public/x-cdk/redeem', s: 200, d: '付款已派出' },
        { at: '10-09 15:21:20', prod: 'x', m: 'POST', p: '/public/x-cdk/preflight', s: 200, d: '报价通过' },
        { at: '10-09 15:08:43', prod: 'x', m: 'POST', p: '/public/x-cdk/redeem', s: 200, d: '付款已派出' },
        { at: '10-09 15:04:23', prod: 'x', m: 'POST', p: '/public/x-cdk/preflight', s: 404, d: '接口不存在 · 查了一条上游没有的单' },
        { at: '10-09 15:03:56', prod: 'tg', m: 'POST', p: '/public/tg-cdk/redeem', s: 200, d: '付款已派出' },
        { at: '10-09 15:03:42', prod: 'tg', m: 'POST', p: '/public/tg-cdk/preflight', s: 404, d: '接口不存在 · 查了一条上游没有的单' },
        { at: '10-09 14:58:10', prod: 'x', m: 'POST', p: '/public/x-cdk/redeem', s: 403, d: '出口 IP 不在白名单' },
        { at: '10-09 14:55:01', prod: 'x', m: 'POST', p: '/x-direct/cdks/generate', s: 200, d: '发码，还没扣钱包' },
        { at: '10-09 14:49:10', prod: 'tg', m: 'POST', p: '/public/tg-cdk/redeem', s: 200, d: '付款已派出' },
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
    { key: 'basic_monthly', name: 'Basic · 月付', cost: { spacex: 3.2, avan_cdk: null }, source: 'spacex', on: true, cap: 4 },
    { key: 'basic_yearly', name: 'Basic · 年付', cost: { spacex: 28, avan_cdk: null }, source: 'spacex', on: true, cap: 32 },
    { key: 'premium_monthly', name: 'Premium · 月付', cost: { spacex: 8.2, avan_cdk: 8.1 }, source: 'spacex', on: true, cap: 9.5 },
    { key: 'premium_3m', name: 'Premium · 3 个月', cost: { spacex: 22, avan_cdk: 15 }, source: 'avan_cdk', on: true, cap: 15 },
    { key: 'premium_6m', name: 'Premium · 6 个月', cost: { spacex: null, avan_cdk: 34 }, source: 'avan_cdk', on: true, cap: 0 },
    { key: 'premium_12m', name: 'Premium · 12 个月', cost: { spacex: 82, avan_cdk: 79 }, source: 'avan_cdk', on: true, cap: 0 },
    { key: 'plus_monthly', name: 'Premium+ · 月付', cost: { spacex: 15, avan_cdk: null }, source: 'spacex', on: true, cap: 18 },
    { key: 'plus_3m', name: 'Premium+ · 3 个月', cost: { spacex: null, avan_cdk: 40 }, source: 'avan_cdk', on: true, cap: 0 },
    { key: 'plus_6m', name: 'Premium+ · 6 个月', cost: { spacex: null, avan_cdk: 75 }, source: 'avan_cdk', on: true, cap: 0 },
    { key: 'plus_12m', name: 'Premium+ · 12 个月', cost: { spacex: 150, avan_cdk: 148 }, source: 'avan_cdk', on: true, cap: 0 },
  ],
  xAlerts: { wallet: 50, card: 10, stuck: 30 },

  // Telegram Premium：只走 Avanfinity CDK，客户填用户名。套餐与上游 TgDirectPlanKey 一致。
  tgPlans: [
    { key: 'premium_3m', name: 'Premium · 3 个月', cost: 18.5, on: true, cap: 22, currency: 'bdt', official: 30000 },
    { key: 'premium_6m', name: 'Premium · 6 个月', cost: 34.0, on: true, cap: 0, currency: 'bdt', official: 56000 },
    { key: 'premium_12m', name: 'Premium · 12 个月', cost: 62.0, on: true, cap: 72, currency: 'bdt', official: 108000 },
  ],
  tgAlerts: { wallet: 50, stuck: 30 },
  tgCodes: [
    { id: 801, code: 'DNT-P3M-8K2Q-AC', plan: 'premium_3m', status: 'done', group: 'done', user: 'Jceywjt', amt: '30,000 BDT', fee: '', at: '10-07 05:56', ev: ['05:13 报价 @Jceywjt', '05:55 报价 @Jceywjt', '05:56 客户确认开通', '05:56 第二次 redeem：派发付款'] },
    { id: 800, code: 'DNT-P6M-1AA0-AC', plan: 'premium_6m', status: 'running', group: 'running', user: 'tg_alice', amt: '56,000 BDT', fee: '$0.40', at: '10-07 04:12', ev: ['04:10 报价 @tg_alice', '04:12 客户确认开通', '04:12 注资中'] },
    { id: 799, code: 'DNT-P12-90ZX-AC', plan: 'premium_12m', status: 'todo', group: 'todo', user: 'carol_tg', amt: '108,000 BDT', fee: '', at: '10-06 21:03', ev: ['21:01 报价 @carol_tg', '21:03 客户确认开通', '21:04 上游超时，付款结果不确定'] },
    { id: 798, code: 'DNT-P3M-NEW1-AC', plan: 'premium_3m', status: 'unused', group: 'unused', user: '', amt: '—', fee: '', at: '10-06 18:00', ev: [] },
  ],
  tgBatches: [
    { id: 12, at: '10-06 18:00', plan: 'premium_3m', qty: 10, used: 1, note: '闲鱼' },
  ],

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
    { id: 1, code: 'DNX-F14EC888-A425F3AA-D754A286', plan: 'premium_3m', source: 'avan_cdk', status: 'done', group: 'done', user: 'Jceywjt', msg: '', official: '30,000 BDT', usd: 0, fee: '', note: '', at: '2026-10-07 04:45:36', ev: ['10-07 05:13:06 报价 @Jceywjt', '10-07 05:55:58 报价 @Jceywjt', '10-07 05:56:14 客户确认开通', '10-07 05:56:22 第二次 redeem：派发付款'] },
    { id: 501, code: 'DNX-P1M-7HQ2-AC', plan: 'premium_monthly', source: 'spacex', status: 'running', group: 'running', user: 'alice_dev', msg: 'CDK 已提交，等待开通', official: '—', usd: 8.1, fee: '', note: '', at: '07-28 10:31', ev: ['10:31 客户提交 Cookie', '10:32 等待开通'] },
    { id: 499, code: 'DNX-P12-QQ90-AC', plan: 'premium_12m', source: 'avan_cdk', status: 'todo', group: 'todo', user: 'carol', msg: '上游返回不确定，需要人工确认是否已开通', official: '108,000 BDT', usd: 79, fee: '', note: '客服 #331', at: '07-28 08:12', ev: ['08:12 客户提交 @carol', '08:13 上游超时', '08:43 查询 6 次仍不确定'] },
    { id: 498, code: 'DNX-B1M-PO0A-AC', plan: 'basic_monthly', source: 'spacex', status: 'unused', group: 'unused', user: '', msg: '未兑换', official: '—', usd: 0, fee: '', note: '', at: '07-27 20:00', ev: [] },
    { id: 497, code: 'DNX-PL1-8U7Y-SX', plan: 'plus_monthly', source: 'spacex', status: 'failed', group: 'failed', user: 'dave', msg: 'Cookie 已过期，客户可重新提交', official: '—', usd: 0, fee: '', note: '', at: '07-27 19:30', ev: ['19:30 客户提交 Cookie', '19:30 SpaceX 返回 cookie_invalid'] },
  ],
  xBatches: [
    { id: 31, at: '10-07 04:40', plan: 'premium_3m', source: 'avan_cdk', qty: 1, used: 1, note: '' },
    { id: 30, at: '07-27 15:00', plan: 'premium_monthly', source: 'spacex', qty: 10, used: 9, note: '' },
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
    plat: 'avan', platTab: 'pay', platSide: 'avan',
    payShow: { parked: false, dead: false },
    callFilter: 'all',
    xTab: 'issue', xIssue: { plan: 'premium_3m', qty: 1, note: '', region: '日本' }, xIssued: [], xGroup: 'all', xQ: '', xList: { q: '', group: 'all', plan: '', page: 1 }, xRecPage: 1, xSelIds: [],
    tgTab: 'issue', tgIssue: { plan: 'premium_3m', qty: 1, note: '' }, tgIssued: [], tgGroup: 'all', tgQ: '', tgList: { q: '', group: 'all', plan: '', page: 1 }, tgRecPage: 1, tgSelIds: [],
    cdkTab: 'site', cdkIssue: { plan: 'plus', region: '菲律宾 PHP', qty: 10, note: '', dual: false }, cdkIssued: [], cdkFilter: { status: '', kind: '' },
    ordFilter: { product: '', plat: '', st: '' }, ordSel: null,
    redeem: { step: 1, code: '', info: null, cred: '', err: '' },
    skin: 'default',
  },
}
