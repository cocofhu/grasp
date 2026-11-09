"""Contracts which prevent accidental gaps in the integration matrix."""
import importlib.util
import io
import itertools
import json
import os
from pathlib import Path
import unittest
from unittest import mock
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
            test.close_backends = lambda: None
            def rejected(*_args, **_kwargs):
                raise ConnectionError('synthetic unavailable server')
            test.request = rejected
            with self.assertRaisesRegex(AssertionError, 'session cleanup failed'):
                test.close()


class CleanupContracts(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.test = runner.E2E.__new__(runner.E2E)
        self.test.data, self.test.marker = Path(self.temp.name), 'checkpoint.txt'
        self.test.args = SimpleNamespace(runtime=str(self.test.data/'runtime'), keep_data=True)
        self.test.cleanup, self.test.fixture_cleanup = [], []
        self.test.sessions, self.test.processes, self.test.handles = ['owned-session'], [], []
        self.marker = self.test.data/'launches/owned-worktree/checkpoint.txt'
        self.marker.parent.mkdir(parents=True)
        self.marker.write_bytes(b'synthetic-native-checkpoint\x00\xff\n')
        self.before = {'id': 'owned-session', 'sandboxId': 42, 'status': 'running'}
        self.after = {**self.before, 'status': 'closed', 'sandbox': {'id': 42, 'status': 'stopped', 'desiredState': 'stopped'}}

    @staticmethod
    def http_error(status=409, code='environment_stop_pending'):
        error = runner.urllib.error.HTTPError('http://localhost/close', status, 'synthetic failure', {}, None)
        error.grasp_response = {'code': code}
        return error

    def responses(self, close_error=None, mutate=None):
        reads = iter([self.before, self.after])
        def request(path, body=None):
            if path.endswith('/close'):
                if mutate:
                    mutate()
                if close_error:
                    raise close_error
                return self.after  # A successful POST alone is never a stop proof.
            return next(reads)
        self.test.request = mock.Mock(side_effect=request)

    def test_exact_stop_pending_requires_readback_and_preserves_unconfirmed_result(self):
        self.after['sandbox'].update(status='error', desiredState='stopped')
        self.responses(self.http_error())
        self.test.close_session('owned-session')
        self.assertEqual(self.test.request.call_args_list, [mock.call('/api/solo/sessions/owned-session'), mock.call('/api/solo/sessions/owned-session/close', {}), mock.call('/api/solo/sessions/owned-session')])
        self.assertEqual(self.test.cleanup, [{'sessionId': 'owned-session', 'sandboxId': 42, 'status': 'closed_stop_pending_workspace_retained', 'workspacePreserved': True, 'providerStopConfirmed': False}])
        self.assertEqual(self.marker.read_bytes(), b'synthetic-native-checkpoint\x00\xff\n')

    def test_successful_stop_also_requires_authoritative_get(self):
        self.responses()
        self.test.close_session('owned-session')
        self.assertEqual(self.test.request.call_count, 3)
        self.assertTrue(self.test.cleanup[0]['providerStopConfirmed'])

    def test_other_http_errors_are_not_accepted_as_stop_pending(self):
        for status, code in [(409, 'environment_retained'), (409, ''), (500, 'environment_stop_pending'), (401, 'environment_stop_pending')]:
            with self.subTest(status=status, code=code):
                self.responses(self.http_error(status, code))
                with self.assertRaises(runner.urllib.error.HTTPError):
                    self.test.close_session('owned-session')
                self.assertEqual(self.test.cleanup, [])
        for malformed in [None, [], 'environment_stop_pending']:
            error = self.http_error()
            error.grasp_response = malformed
            self.responses(error)
            with self.assertRaises(runner.urllib.error.HTTPError):
                self.test.close_session('owned-session')

    def test_network_failure_and_missing_readback_are_not_stop_proofs(self):
        for failure in [ConnectionError('synthetic network failure'), runner.urllib.error.URLError('synthetic network failure')]:
            with self.subTest(failure=type(failure).__name__):
                self.responses(failure)
                with self.assertRaises(type(failure)):
                    self.test.close_session('owned-session')
        self.test.request = mock.Mock(side_effect=[self.before, self.http_error(), ConnectionError('readback unavailable')])
        with self.assertRaisesRegex(ConnectionError, 'readback unavailable'):
            self.test.close_session('owned-session')
        self.assertEqual(self.test.cleanup, [])

    def test_pending_must_preserve_exact_session_binding_and_closed_status(self):
        original = self.after.copy()
        for changed in [{'id': 'other-session'}, {'sandboxId': 0}, {'sandboxId': 43}, {'status': 'running'}]:
            with self.subTest(changed=changed):
                self.after = {**original, **changed}
                self.responses(self.http_error())
                with self.assertRaisesRegex(AssertionError, 'original environment'):
                    self.test.close_session('owned-session')
                self.assertEqual(self.test.cleanup, [])

    def test_pending_cannot_hide_missing_changed_or_extra_native_markers(self):
        for change in ['missing', 'changed', 'extra']:
            with self.subTest(change=change):
                def mutate():
                    if change == 'missing':
                        self.marker.unlink()
                    elif change == 'changed':
                        self.marker.write_bytes(b'wrong checkpoint')
                    else:
                        (self.marker.parent.parent/'checkpoint.txt').write_bytes(b'unexpected worktree')
                self.responses(self.http_error(), mutate)
                with self.assertRaisesRegex(AssertionError, 'retained native workspace'):
                    self.test.close_session('owned-session')
                self.assertEqual(self.test.cleanup, [])
                self.marker.write_bytes(b'synthetic-native-checkpoint\x00\xff\n')

    def test_missing_original_binding_or_marker_cannot_be_accepted(self):
        for binding in [None, 0, True, '42']:
            with self.subTest(binding=binding):
                self.before['sandboxId'] = binding
                self.responses(self.http_error())
                with self.assertRaisesRegex(AssertionError, 'missing original'):
                    self.test.close_session('owned-session')
                self.assertEqual(self.test.request.call_count, 1)
        self.before['sandboxId'] = 42
        self.marker.unlink()
        self.responses(self.http_error())
        with self.assertRaisesRegex(AssertionError, 'missing original'):
            self.test.close_session('owned-session')

    def test_http_success_cannot_claim_stop_while_environment_is_running(self):
        self.after['sandbox']['status'] = 'running'
        self.responses()
        with self.assertRaisesRegex(AssertionError, 'did not confirm'):
            self.test.close_session('owned-session')
        self.assertEqual(self.test.cleanup, [])

    def test_marker_symlink_cannot_read_other_workspaces(self):
        self.marker.unlink()
        foreign = self.test.data/'outside-worktree'
        foreign.write_bytes(b'other contents')
        self.marker.symlink_to(foreign)
        self.responses(self.http_error())
        with self.assertRaisesRegex(AssertionError, 'escaped'):
            self.test.close_session('owned-session')
        self.assertEqual(self.test.request.call_count, 1)

    def test_close_failure_still_cleans_owned_processes_and_retains_evidence(self):
        self.test.args.keep_data = False
        self.responses(ConnectionError('synthetic unavailable server'))
        proc = mock.Mock(pid=12345)
        proc.poll.return_value = None
        self.test.processes = [proc]
        self.test.close_backends = mock.Mock()
        with mock.patch.object(runner.os, 'killpg') as kill:
            with self.assertRaisesRegex(AssertionError, 'session cleanup failed'):
                self.test.close()
        kill.assert_called_once_with(12345, runner.signal.SIGTERM)
        proc.wait.assert_called_once_with(timeout=8)
        self.test.close_backends.assert_called_once()
        self.assertTrue(self.marker.exists())

    def test_backend_group_signal_rejects_reused_pid(self):
        identity = {'pid': 12345, 'start': '100', 'group': 12345}
        with mock.patch.object(runner, 'process_identity', return_value={**identity, 'start': '101'}), mock.patch.object(runner.os, 'killpg') as kill:
            with self.assertRaisesRegex(AssertionError, 'identity changed'):
                runner.signal_owned_backend(identity, runner.signal.SIGTERM)
        kill.assert_not_called()

    def test_process_discovery_skips_other_users_before_reading_protected_proc_files(self):
        root, proc = mock.MagicMock(), mock.MagicMock()
        root.__truediv__.return_value = proc
        proc.stat.return_value.st_uid = os.getuid()+1
        with mock.patch.object(runner, 'Path', return_value=root), mock.patch.object(runner.os, 'readlink') as readlink:
            self.assertIsNone(runner.process_identity(12345))
        proc.__truediv__.assert_not_called()
        readlink.assert_not_called()

    def test_only_exact_runtime_and_private_worktree_backend_can_be_cleaned(self):
        binary = str((self.test.data/'runtime/bin/backend').resolve())
        own = {'pid': 111, 'group': 111, 'uid': os.getuid(), 'start': '1', 'exe': binary, 'cwd': str(self.marker.parent)}
        identities = {111: own, 112: {**own, 'exe': '/other/backend'}, 113: {**own, 'cwd': str(self.test.data/'other-launches')}, 114: {**own, 'uid': os.getuid()+1}}
        with mock.patch.object(runner.Path, 'iterdir', return_value=[Path('/proc')/str(pid) for pid in identities]), mock.patch.object(runner, 'process_identity', side_effect=identities.get):
            self.assertEqual(runner.owned_backend_processes(self.test.data, self.test.args.runtime), [own])

    def test_fixture_backend_exit_does_not_turn_pending_into_provider_stop_proof(self):
        self.responses(self.http_error())
        self.test.close_session('owned-session')
        identity = {'pid': 12345, 'group': 12345}
        with mock.patch.object(runner, 'owned_backend_processes', side_effect=[[identity], []]), mock.patch.object(runner, 'process_identity', return_value=None), mock.patch.object(runner, 'process_group_alive', return_value=False), mock.patch.object(runner, 'signal_owned_backend') as kill:
            self.test.close_backends()
        kill.assert_called_once_with(identity, runner.signal.SIGTERM)
        self.assertTrue(self.test.fixture_cleanup[0]['exited'])
        self.assertFalse(self.test.cleanup[0]['providerStopConfirmed'])

    def test_exited_backend_cannot_hide_live_group_descendants(self):
        identity = {'pid': 12345, 'group': 12345}
        with mock.patch.object(runner, 'owned_backend_processes', return_value=[identity]), mock.patch.object(runner, 'process_identity', return_value=None), mock.patch.object(runner, 'process_group_alive', return_value=True), mock.patch.object(runner, 'signal_owned_backend'):
            with self.assertRaisesRegex(AssertionError, 'group did not exit'):
                self.test.close_backends()
        self.assertEqual(self.test.fixture_cleanup, [])

    def test_http_error_preserves_only_object_response_for_stable_code_checks(self):
        self.test.base = 'http://localhost'
        for body, expected in [(b'{"code":"environment_stop_pending"}', {'code': 'environment_stop_pending'}), (b'null', {}), (b'[]', {}), (b'invalid', {})]:
            with self.subTest(body=body):
                error = runner.urllib.error.HTTPError('http://localhost/close', 409, 'synthetic failure', {}, io.BytesIO(body))
                self.test.client = mock.Mock()
                self.test.client.open.side_effect = error
                with self.assertRaises(runner.urllib.error.HTTPError) as caught:
                    runner.E2E.request(self.test, '/close', {})
                self.assertEqual(caught.exception.grasp_response, expected)


if __name__ == '__main__':
    unittest.main()
