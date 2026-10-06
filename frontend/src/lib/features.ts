// 功能开关（构建期）。代理功能已停用：默认隐藏代理端 /partner、代理换码及后台入口，
// 代码、数据和后端接口全部保留。需要恢复时构建前设 VITE_ENABLE_AGENT=true。
export const AGENT_ENABLED = import.meta.env.VITE_ENABLE_AGENT === 'true'

// X 直充（Avan x_direct）和 X 付款卡界面：业务上只用 CDK，界面隐藏，代码和接口保留。
// 恢复时构建前设 VITE_ENABLE_X_DIRECT=true。
export const X_DIRECT_UI = import.meta.env.VITE_ENABLE_X_DIRECT === 'true'
