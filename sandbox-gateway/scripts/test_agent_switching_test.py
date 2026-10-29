"""Contracts which prevent accidental gaps in the integration matrix."""
import importlib.util
import itertools
import json
from pathlib import Path
import unittest
from unittest import mock
from types import SimpleNamespace
import tempfile
import tomllib

spec = importlib.util.spec_from_file_location('agent_switching', Path(__file__).with_name('test-agent-switching.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class MatrixContracts(unittest.TestCase):
    def test_every_directed_pair_is_exercised_once(self):
        names = ['cursor', 'claude_code', 'codebuddy', 'trae', 'opencode', 'codex', 'deepseek_harness', 'kimi_code']
        walk = runner.directed_walk(names)
        edges = list(zip(walk, walk[1:]))
        self.assertEqual(len(walk), 57)
        self.assertEqual(len(set(edges)), 56)
        self.assertEqual(set(edges), set(itertools.permutations(names, 2)))

    def test_local_matrix_plans_every_directed_pair(self):
        names = ['cursor', 'claude_code', 'codebuddy', 'trae', 'opencode', 'codex', 'deepseek_harness', 'kimi_code']
        test = runner.E2E.__new__(runner.E2E)
        test.args = SimpleNamespace(execution_mode='local')
        test.tools = [{'id': name} for name in names]
        with mock.patch.object(test, 'scenario', side_effect=RuntimeError('plan captured')) as scenario:
            with self.assertRaisesRegex(RuntimeError, 'plan captured'):
                test.run_matrix()
        walk = scenario.call_args.args[0]
        edges = list(zip(walk, walk[1:]))
        self.assertEqual(len(edges), 56)
        self.assertEqual(set(edges), set(itertools.permutations(names, 2)))

    def test_new_product_agent_without_case_fails(self):
        tools = [{'id': 'opencode', 'mode': 'native-cli'}]
        with self.assertRaisesRegex(AssertionError, 'missing=.*new_agent'):
            runner.validate_inventory(tools, [{'toolId': 'opencode'}, {'toolId': 'new_agent'}])

    def test_removed_product_agent_cannot_leave_stale_green_case(self):
        tools = [{'id': 'opencode', 'mode': 'native-cli'}]
        with self.assertRaisesRegex(AssertionError, 'extra=.*opencode'):
            runner.validate_inventory(tools, [])

    def test_platform_wire_coverage_must_be_explicit(self):
        tools = [{'id': 'cursor', 'mode': 'platform-wire-fixture'}]
        with self.assertRaisesRegex(AssertionError, 'missing native coverage'):
            runner.validate_inventory(tools, [{'toolId': 'cursor'}])

    def test_kimi_matrix_covers_efforts_reset_and_supported_and_unsupported_auxiliary_models(self):
        tools = json.loads(Path(__file__).with_name('agent-e2e-matrix.json').read_text())['tools']
        tool = next(tool for tool in tools if tool['id'] == 'kimi_code')
        self.assertEqual(tool['mode'], 'platform-wire-fixture')
        self.assertIn('vendor inference is not covered', tool['reason'])
        self.assertEqual(tool['model'], 'k3.1-pro')
        self.assertEqual(tool['efforts'], ['low', 'high', 'max'])
        test = runner.E2E.__new__(runner.E2E)
        test.tools, test.nonce, test.marker, test.run, test.model = [tool], 'task-'+'a'*24, 'checkpoint.txt', 'unit-test', 'http://localhost'
        test.request = lambda *_args, **_kwargs: None
        test.scenario(['kimi_code'])
        self.assertEqual([step.get('effort') for step in test.plan if step['kind'] == 'effort'], ['low', 'high', 'max', ''])
        self.assertEqual([test.resolved_effort(step) for step in test.plan], ['max', 'low', 'high', 'max', 'max', 'max', 'max', '', 'max'])
        self.assertEqual([step.get('model') for step in test.plan if step['kind'] == 'model'], ['k3', 'kimi-for-coding', 'kimi-for-coding-highspeed', ''])

    def test_reasoning_wire_assertion_requires_every_request_to_carry_the_selected_level(self):
        for requests in [[], [{'reasoning': {}}], [{'reasoning': {'reasoning_effort': 'max'}}], [{'reasoning': {'thinking_effort': 'max'}}], [{'reasoning': {'thinking_type': 'enabled', 'thinking_effort': 'low'}}], [{'reasoning': {'thinking_type': 'enabled', 'thinking_effort': 'max'}}, {'reasoning': {'thinking_type': 'enabled', 'thinking_effort': 'high'}}]]:
            with self.assertRaisesRegex(AssertionError, 'every main model request'):
                runner.assert_reasoning_requests(requests, 'max', 'kimi_code')
        runner.assert_reasoning_requests([{'reasoning': {'thinking_type': 'enabled', 'thinking_effort': 'max'}}, {'reasoning': {'thinking_type': 'enabled', 'thinking_effort': 'max'}}], 'max', 'kimi_code')

    def test_history_result_must_match_its_native_call(self):
        message = {'events': [
            {'data': {'update': {'sessionUpdate': 'tool_call', 'toolCallId': 'history-1', 'title': 'artifact-store/get_history'}}},
            {'data': {'update': {'sessionUpdate': 'tool_call_update', 'toolCallId': 'other', 'status': 'completed'}}},
        ]}
        self.assertFalse(runner.history_tool_completed(message))
        message['events'].append({'data': {'update': {'sessionUpdate': 'tool_call_update', 'toolCallId': 'history-1', 'status': 'completed'}}})
        self.assertTrue(runner.history_tool_completed(message))

    def test_deferred_native_history_is_distinguished_from_discovery(self):
        message = {'events': [
            {'data': {'update': {'sessionUpdate': 'tool_call', 'toolCallId': 'search', 'title': 'ToolSearch', 'rawInput': {'query': 'get_history'}}}},
            {'data': {'update': {'sessionUpdate': 'tool_call_update', 'toolCallId': 'search', 'status': 'completed'}}},
        ]}
        self.assertFalse(runner.history_tool_completed(message))
        message['events'].extend([
            {'data': {'update': {'sessionUpdate': 'tool_call', 'toolCallId': 'actual', 'title': 'DeferExecuteTool', 'rawInput': {'toolName': 'mcp__artifact-store__get_history'}}}},
            {'data': {'update': {'sessionUpdate': 'tool_call_update', 'toolCallId': 'actual', 'status': 'completed'}}},
        ])
        self.assertTrue(runner.history_tool_completed(message))

    def test_failed_session_cleanup_cannot_be_reported_as_success(self):
        with tempfile.TemporaryDirectory() as data:
            test = runner.E2E.__new__(runner.E2E)
            test.sessions, test.processes, test.handles = ['test-session'], [], []
            test.args, test.data = SimpleNamespace(keep_data=True), Path(data)
            def rejected(*_args, **_kwargs):
                raise ConnectionError('synthetic unavailable server')
            test.request = rejected
            with self.assertRaisesRegex(AssertionError, 'session cleanup failed'):
                test.close()

    def test_local_mode_seed_rejects_preexisting_credentials(self):
        test = runner.E2E.__new__(runner.E2E)
        test.args = SimpleNamespace(execution_mode='local')
        test.tools = [{'id': 'opencode', 'mode': 'native-cli', 'model': 'gpt-5', 'efforts': []}]
        def fake_request(path, *_args, **_kwargs):
            if path == '/api/settings':
                return {'execution_mode': 'local'}
            if path == '/api/workspace/credentials':
                return {'items': [{'id': 'cred-1'}]}
            return {}
        test.request = fake_request
        with self.assertRaisesRegex(AssertionError, 'must not require or pre-populate workspace credentials'):
            test.seed()

    def test_local_host_auth_uses_saved_native_configs_without_secret_environment(self):
        with tempfile.TemporaryDirectory() as data:
            test = runner.E2E.__new__(runner.E2E)
            test.data, test.model = Path(data), 'http://127.0.0.1:12345'
            env = {'PATH': '/synthetic/bin', 'LANG': 'C.UTF-8'}
            test.prepare_local_host_home(env)
            home = Path(env['HOME'])
            self.assertEqual(env, {'PATH': '/synthetic/bin', 'LANG': 'C.UTF-8', 'HOME': str(test.data/'host-home')})
            claude = json.loads((home/'.claude/settings.json').read_text())
            self.assertEqual(claude['env']['ANTHROPIC_BASE_URL'], test.model)
            self.assertEqual(claude['env']['ANTHROPIC_API_KEY'], 'synthetic-local-host-key')
            codebuddy = json.loads((home/'.codebuddy/models.json').read_text())
            self.assertTrue(all(model['apiKey'] == 'synthetic-local-host-key' and model['url'] == test.model+'/v1/chat/completions' for model in codebuddy['models']))
            codex = tomllib.loads((home/'.codex/config.toml').read_text())
            self.assertEqual(codex['model_providers']['local']['experimental_bearer_token'], 'synthetic-local-host-key')
            self.assertNotIn('env_key', codex['model_providers']['local'])
            dsh = json.loads((home/'.dsh/.credentials.yaml').read_text())
            self.assertEqual(dsh, {'version': 1, 'refs': {'DEEPSEEK_API_KEY': 'synthetic-local-host-key'}})
            self.assertEqual(json.loads((home/'.dsh/cordis.patch.yml').read_text()), [{'id': 'llm-deepseek', 'config': {'apiKeyEnv': 'DEEPSEEK_API_KEY', 'baseURL': test.model}}])
            kimi = tomllib.loads((home/'.kimi-code/config.toml').read_text())
            self.assertEqual(kimi['providers']['kimi']['api_key'], 'synthetic-local-host-key')
            self.assertNotIn('api_key_env', kimi['providers']['kimi'])
            for path in home.rglob('*'):
                if path.is_file():
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600, path)


if __name__ == '__main__':
    unittest.main()
