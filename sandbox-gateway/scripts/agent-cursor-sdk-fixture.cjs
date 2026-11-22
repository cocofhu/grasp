// @cursor/sdk stand-in for the agent-switching E2E image. Cursor's bridge
// requires the package synchronously, so the ESM double loads on first use.
const fixture = () => import(process.env.GRASP_CURSOR_SDK_FIXTURE || '/opt/grasp-agent-e2e/agent-platform-wire-fixture.mjs').then((m) => m.cursorSdk)

module.exports = {
  Agent: {
    create: async (options) => (await fixture()).Agent.create(options),
    resume: async (agentId, options) => (await fixture()).Agent.resume(agentId, options),
  },
  Cursor: {models: {list: async (options) => (await fixture()).Cursor.models.list(options)}},
  createAgentPlatform: async (options) => (await fixture()).createAgentPlatform(options),
}
