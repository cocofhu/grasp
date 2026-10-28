#!/usr/bin/env python3
"""Real Grasp/API/runtime/MCP E2E with native CLIs and a free model server.

Only Cursor/Trae use explicitly labelled proprietary-platform wire doubles.
Every other CLI is real. No vendor key is read, inherited or required.
"""
import argparse
import http.cookiejar
import itertools
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
DEMO_HASH = '$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi'


def directed_walk(names):
    """Euler walk covers all N*(N-1) ordered switches in N*(N-1)+1 turns."""
    edges = {name: [other for other in names if other != name] for name in names}
    stack, walk = [names[0]], []
    while stack:
        if edges[stack[-1]]:
            stack.append(edges[stack[-1]].pop())
        else:
            walk.append(stack.pop())
    return list(reversed(walk))


def validate_inventory(tools, capabilities):
    ids = [tool['id'] for tool in tools]
    supported = {entry['toolId'] for entry in capabilities}
    if len(set(ids)) != len(ids) or set(ids) != supported:
        raise AssertionError(f'E2E inventory differs from product capabilities: missing={sorted(supported-set(ids))}, extra={sorted(set(ids)-supported)}')
    for tool in tools:
        if tool['mode'] not in ['native-cli', 'platform-wire-fixture']:
            raise AssertionError(f'unknown E2E coverage mode: {tool["id"]}')
        if tool['mode'] == 'platform-wire-fixture' and not tool.get('reason'):
            raise AssertionError('a platform double must state the missing native coverage')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def history_tool_completed(message):
    """Require the runtime's parsed native MCP events as well as HTTP evidence."""
    history_calls, completed = set(), set()
    for event in message.get('events', []):
        if isinstance(event, str):
            event = json.loads(event)
        update = (event.get('data') or event.get('params') or {}).get('update') or {}
        if update.get('sessionUpdate') == 'tool_call':
            raw = update.get('rawInput') or {}
            if isinstance(raw, str):
                try:
                    raw = json.loads(raw)
                except ValueError:
                    raw = {}
            name = str(update.get('title', ''))+' '+str(raw.get('toolName', '') if isinstance(raw, dict) else '')
            if 'get_history' in name:
                history_calls.add(update.get('toolCallId'))
        if update.get('sessionUpdate') == 'tool_call_update' and update.get('status') == 'completed':
            completed.add(update.get('toolCallId'))
    return bool(history_calls & completed)


