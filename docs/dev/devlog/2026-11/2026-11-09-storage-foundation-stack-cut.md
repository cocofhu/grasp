# Storage foundation stack cut

Date: 2026-11-09

Scope: server/internal/{objectstore,filestore,payloadstore,database,storageguard,historymigration,chatsession,executionlineage,leasewatch,blob,config,models}; server/cmd/storage-admin; server/cmd/server.

The storage foundation introduces shared object storage, typed immutable payload references, offline normalization and exact legacy-history origins, primary closure evidence, database drain, and durable queue/journal storage. New workspace jobs, binding, release, instruction, launch and workflow conversation schema remain in the consuming stack layers. WorkspaceImport is owned by the Git component.

The server registers its storage runtime before normal workers and shares the payload backend with attachments. Until the lifecycle layer installs external execution guards on all workers, an initialized execution dataset prevents startup. This foundation is a dependency layer, not a substitute for the complete migration release.

Validation: complete server compile; focused storage/database/history/queue/authority/admin tests. Final logs and source binding are recorded separately in the private stack-cut handoff. Cross-layer instruction/outbox and workspace tests are introduced with their actual services.
