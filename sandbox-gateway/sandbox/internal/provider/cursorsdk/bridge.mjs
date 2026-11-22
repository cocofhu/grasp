// Grasp's bridge to @cursor/sdk: one local Cursor agent per process, driven
// by the sandbox backend over stdio. Commands arrive on stdin as JSON lines
// {id, type, ...} and are answered {type:"response", id, ok, data|error};
// everything else on stdout is an event of the running turn.
import { createRequire } from "node:module";
import { createInterface } from "node:readline";

const writeOut = process.stdout.write.bind(process.stdout);
// stdout carries protocol frames only; stray SDK logging goes to stderr.
process.stdout.write = process.stderr.write.bind(process.stderr);
console.log = console.info = console.debug = console.warn = console.error;
process.stdout.on("error", () => {});
process.stderr.on("error", () => {});

const emit = (frame) => writeOut(JSON.stringify(frame) + "\n");

const sdkDir = process.env.GRASP_CURSOR_SDK_DIR || "/opt/grasp/cursor-sdk";
const sdk = createRequire(`${sdkDir}/package.json`)("@cursor/sdk");

let agent;
let agentOptions;
let cwd;
// The running turn: the run started by send, then each follow-up run the
// server queued from a steer. The turn ends when the last of them ends.
let turn;

function errorText(err) {
  if (err === undefined || err === null) return "";
  if (typeof err === "string") return err;
  const code = err.code ? ` (${err.code})` : "";
  return `${err.name && err.name !== "Error" ? `${err.name}: ` : ""}${err.message ?? String(err)}${code}`;
}

async function open({ agentId, cwd: dir, model, mcpServers, apiKey }) {
  cwd = dir;
  agentOptions = {
    apiKey,
    model,
    // The platform has no one to answer the agent's own questions.
    disallowedTools: ["askQuestion"],
    ...(mcpServers && Object.keys(mcpServers).length > 0 ? { mcpServers } : {}),
    local: {
      cwd: dir,
      // Team, MDM and plugin settings sync from the account at start and can
      // block the first turn; project rules and the user's config still load.
      settingSources: ["project", "user"],
      enableAgentRetries: true,
      onConnectionStateChange: (state) => emit({ type: "connection", ...state }),
    },
  };
  let resumeError;
  if (agentId) {
    try {
      agent = await sdk.Agent.resume(agentId, agentOptions);
      return { agentId: agent.agentId, resumed: true };
    } catch (err) {
      resumeError = errorText(err);
    }
  }
  agent = await sdk.Agent.create(agentOptions);
  return { agentId: agent.agentId, resumed: false, ...(resumeError ? { resumeError } : {}) };
}

// checkpointed reports whether the run reached the conversation: Cursor keeps
// a stopped run only from its first checkpoint on.
async function checkpointed(runId) {
  try {
    const platform = await sdk.createAgentPlatform({ workspaceRef: cwd });
    const run = await platform.store.getRun(agent.agentId, runId);
    const latest = run?.latestCheckpointRef?.blobId;
    return latest !== undefined && latest !== run?.startCheckpointRef?.blobId;
  } catch {
    return undefined;
  }
}

function addUsage(a, b) {
  if (!a) return { ...b };
  const out = { ...a };
  for (const [k, v] of Object.entries(b)) if (typeof v === "number") out[k] = (out[k] ?? 0) + v;
  return out;
}

function follow(t, run) {
  t.runs.push(run);
  t.current = run;
  run
    .wait()
    .then(
      (result) => ({ result }),
      (err) => ({ error: err }),
    )
    .then(async ({ result, error }) => {
      if (result?.usage) t.usage = addUsage(t.usage, result.usage);
      if (t.current !== run) return;
      const status = error ? "error" : result.status;
      const frame = { type: "run_end", runId: run.id, status };
      if (t.usage) frame.usage = t.usage;
      const err = error ?? result.error;
      if (err) frame.error = errorText(err);
      if (status === "cancelled") {
        const kept = await checkpointed(run.id);
        if (kept !== undefined) frame.checkpointed = kept;
      }
      if (turn === t) turn = undefined;
      emit(frame);
    });
}

async function send({ text, images }) {
  if (!agent) throw new Error("agent is not open");
  if (turn) throw new Error("a turn is already running");
  const t = { runs: [] };
  turn = t;
  try {
    const message = images?.length ? { text, images } : text;
    const run = await agent.send(message, {
      onDelta: ({ update }) => {
        if (turn === t) emit({ type: "delta", update });
      },
    });
    follow(t, run);
    return { runId: run.id };
  } catch (err) {
    if (turn === t) turn = undefined;
    throw err;
  }
}

async function steer({ text }) {
  const run = turn?.current;
  if (!run) return { outcome: "no_turn" };
  if (typeof run.steer !== "function") return { outcome: "revert_to_followup" };
  const outcome = await run.steer(text);
  if (typeof outcome === "string") return { outcome };
  if (outcome?.outcome === "queued" && outcome.run) {
    follow(turn, outcome.run);
    return { outcome: "queued" };
  }
  // A follow-up the server queued without naming its run would go unseen.
  return { outcome: "revert_to_followup" };
}

async function cancel() {
  const run = turn?.current;
  if (!run) return null;
  try {
    await run.cancel();
  } catch (err) {
    if (run.status === "running") throw err;
  }
  return null;
}

async function models({ apiKey }) {
  return sdk.Cursor.models.list({ apiKey });
}

const handlers = { open, send, steer, cancel, models };

async function handle(line) {
  let req;
  try {
    req = JSON.parse(line);
  } catch {
    return;
  }
  if (req.type === "close") {
    try {
      await turn?.current?.cancel();
    } catch {}
    try {
      agent?.close();
    } catch {}
    emit({ type: "response", id: req.id, ok: true });
    process.exit(0);
  }
  const fn = handlers[req.type];
  if (!fn) {
    emit({ type: "response", id: req.id, ok: false, error: `unknown command ${req.type}` });
    return;
  }
  try {
    const data = await fn(req);
    emit({ type: "response", id: req.id, ok: true, data: data ?? null });
  } catch (err) {
    emit({ type: "response", id: req.id, ok: false, error: errorText(err) });
  }
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on("line", (line) => {
  if (line.trim()) void handle(line);
});
rl.on("close", () => {
  try {
    agent?.close();
  } catch {}
  process.exit(0);
});
