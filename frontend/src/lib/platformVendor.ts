// 卡台按厂商归类，以及 GPT 主/备角色判定。卡台页侧栏和「产品 × 卡台」矩阵共用。

export type Vendor = 'spacex' | 'avan'

export interface VendorAcc {
  id: number
  protocol: string
  status: string
  priority: number
  serves_openai: boolean
  is_primary_default: boolean
  capabilities: string
}

export function vendorOf(protocol: string): Vendor {
  return protocol.startsWith('avanfinity') ? 'avan' : 'spacex'
}

/** GPT 角色：排序第一且启用的是主台，其余启用的是备台，停用的不参与。 */
export function gptRoleOf(a: VendorAcc, openaiSorted: VendorAcc[]): 'primary' | 'backup' | 'off' {
  if (!a.serves_openai || a.status !== 'active') return 'off'
  const firstActive = openaiSorted.find((x) => x.status === 'active')
  if (a.is_primary_default) return 'primary'
  if (!openaiSorted.some((x) => x.is_primary_default && x.status === 'active') && firstActive?.id === a.id) return 'primary'
  return 'backup'
}
