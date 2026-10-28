"""Contracts which prevent accidental gaps in the integration matrix."""
import importlib.util
import itertools
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


if __name__ == '__main__':
    unittest.main()