class E2E:
    def __init__(self, args):
        self.args = args
        self.output = Path(args.output).resolve()
        self.output.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.data = Path(tempfile.mkdtemp(prefix='grasp-agent-e2e-'))
        self.base = f'http://127.0.0.1:{free_port()}'
        self.processes, self.handles, self.sessions, self.cases = [], [], [], []
        self.client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.tools = json.loads((HERE/'agent-e2e-matrix.json').read_text())['tools']
        self.groups = {}
        self.last_messages = []
        self.run = 'switch-'+secrets.token_hex(6)
        self.nonce = 'task-'+secrets.token_hex(12)
        self.marker = 'checkpoint.txt'

    def request(self, path, body=None, method=None, base=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request((base or self.base)+path, data, {'Content-Type': 'application/json'}, method=method or ('POST' if body is not None else 'GET'))
        try:
            with self.client.open(req, timeout=20) as response:
                raw = response.read(8_000_000)
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            # Endpoints and all credentials in this isolated suite are
            # synthetic. Keep the server's stable error code for diagnosis.
            detail = error.read(1500).decode(errors='replace')
            error.msg = str(error.msg)+' '+path+' '+detail
            raise

    def launch(self, argv, env, name):
        handle = (self.output/(name+'.log')).open('w')
        self.handles.append(handle)
        proc = subprocess.Popen(argv, cwd=ROOT, env=env, stdout=handle, stderr=subprocess.STDOUT, start_new_session=True)
        self.processes.append(proc)
        return proc

    def wait(self, condition, label, seconds=60):
        deadline = time.monotonic()+seconds
        while time.monotonic() < deadline:
            for proc in self.processes:
                if proc.poll() is not None:
                    raise AssertionError(f'a managed E2E process exited ({proc.returncode}); see logs')
            try:
                value = condition()
                if value:
                    return value
            except (urllib.error.URLError, ConnectionError, TimeoutError):
                pass
            time.sleep(0.2)
        raise AssertionError(f'timed out waiting for {label}')

    def start(self):
        model_port = free_port()
        self.model = f'http://127.0.0.1:{model_port}'
        # Deliberately drop host API keys, login tokens, proxy credentials and
        # CI secrets. The only credentials used below are synthetic localhost data.
        env = {key: value for key, value in os.environ.items() if key in ['PATH', 'HOME', 'LANG', 'LC_ALL', 'TMPDIR', 'TZ']}
        self.launch(['node', str(HERE/'mock-agent-model.mjs'), '--listen', '127.0.0.1', '--port', str(model_port), '--report-file', str(self.output/'model-report.json')], env, 'model')
        self.wait(lambda: self.request('/health', base=self.model), 'mock model')
        fixture_bin = self.data/'platform-wire-bin'
        fixture_bin.mkdir()
        for tool in self.tools:
            if tool['mode'] == 'platform-wire-fixture':
                script = fixture_bin/tool['bin']
                script.write_text('#!/bin/sh\nexec node '+shlex.quote(str(HERE/'agent-platform-wire-fixture.mjs'))+' '+shlex.quote(tool['id'])+' "$@"\n')
                script.chmod(0o755)
            elif not shutil.which(tool['bin']):
                raise AssertionError(f'required real CLI missing: {tool["id"]} ({tool["bin"]}); no skip or fake fallback is permitted')
        env.update({'PATH': str(fixture_bin)+os.pathsep+env['PATH'], 'GRASP_EXECUTION_MODE': 'worktree', 'GRASP_WORKTREE_ROOT': str(self.data/'launches'), 'GRASP_WORKTREE_BACKEND': str(Path(self.args.runtime).resolve()/'bin/backend'), 'GRASP_BLOBS_ROOT': str(self.data/'blobs'), 'GIT_ASKPASS': '/bin/false', 'GIT_TERMINAL_PROMPT': '0'})
        port = int(self.base.rsplit(':', 1)[1])
        config = {'server': {'port': port, 'deployment_mode': 'local-demo', 'mcp_advertise': self.base, 'public_advertise': self.base}, 'database': {'path': str(self.data/'grasp.db')}, 'engine': {'profiles_root': str(self.data/'profiles')}, 'sandbox': {'work_dir': str(self.data/'configs'), 'chat_idle_timeout_seconds': 90}, 'auth': {'users': [{'username': 'admin', 'password_hash': DEMO_HASH, 'is_admin': True}]}}
        config_path = self.data/'config.json'
        config_path.write_text(json.dumps(config)); config_path.chmod(0o600)
        self.launch([str(Path(self.args.server).resolve()), str(config_path)], env, 'server')
        self.wait(lambda: self.request('/api/health').get('ready'), 'Grasp readiness')
        self.request('/api/auth/login', {'username': 'admin', 'password': 'demo1234'})
        validate_inventory(self.tools, self.request('/api/workspace/tool-capabilities')['items'])
        if self.args.tools:
            wanted = self.args.tools.split(',')
            self.tools = [tool for tool in self.tools if tool['id'] in wanted]
            if {tool['id'] for tool in self.tools} != set(wanted):
                raise AssertionError('unknown --tools selection')

    def seed(self):
        credential = self.request('/api/workspace/credentials', {'purpose': 'model_api', 'issuer': 'custom', 'name': 'E2E localhost only', 'secretKind': 'api_key', 'value': 'synthetic-local-model-key'})
        connections = {}
        for protocol in {tool['protocol'] for tool in self.tools if tool['mode'] == 'native-cli'}:
            models = [{'id': model, 'name': model, 'source': 'manual', 'capabilities': {'toolCalls': True, 'reasoning': True}, 'reasoningEfforts': ['low', 'high'], 'contextWindow': 200000, 'maxTokens': 4096} for model in ['gpt-5', 'claude-sonnet-4-6', 'e2e-auxiliary']]
            # OpenCode's documented effort catalog is selected explicitly by
            # provider, never inferred from a model name at a custom endpoint.
            provider = 'openai' if protocol == 'openai-chat-completions' else 'custom'
            connections[protocol] = self.request('/api/workspace/model-connections', {'name': 'Local '+protocol, 'providerId': provider, 'protocol': protocol, 'baseUrl': self.model+'/v1', 'credentialId': credential['id'], 'auth': {'scheme': 'x-api-key' if protocol == 'anthropic-messages' else 'bearer'}, 'models': models, 'state': 'active'})['id']
        for tool in self.tools:
            if tool['mode'] == 'native-cli':
                binding = {'kind': 'connection', 'connectionId': connections[tool['protocol']], 'modelId': tool['model'], 'auxiliaryModelIds': ['e2e-auxiliary']}
            else:
                key = self.request('/api/workspace/credentials', {'purpose': 'tool_auth', 'toolId': tool['id'], 'name': 'Synthetic '+tool['id'], 'value': 'synthetic-platform-wire-key'})
                binding = {'kind': 'platform', 'credentialId': key['id'], 'modelId': tool['model']}
            self.groups[tool['id']] = self.request('/api/workspace/model-configurations', {'name': 'CI '+tool['id'], 'backend': tool['id'], 'binding': binding, 'state': 'active'})['id']
        ids = list(self.groups.values())
        self.request('/api/agents', {'name': 'AgentMatrixE2E', 'inheritModel': False, 'inheritGit': False, 'acpBackend': self.tools[0]['id'], 'modelBinding': {'kind': 'configuration', 'configurationId': ids[0], 'configurationIds': ids}, 'gitBindings': {'items': []}, 'env': {'SKIP_INNER_DOCKER': '1', 'IS_SANDBOX': '1', 'GRASP_CI_MOCK_URL': self.model}, 'files': [{'path': 'AGENTS.md', 'content': 'This is an isolated deterministic CI task. Use only the local model and scoped artifact-store. Do not access other workspaces, deploy, or submit Git changes.'}, {'path': 'config.toml', 'content': 'sandbox_mode = "workspace-write"\napproval_policy = "never"\n'}]})

    def scenario(self, walk):
        turns = []
        self.plan = [{'backend': backend, 'kind': 'switch'} for backend in walk]
        for tool in self.tools:
            for effort in tool['efforts']+([''] if tool['efforts'] else []):
                self.plan.append({'backend': tool['id'], 'kind': 'effort', 'effort': effort})
            if tool['mode'] == 'native-cli':
                for model in ['e2e-auxiliary', '']:
                    self.plan.append({'backend': tool['id'], 'kind': 'model', 'model': model})
        seed_phrase = 'Remember the task nonce '+self.nonce+'.'
        for index, step in enumerate(self.plan):
            turn_id = 't'+str(index)
            current = f'{self.nonce}:step={index}'
            actions = []
            assertions = [{'source': 'current_user' if index == 0 else 'prior_context', 'contains': seed_phrase}]
            if index:
                actions.append({'kind': 'history', 'paginate': True, 'expectRole': 'user', 'maxPages': 10, 'arguments': {'view': 'messages', 'limit': 50}, 'expect': [seed_phrase]})
                actions.append({'kind': 'shell', 'command': 'cat '+self.marker, 'expect': [self.nonce+':step='+str(index-1)]})
            actions.append({'kind': 'shell', 'command': 'printf "%s\\n" '+shlex.quote(current)+' >> '+self.marker+'; cat '+self.marker, 'expect': [current]})
            reply = f'GRASP_E2E_PASS {turn_id}'
            turns.append({'id': turn_id, 'assert': assertions, 'actions': actions, 'reply': reply})
            step.update({'id': turn_id, 'reply': reply})
        self.request('/__mock/scenarios', {'id': self.run, 'turns': turns}, base=self.model)

    def completed(self, session_id, expected):
        state = self.request('/api/solo/sessions/'+session_id)
        messages = self.request('/api/solo/sessions/'+session_id+'/messages')
        self.last_messages = messages
        if state.get('status') == 'error':
            raise AssertionError(f'Grasp session failed: {state.get("error")}')
        failed = [message for message in messages if message.get('status') == 'failed']
        if failed:
            raise AssertionError(f'turn failed: {failed[-1].get("error")}')
        assistants = [message for message in messages if message['role'] == 'assistant']
        if state.get('busy') or len(assistants) < expected:
            return None
        return state, assistants[-1]

    def resolved_effort(self, step):
        # An empty session effort is stored as the last advertised level.
        # OpenCode reads the provider catalog, which does not list the matrix
        # auxiliary id, so that model keeps an empty effort.
        effort = step.get('effort') or ''
        if effort:
            return effort
        tool = next(tool for tool in self.tools if tool['id'] == step['backend'])
        efforts = tool.get('efforts') or []
        if not efforts:
            return ''
        model = step.get('model') or tool['model']
        if tool['id'] == 'opencode' and model != tool['model']:
            return ''
        return efforts[-1]

    def run_matrix(self):
        names = [tool['id'] for tool in self.tools]
        walk = directed_walk(names)
        self.scenario(walk)
        session_id, sandbox_id = None, None
        for index, step in enumerate(self.plan):
            prompt = f'[GRASP_MOCK run={self.run} turn={step["id"]}] Continue the existing task.'
            if index == 0:
                prompt += ' Remember the task nonce '+self.nonce+'.'
                session = self.request('/api/solo/sessions', {'agentName': 'AgentMatrixE2E', 'initialConfigurationId': self.groups[step['backend']], 'initialModelId': next(tool['model'] for tool in self.tools if tool['id'] == step['backend']), 'text': prompt, 'title': 'Credential-free all-agent switch matrix'})
                session_id = session['id']; self.sessions.append(session_id)
            else:
                self.request('/api/solo/sessions/'+session_id+'/model', {'configurationId': self.groups[step['backend']], 'modelId': step.get('model', ''), 'reasoningEffort': step.get('effort', '')})
                self.request('/api/solo/sessions/'+session_id+'/turns', {'text': prompt})
            state, message = self.wait(lambda: self.completed(session_id, index+1), 'turn '+step['id']+' / '+step['backend'], self.args.turn_timeout)
            if step['reply'] not in message.get('content', ''):
                raise AssertionError('native CLI reply did not contain the model fixture completion marker')
            if sandbox_id is None:
                sandbox_id = state['sandboxId']
            if state['sandboxId'] != sandbox_id:
                raise AssertionError('switch replaced the sandbox/workspace')
            if state.get('currentConfigurationId') != self.groups[step['backend']]:
                raise AssertionError('actual selected model group differs from the requested group')
            execution = message.get('execution') or {}
            if execution.get('backend') != step['backend'] or execution.get('configurationId') != self.groups[step['backend']]:
                raise AssertionError('persisted reply execution does not match the tool/model group that actually ran')
            expected_model = step.get('model') or next(tool['model'] for tool in self.tools if tool['id'] == step['backend'])
            if execution.get('modelId') != expected_model or execution.get('agentName') != 'AgentMatrixE2E':
                raise AssertionError('reply model or Agent snapshot is not the route that actually ran')
            expected_effort = self.resolved_effort(step)
            if execution.get('reasoningEffort', '') != expected_effort:
                raise AssertionError('persisted reply reasoning does not match this turn: got %r, expected %r' % (execution.get('reasoningEffort', ''), expected_effort))
            if index and not history_tool_completed(message):
                raise AssertionError('native MCP history result did not pass through the runtime event parser')
            case = {'from': self.plan[index-1]['backend'] if index else None, 'to': step['backend'], 'kind': step['kind'], 'effort': step.get('effort'), 'model': step.get('model'), 'mode': next(tool['mode'] for tool in self.tools if tool['id'] == step['backend']), 'sessionId': session_id, 'sandboxId': sandbox_id, 'turn': step['id'], 'status': 'passed', 'execution': execution, 'usage': message.get('usage'), 'durationMs': message.get('durationMs')}
            self.cases.append(case)
            print(json.dumps(case), flush=True)
        pairs = {(case['from'], case['to']) for case in self.cases if case['from'] and case['kind'] == 'switch'}
        if pairs != set(itertools.permutations(names, 2)):
            raise AssertionError('directed switch coverage is incomplete')
        report = self.request('/__mock/report', base=self.model)
        for step in self.plan:
            observed = [request for request in report['requests'] if request.get('run') == self.run and request.get('turn') == step['id']]
            if not any(request.get('completed') for request in observed):
                raise AssertionError('missing real native model request completion evidence for '+step['id'])
            model = step.get('model') or next(tool['model'] for tool in self.tools if tool['id'] == step['backend'])
            if not any(request.get('model') == model and request.get('completed') for request in observed):
                raise AssertionError('selected primary/auxiliary model did not reach the upstream API for '+step['id'])
            main_requests = [request for request in observed if request.get('availableTools') and not request.get('failure') and request.get('model') == model]
            wired = step.get('effort') or (self.resolved_effort(step) if step['kind'] == 'effort' and step.get('effort') == '' else '')
            if wired and (not main_requests or any(wired not in [(request.get('reasoning') or {}).get(key) for key in ['reasoning_effort', 'effort', 'output_effort']] for request in main_requests)):
                raise AssertionError('reasoning strength did not reach every main model request for '+step['backend']+'/'+wired)
        self.check_efforts(session_id)
        persisted = [message for message in self.request('/api/solo/sessions/'+session_id+'/messages') if message['role'] == 'assistant']
        if len(persisted) != len(self.plan):
            raise AssertionError('assistant replies were dropped or duplicated')
        for message, step in zip(persisted, self.plan):
            execution = message.get('execution') or {}
            if execution.get('backend') != step['backend'] or execution.get('configurationId') != self.groups[step['backend']]:
                raise AssertionError('later selection changed an earlier reply execution snapshot')
            expected_model = step.get('model') or next(tool['model'] for tool in self.tools if tool['id'] == step['backend'])
            if execution.get('modelId') != expected_model or execution.get('agentName') != 'AgentMatrixE2E' or execution.get('reasoningEffort', '') != self.resolved_effort(step):
                raise AssertionError('later selection relabelled an earlier reply model/Agent/effort')

    def check_efforts(self, session_id):
        # Unsupported strengths must be rejected, not silently reported applied.
        for tool in self.tools:
            state = self.request('/api/solo/sessions/'+session_id+'/model', {'configurationId': self.groups[tool['id']], 'modelId': ''})
            available = state.get('reasoningEfforts') or []
            if tool['efforts']:
                if not set(tool['efforts']).issubset(available):
                    raise AssertionError('declared reasoning levels missing for '+tool['id'])
            else:
                if available:
                    raise AssertionError('unsupported reasoning unexpectedly exposed for '+tool['id'])
                try:
                    self.request('/api/solo/sessions/'+session_id+'/model', {'reasoningEffort': 'high'})
                except urllib.error.HTTPError as error:
                    if error.code != 400:
                        raise
                else:
                    raise AssertionError('unsupported reasoning strength was silently accepted')

    def save_report(self, error=None):
        report = {'status': 'failed' if error else 'passed', 'error': str(error) if error else None, 'fullInventory': not bool(self.args.tools), 'tools': self.tools, 'switches': self.cases, 'paidModelCalls': 0, 'nativePlatformInference': 'not covered: Cursor/Trae require their own platform credentials; their CLI wire adapters are exercised with labelled doubles'}
        (self.output/'report.json').write_text(json.dumps(report, indent=2))
        if error:
            # Test prompts, commands and credentials are synthetic. Keep the
            # failing CLI's native event evidence, never its environment/config.
            (self.output/'failure-messages.json').write_text(json.dumps(self.last_messages[-4:], indent=2))

    def close(self):
        errors = []
        for session in self.sessions:
            try:
                result = self.request('/api/solo/sessions/'+session+'/close', {})
                if result.get('status') != 'closed':
                    errors.append('session did not close: '+session)
            except Exception as error:
                errors.append('session cleanup failed: '+str(error))
        for proc in reversed(self.processes):
            if proc.poll() is None:
                try:
                    os.killpg(proc.pid, signal.SIGTERM)
                except ProcessLookupError:
                    continue
                try:
                    proc.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    try:
                        os.killpg(proc.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    proc.wait()
        for handle in self.handles:
            handle.close()
        if not self.args.keep_data:
            shutil.rmtree(self.data)
        if errors:
            raise AssertionError('; '.join(errors))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--server', required=True, help='built Grasp server executable')
    parser.add_argument('--runtime', required=True, help='extracted runtime directory with bin/backend')
    parser.add_argument('--output', default='agent-e2e-artifacts')
    parser.add_argument('--tools', help='local debugging subset only; CI always uses all tools')
    parser.add_argument('--turn-timeout', type=int, default=120)
    parser.add_argument('--keep-data', action='store_true')
    args = parser.parse_args()
    test = E2E(args)
    failure = None
    try:
        test.start(); test.seed(); test.run_matrix()
    except Exception as error:
        failure = error
    finally:
        try:
            test.close()
        except Exception as error:
            failure = AssertionError(str(failure)+'; '+str(error)) if failure else error
        test.save_report(failure)
    if failure:
        raise failure


if __name__ == '__main__':
    main()
