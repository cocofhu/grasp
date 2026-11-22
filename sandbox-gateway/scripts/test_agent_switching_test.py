"""Contracts which prevent accidental gaps in the integration matrix."""
import importlib.util
import itertools
import json
from pathlib import Path
import unittest
from types import SimpleNamespace
import tempfile

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

    def test_fast_probe_artifacts_exclude_private_cli_state(self):
        with tempfile.TemporaryDirectory() as data:
            output = Path(data)
            report = {'status': 'passed', 'cases': [{} for _ in range(8)]}
            (output/'codex-fast-report.json').write_text(json.dumps(report))
            (output/'probe.log').write_text('native probe completed\n')
            (output/'codex-fast-runtime.log').write_text('runtime completed\n')
            self.assertEqual(runner.load_codex_fast_report(output), report)
            private = output/'codex-home'/'tmp'/'arg0-linux'
            private.mkdir(parents=True, mode=0o700)
            with self.assertRaisesRegex(AssertionError, 'private CLI state'):
                runner.load_codex_fast_report(output)

    def test_fast_probe_rejects_symlinks_and_incomplete_native_coverage(self):
        with tempfile.TemporaryDirectory() as data:
            output = Path(data)
            report = output/'codex-fast-report.json'
            report.write_text(json.dumps({'status': 'passed', 'cases': [{} for _ in range(7)]}))
            with self.assertRaisesRegex(AssertionError, 'every required case'):
                runner.load_codex_fast_report(output)
            report.write_text(json.dumps({'status': 'passed', 'cases': [{} for _ in range(8)]}))
            (output/'probe.log').symlink_to(report)
            with self.assertRaisesRegex(AssertionError, 'only report/log files'):
                runner.load_codex_fast_report(output)

    def test_history_result_must_match_its_native_call(self):
        message = {'events': [
            {'data': {'update': {'sessionUpdate': 'tool_call', 'toolCallId': 'history-1', 'title': 'grasp-tool/get_history'}}},
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
            {'data': {'update': {'sessionUpdate': 'tool_call', 'toolCallId': 'actual', 'title': 'DeferExecuteTool', 'rawInput': {'toolName': 'mcp__grasp-tool__get_history'}}}},
            {'data': {'update': {'sessionUpdate': 'tool_call_update', 'toolCallId': 'actual', 'status': 'completed'}}},
        ])
        self.assertTrue(runner.history_tool_completed(message))

    def test_failed_session_cleanup_cannot_be_reported_as_success(self):
        with tempfile.TemporaryDirectory() as data:
            test = runner.E2E.__new__(runner.E2E)
            test.sessions, test.processes, test.handles = ['test-session'], [], []
            test.args, test.data, test.gateway = SimpleNamespace(keep_data=True), Path(data), None
            def rejected(*_args, **_kwargs):
                raise ConnectionError('synthetic unavailable server')
            test.request = rejected
            with self.assertRaisesRegex(AssertionError, 'session cleanup failed'):
                test.close()

    def test_leftover_gateway_sandboxes_are_deleted_or_reported(self):
        with tempfile.TemporaryDirectory() as data:
            test = runner.E2E.__new__(runner.E2E)
            test.sessions, test.processes, test.handles = [], [], []
            test.args, test.data = SimpleNamespace(keep_data=True), Path(data)
            test.gateway, test.gateway_key = 'http://gateway', 'key'
            deleted = []
            def gateway(path, body=None, method=None, base=None, headers=None):
                self.assertEqual((base, headers), ('http://gateway', {'Authorization': 'Bearer key'}))
                if method == 'DELETE':
                    deleted.append(path)
                    return None
                return {'sandboxes': [{'id': 'a'}, {'id': 'b'}]}
            test.request = gateway
            test.close()
            self.assertEqual(deleted, ['/api/v1/sandboxes/a', '/api/v1/sandboxes/b'])
            def unavailable(*_args, **_kwargs):
                raise ConnectionError('synthetic unavailable gateway')
            test.request = unavailable
            with self.assertRaisesRegex(AssertionError, 'sandbox cleanup failed'):
                test.close()


if __name__ == '__main__':
    unittest.main()
