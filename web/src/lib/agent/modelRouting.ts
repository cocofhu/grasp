import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConcreteModelBinding, CredentialItem, ModelBinding, ModelConfiguration, ModelConnection, ModelProtocol, ToolCapability } from '@/lib/api/api'

export const MODEL_RUNTIME_ENV_KEYS = new Set(['ACP_BRIDGE_MODEL', 'ACP_BRIDGE_REASONING_EFFORT', 'GRASP_OPENCODE_PROVIDER', 'GRASP_OPENCODE_BASE_URL', 'GRASP_OPENCODE_MODEL_VISION'])
export const MODEL_PROTOCOLS: ModelProtocol[] = ['openai-chat-completions', 'openai-responses', 'anthropic-messages']
export const MODEL_PROVIDER_PRESETS: { id: string; label: string; protocol: ModelProtocol; baseUrl: string; scheme: 'bearer' | 'x-api-key'; docsUrl?: string }[] = [
  { id: 'deepseek', label: 'DeepSeek', protocol: 'openai-chat-completions', baseUrl: 'https://api.deepseek.com/v1', scheme: 'bearer' },
  { id: 'anthropic', label: 'Anthropic', protocol: 'anthropic-messages', baseUrl: 'https://api.anthropic.com', scheme: 'x-api-key' },
  { id: 'openai', label: 'OpenAI', protocol: 'openai-responses', baseUrl: 'https://api.openai.com/v1', scheme: 'bearer' },
  { id: 'moonshot', label: 'Moonshot / Kimi', protocol: 'anthropic-messages', baseUrl: 'https://api.moonshot.ai/anthropic', scheme: 'bearer' },
  { id: 'zai', label: 'Z.ai / GLM', protocol: 'anthropic-messages', baseUrl: 'https://api.z.ai/api/anthropic', scheme: 'bearer' },
  // Standard API endpoints. Coding/Token Plan keys can require a different base URL.
  { id: 'qwen', label: 'Qwen / 阿里云百炼', protocol: 'openai-chat-completions', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', scheme: 'bearer', docsUrl: 'https://help.aliyun.com/zh/model-studio/base-url' },
  { id: 'doubao', label: 'Doubao / 火山方舟', protocol: 'openai-chat-completions', baseUrl: 'https://ark.cn-beijing.volces.com/api/v3', scheme: 'bearer', docsUrl: 'https://docs.volcengine.com/docs/ark/base-url-and-authentication?lang=zh' },
  { id: 'siliconflow', label: 'SiliconFlow / 硅基流动', protocol: 'openai-chat-completions', baseUrl: 'https://api.siliconflow.cn/v1', scheme: 'bearer', docsUrl: 'https://docs.siliconflow.cn/docs/userguide/quickstart' },
  { id: 'minimax', label: 'MiniMax', protocol: 'openai-chat-completions', baseUrl: 'https://api.minimax.cn/v1', scheme: 'bearer', docsUrl: 'https://platform.minimax.cn/docs/api-reference/text-openai-api' },
  { id: 'zhipu', label: '智谱 / GLM', protocol: 'openai-chat-completions', baseUrl: 'https://open.bigmodel.cn/api/paas/v4', scheme: 'bearer', docsUrl: 'https://docs.bigmodel.cn/cn/guide/develop/openai/introduction' },
  { id: 'google', label: 'Google / Gemini', protocol: 'openai-chat-completions', baseUrl: 'https://generativelanguage.googleapis.com/v1beta/openai', scheme: 'bearer', docsUrl: 'https://ai.google.dev/gemini-api/docs/openai' },
  { id: 'openrouter', label: 'OpenRouter', protocol: 'openai-chat-completions', baseUrl: 'https://openrouter.ai/api/v1', scheme: 'bearer', docsUrl: 'https://openrouter.ai/docs/api_reference/authentication' },
  { id: 'xai', label: 'xAI / Grok', protocol: 'openai-chat-completions', baseUrl: 'https://api.x.ai/v1', scheme: 'bearer', docsUrl: 'https://docs.x.ai/developers/model-capabilities/legacy/chat-completions' },
  { id: 'hunyuan', label: '腾讯混元 / TokenHub', protocol: 'openai-chat-completions', baseUrl: 'https://tokenhub.tencentmaas.com/v1', scheme: 'bearer', docsUrl: 'https://cloud.tencent.com/document/product/1823/130079' },
  { id: 'qianfan', label: '百度千帆', protocol: 'openai-chat-completions', baseUrl: 'https://qianfan.baidubce.com/v2', scheme: 'bearer', docsUrl: 'https://ai.baidu.com/ai-doc/WENXINWORKSHOP/2m3fihw8s' },
]
export function modelProviderPreset(id: string) { return MODEL_PROVIDER_PRESETS.find((preset) => preset.id === id) }

export function credentialUsable(item: CredentialItem): boolean {
  return item.configured && item.enabled !== false && !item.revokedAt && item.state !== 'cleared' && item.state !== 'revoked'
}

export function isModelCredential(item: CredentialItem): boolean {
  return item.purpose === 'model_api'
}
export function isToolCredential(item: CredentialItem, toolId: string): boolean {
  return item.purpose === 'tool_auth' && item.toolId === toolId
}
export function connectionCapabilities(capabilities: ToolCapability[], toolId: string, connection?: ModelConnection) {
  if (toolId === 'cursor' || toolId === 'trae') return []
  if (connection) {
    if (connection.auth.scheme === 'custom-header') return []
    if (connection.protocol === 'anthropic-messages' && toolId !== 'claude_code' && connection.auth.scheme !== 'x-api-key') return []
    if (connection.protocol !== 'anthropic-messages' && connection.auth.scheme !== 'bearer') return []
  }
  return capabilities.filter((cap) => cap.toolId === toolId && cap.adapterState === 'existing' && cap.modes.includes('connection') && (!connection || cap.protocols?.includes(connection.protocol)))
}
export function bindingProblem(binding: ModelBinding | ConcreteModelBinding | undefined, toolId: string, connections: ModelConnection[], credentials: CredentialItem[], capabilities: ToolCapability[], configurations: ModelConfiguration[] = []): string {
  if (binding?.kind === 'configuration') {
    const configuration = configurations.find((item) => item.id === binding.configurationId)
    if (!configuration || configuration.state !== 'active') return 'missingConfiguration'
    if (configuration.backend !== toolId) return 'configurationBackendMismatch'
    return bindingProblem(configuration.binding, toolId, connections, credentials, capabilities)
  }
  if (!binding) return 'missingConfiguration'
  if (binding.kind === 'platform') return credentials.some((item) => item.id === binding.credentialId && credentialUsable(item) && isToolCredential(item, toolId)) ? '' : 'missingCredential'
  const connection = connections.find((item) => item.id === binding.connectionId)
  if (!connection || connection.state !== 'active') return 'missingConnection'
  if (!credentials.some((item) => item.id === connection.credentialId && credentialUsable(item) && isModelCredential(item))) return 'missingCredential'
  const caps = connectionCapabilities(capabilities, toolId, connection)
  if (!caps.length) return 'unsupported'
  if (!binding.modelId.trim() || !connection.models.some((model) => model.id === binding.modelId)) return 'missingModel'
  const roles = binding.toolSettings?.modelRoles || {}
  if (Object.values(roles).some((model) => model && !connection.models.some((entry) => entry.id === model))) return 'missingModel'
  if (roles.main && roles.main !== binding.modelId) return 'missingModel'
  if (caps.some((cap) => (cap.requiredRoles || []).some((role) => !roles[role as keyof typeof roles]))) return 'requiredRoles'
  return ''
}

const zh = {
  close: '关闭', addCredential: '新增凭据', advancedSettings: '高级设置', chooseCredential: '选择凭据', noCredentialsForTool: '此编码工具还没有可用凭据。', needsSetup: '待完善', configuration: '模型配置', selectConfiguration: '选择模型配置', noConfigurations: '当前工具还没有可用的模型配置。', missingConfiguration: '模型配置不存在或已停用。', configurationBackendMismatch: '模型配置的编码工具与当前 Agent 不匹配。', addConfiguration: '新增模型', editConfiguration: '编辑模型', configurationHint: '把编码工具、凭据和所选模型保存为一条有名称的配置，供 Agent 绑定使用。', configurationSaved: '模型配置已保存', configurationRemoved: '模型配置已删除', configurationName: '模型名称', credentialOrService: '凭据 / 服务', platformCredentials: '工具平台凭据', serviceCredentials: '模型服务凭据', manualModelHint: '可以选择已有模型或填写服务商原始模型 ID，保留其中的 /。', configurationDeleteConfirm: '删除后，绑定此模型配置的 Agent 将无法运行。确认删除？', immutableTool: '保存后编码工具不能更改；需要换工具时请新建模型配置。', chooseCredentialFirst: '先选择当前工具的凭据或兼容服务。', configurationUnavailable: '该模型配置的凭据或服务已失效，请编辑修复。', modelsTab: '模型', editService: '编辑 API 账号', customProvider: '自定义服务商', providerId: '服务商标识', providerLabel: '服务商名称', connectionName: 'API 配置名称', configureConnectionNow: '配置 API 地址', keyOnlyHint: '仅保存密钥，可稍后在 API 账号中配置地址。', keySavedConnectionFailed: 'API 账号已保存，API 地址保存失败。重试不会重复添加账号。', retryConnection: '重试保存 API 地址', savedKey: '密钥已保存', credentials: '凭据', loading: '加载中…', retry: '重试', name: '名称', provider: '服务商', protocol: '协议', baseUrl: '基础地址', credential: 'API 账号', auth: '鉴权方式', headerName: '请求头名称', models: '可用模型', modelsHint: '每行填写一个服务商原始模型 ID，允许包含 /。可以稍后补充。', region: '区域（可选）', product: '产品或套餐（可选）', details: '连接信息', chooseService: '选择服务', review: '检查并保存', next: '下一步', back: '上一步', save: '保存', cancel: '取消', edit: '编辑', remove: '删除', disable: '停用', enable: '启用', disabled: '已停用', active: '已启用', unverified: '未验证', noModels: '尚未添加模型，配置模型后才能用于 Agent。', noKey: '请先添加 API 账号。', selectKey: '选择 API 账号', connectionSaved: 'API 账号已保存', deleteConfirm: '删除后引用此连接的 Agent 将无法运行。确认删除？', compatibleTools: '可用工具', pendingAdapter: '当前没有可用的工具适配', source: '模型来源', platform: '工具平台授权', connection: 'API 账号', platformHint: '使用当前工具的平台授权和模型。', connectionSourceHint: '选择当前编码工具支持的 API 账号。', noByok: '当前工具使用平台授权，不开放直连第三方模型服务。', noCapability: '当前工具尚未发布可用的连接适配。', selectCredential: '选择工具平台授权', selectConnection: '选择 API 账号', model: '主模型', autoModel: '由工具选择默认模型', selectModel: '选择模型', auxiliary: '辅助任务模型', followMain: '与主模型相同', chooseTool: '编码工具', missingCredential: '授权或模型密钥未配置、已清除或已撤销。', missingConnection: 'API 账号的地址配置不可用，请刷新后重试。', unsupported: '当前工具不支持此连接协议或尚未适配。', missingModel: '请选择模型，并检查辅助模型。', requiredRoles: '请为必需的辅助任务选择模型。', choose: '请选择', modelKeys: 'API 账号', addKey: '新增 API 账号', issuer: '密钥发行方', keyHint: '保存 API 账号的密钥，也可同时配置 API 地址。', secret: 'API Key / Token', noKeys: '还没有 API 账号', toolAuth: '工具平台授权', role_reasoning: '推理任务', routeProfile: '工具路由', standardRoute: '通用协议', inputs: '输入类型（可选）', contextWindow: '上下文窗口（可选）', maxTokens: '最大输出 Token（可选）', modelDetails: '模型能力（按供应商实际规格填写）', role_main: '主任务', role_fast: '快速任务', role_subagent: '子 Agent', role_opus: 'Opus 档位', role_sonnet: 'Sonnet 档位', role_haiku: 'Haiku 档位', configureKeys: '管理 API 账号', invalidForm: '请检查名称、地址、鉴权和密钥。', documented: '文档依据', toolChanged: '切换工具后，请重新选择该工具的模型配置。', modelCount: '个模型', revision: '修订',
}
const en: Record<keyof typeof zh, string> = {
  close: 'Close', addCredential: 'Add credential', advancedSettings: 'Advanced settings', chooseCredential: 'Choose credential', noCredentialsForTool: 'No credential is available for this coding tool yet.', needsSetup: 'Needs setup', configuration: 'Model configuration', selectConfiguration: 'Select model configuration', noConfigurations: 'No active model configuration is available for this tool.', missingConfiguration: 'The model configuration is missing or disabled.', configurationBackendMismatch: 'The model configuration uses a different coding tool than this Agent.', addConfiguration: 'Add model', editConfiguration: 'Edit model', configurationHint: 'Save a named combination of coding tool, credential and selected model for Agents to use.', configurationSaved: 'Model configuration saved', configurationRemoved: 'Model configuration deleted', configurationName: 'Model name', credentialOrService: 'Credential / service', platformCredentials: 'Tool platform credential', serviceCredentials: 'Model service credential', manualModelHint: 'Select a model or enter its exact provider ID, including any /.', configurationDeleteConfirm: 'Agents using this model configuration will no longer run. Delete it?', immutableTool: 'The coding tool cannot change after saving. Create another configuration to use a different tool.', chooseCredentialFirst: 'Select this tool’s authorization or a compatible service first.', configurationUnavailable: 'The credential or service is unavailable. Edit this model configuration to repair it.', modelsTab: 'Models', editService: 'Edit API account', customProvider: 'Custom provider', providerId: 'Provider ID', providerLabel: 'Provider name', connectionName: 'API configuration name', configureConnectionNow: 'Configure API endpoint', keyOnlyHint: 'Save the key now and configure the API address in this account later.', keySavedConnectionFailed: 'The API account was saved, but its API address could not be saved. Retrying does not create another account.', retryConnection: 'Retry saving API address', savedKey: 'Key saved', credentials: 'Credentials', loading: 'Loading…', retry: 'Retry', name: 'Name', provider: 'Provider', protocol: 'Protocol', baseUrl: 'Base URL', credential: 'API account', auth: 'Authentication', headerName: 'Header name', models: 'Available models', modelsHint: 'One original provider model ID per line. IDs may contain /. You can add models later.', region: 'Region (optional)', product: 'Product or plan (optional)', details: 'Connection details', chooseService: 'Choose service', review: 'Review and save', next: 'Next', back: 'Back', save: 'Save', cancel: 'Cancel', edit: 'Edit', remove: 'Delete', disable: 'Disable', enable: 'Enable', disabled: 'Disabled', active: 'Active', unverified: 'Not verified', noModels: 'Add models before using this connection in an Agent.', noKey: 'Add an API account first.', selectKey: 'Select API account', connectionSaved: 'API account saved', deleteConfirm: 'Agents referencing this connection will no longer run. Delete it?', compatibleTools: 'Available tools', pendingAdapter: 'No available tool adapter', source: 'Model source', platform: 'Tool platform', connection: 'API account', platformHint: 'Use this tool’s platform authorization and models.', connectionSourceHint: 'Select an API account supported by this coding tool.', noByok: 'This tool uses platform authorization; direct third-party connections are unavailable.', noCapability: 'No connection adapter is currently available for this tool.', selectCredential: 'Select tool authorization', selectConnection: 'Select API account', model: 'Main model', autoModel: 'Tool default', selectModel: 'Select model', auxiliary: 'Auxiliary task models', followMain: 'Same as main model', chooseTool: 'Coding tool', missingCredential: 'The authorization or model key is missing, cleared, or revoked.', missingConnection: 'The API account address is unavailable. Refresh and try again.', unsupported: 'This protocol is unsupported or its adapter is unavailable.', missingModel: 'Select a model and check auxiliary models.', requiredRoles: 'Choose models for the required auxiliary tasks.', choose: 'Select', modelKeys: 'API accounts', addKey: 'Add API account', issuer: 'Key issuer', keyHint: 'Save an API account key and optionally configure its API address.', secret: 'API Key / Token', noKeys: 'No API accounts yet', toolAuth: 'Tool platform authorization', role_reasoning: 'Reasoning tasks', routeProfile: 'Tool route', standardRoute: 'Standard protocol', inputs: 'Input types (optional)', contextWindow: 'Context window (optional)', maxTokens: 'Maximum output tokens (optional)', modelDetails: 'Model capabilities (use actual provider specifications)', role_main: 'Main task', role_fast: 'Fast tasks', role_subagent: 'Subagents', role_opus: 'Opus tier', role_sonnet: 'Sonnet tier', role_haiku: 'Haiku tier', configureKeys: 'Manage API accounts', invalidForm: 'Check the name, URL, authentication and key.', documented: 'Documentation', toolChanged: 'Select a model configuration for the new coding tool.', modelCount: 'models', revision: 'Revision',
}
export function useRoutingCopy() {
  const { locale } = useI18n()
  return computed(() => locale.value.startsWith('en') ? en : zh)
}
