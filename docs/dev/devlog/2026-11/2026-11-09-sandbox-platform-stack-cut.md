# Sandbox platform stack cut

Date: 2026-11-09

Scope: server/internal/sandbox; sandbox-gateway/gateway; sandbox-gateway/sandbox; sandbox-gateway/scripts; scripts/build-sandbox-runtime.sh.

This dependency layer adds provider-scoped lifecycle capabilities and UID-bounded explicit environment release, bounded native workspace jobs and canonical binding protocols, and retained local runtime stop receipts. A known provider handle survives startup timeout or configuration failure. Controlled local sessions are confirmed exited before a retained worktree can be reported stopped.

The application business FSM, authorization factory, all-worker execution-controller composition and Solo UI remain in the final stack layer. The storage foundation startup barrier remains active here, so this intermediate application cannot run an initialized execution dataset using legacy workers. No transitional default authorization or reverse dependency interface is introduced.

Validation: complete server compile, server sandbox package tests, all gateway tests and all native runtime Go tests passed. Private stack-cut handoff binds exact source and logs; full product Agent switching is validated on the complete stack.
