import { describe, expect, it } from 'vitest'
import { isXPremiumPlan, xPremiumCredential } from '../x-premium'
import { planLabel, planSatisfied } from '../plan'
import { extractCdkSession } from '../batch-session'
describe('X subscriptions', () => {
  const raw = JSON.stringify({ auth_token: 'a'.repeat(40), ct0: 'b'.repeat(64), billing_email: 'fixture@example.test' }, null, 2)
  it('accepts X JSON for redemption without weakening GPT access-token checks', () => {
    expect(xPremiumCredential(raw)).toBe(raw)
    expect(extractCdkSession(raw)).toBe(raw)
    expect(extractCdkSession('eyJ.fixture.token')).toBe('')
    expect(xPremiumCredential(JSON.stringify({ auth_token: 'a'.repeat(40), ct0: 'b'.repeat(64), billing_email: 'x\r\ny@example.test' }))).toBe('')
  })
  it('does not display Premium+ as ChatGPT Plus or consider GPT subscriptions an X subscription', () => {
    expect(isXPremiumPlan('premium_plus_yearly')).toBe(true)
    expect(planLabel('premium_plus_yearly')).toContain('X Premium+')
    expect(planSatisfied('pro', 'premium_monthly')).toBe(false)
    expect(planSatisfied('x_basic_monthly', 'plus')).toBe(false)
    expect(planSatisfied('free', 'basic_monthly')).toBe(false)
    expect(planSatisfied('x_basic_monthly', 'premium_monthly')).toBe(true)
  })
})
