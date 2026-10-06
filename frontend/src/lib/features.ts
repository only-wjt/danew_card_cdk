// 功能开关（构建期）。代理功能已停用：默认隐藏代理端 /partner、代理换码及后台入口，
// 代码、数据和后端接口全部保留。需要恢复时构建前设 VITE_ENABLE_AGENT=true。
export const AGENT_ENABLED = import.meta.env.VITE_ENABLE_AGENT === 'true'
