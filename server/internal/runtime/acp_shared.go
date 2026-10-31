package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// Shared run sandbox (sandbox mode "shared"; newly created workflows default to
// it, an unset mode means per-node): every node of a run reuses one container
// instead of getting its own. A node opening the
// sandbox re-primes it when its env / config home / credentials differ from
// the previous node's (reprime.sh restarts the backend with the new ones),
// then connects a fresh agent session on the default bridge chat. Releasing
// a node only closes its session; the container is retired when the run ends.
//
// While one node keeps its session parked (review, a downstream gate waiting
// for reject, a takeover), another node cannot take the container over, so it
// falls back to a sandbox of its own just like per-node mode.

// sharedRunSandbox is the one sandbox of a shared-mode run.
type sharedRunSandbox struct {
	sb    *sandbox.Sandbox
	home  string // config home of the node that created it; removed with it
	image string
	// fingerprint / envKeys describe what the sandbox is primed with now.
	// An empty fingerprint (adopted after a restart) always re-primes.
	fingerprint string
	envKeys     []string
	// owner is the reactKey whose session holds the sandbox ("" when idle).
	owner string
}

// sharedSandboxes is process-wide: each acpBackend has its own provider, and a
// run's nodes may use different backends.
type sharedSandboxes struct {
	mu    sync.Mutex
	runs  map[string]*sharedRunSandbox
	locks map[string]*sync.Mutex
	// ended names the shared sandboxes of runs just retired. AbortRun runs on
	// every provider in turn; one retiring the sandbox first must not let the
	// others destroy it as an ordinary in-flight sandbox.
	ended map[string]bool
}

func newSharedSandboxes() *sharedSandboxes {
	return &sharedSandboxes{runs: map[string]*sharedRunSandbox{}, locks: map[string]*sync.Mutex{}, ended: map[string]bool{}}
}

// sharedEndedTTL is how long a retired shared sandbox is still recognised.
var sharedEndedTTL = time.Minute

var processSharedSandboxes = newSharedSandboxes()

// sandboxReprimer is implemented by sandbox.Manager.
type sandboxReprimer interface {
	Reprime(ctx context.Context, sb *sandbox.Sandbox, spec sandbox.Spec, prevKeys []string) error
}

// errSharedIncompatible: the node cannot run in the shared sandbox as it is
// (another workspace dir); it uses a sandbox of its own.
var errSharedIncompatible = errors.New("node incompatible with shared sandbox")

// sharedReadyWait bounds the liveness probe of a shared sandbox before reuse;
// reprimeReadyWait the wait for the backend after a re-prime. Vars for tests.
var (
	sharedReadyWait  = 20 * time.Second
	reprimeReadyWait = 120 * time.Second
)

func (s *sharedSandboxes) runLock(runID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.locks[runID]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[runID] = l
	}
	return l
}

func (s *sharedSandboxes) get(runID string) *sharedRunSandbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[runID]
}

func (s *sharedSandboxes) primedEnvKeys(runID string, sb *sandbox.Sandbox) []string {
	if s == nil || sb == nil || runID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.runs[runID]
	if cur == nil || cur.sb != sb {
		return nil
	}
	return append([]string(nil), cur.envKeys...)
}

// notePrimed records a chat group switch on the run's shared sandbox so the
// next automatic node sees a changed fingerprint and re-primes its own binding.
func (s *sharedSandboxes) notePrimed(runID string, sb *sandbox.Sandbox, spec sandbox.Spec) {
	if s == nil || sb == nil || runID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.runs[runID]
	if cur == nil || cur.sb != sb {
		return
	}
	cur.fingerprint = sandbox.SpecFingerprint(spec)
	cur.envKeys = sandbox.ReprimeEnvKeys(spec)
	if spec.Image != "" {
		cur.image = spec.Image
	}
}

func (s *sharedSandboxes) put(runID string, ss *sharedRunSandbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[runID] = ss
}

// take removes the run's shared sandbox (only ss when non-nil) and returns it.
func (s *sharedSandboxes) take(runID string, ss *sharedRunSandbox) *sharedRunSandbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.runs[runID]
	if cur == nil || (ss != nil && cur != ss) {
		return nil
	}
	delete(s.runs, runID)
	return cur
}

// forget drops the run's bookkeeping once the run is over.
func (s *sharedSandboxes) forget(runID string) *sharedRunSandbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.runs[runID]
	delete(s.runs, runID)
	delete(s.locks, runID)
	if cur != nil && cur.sb != nil && cur.sb.Name != "" {
		name := cur.sb.Name
		s.ended[name] = true
		time.AfterFunc(sharedEndedTTL, func() {
			s.mu.Lock()
			delete(s.ended, name)
			s.mu.Unlock()
		})
	}
	return cur
}

// release frees the sandbox named name from its current owner. False when no
// shared sandbox has that name.
func (s *sharedSandboxes) release(name string) bool {
	if name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ss := range s.runs {
		if ss.sb != nil && ss.sb.Name == name {
			ss.owner = ""
			return true
		}
	}
	return s.ended[name]
}

func (s *sharedSandboxes) update(ss *sharedRunSandbox, fn func(*sharedRunSandbox)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(ss)
}

// openSharedSandbox opens the node's session in the run's shared sandbox,
// creating it on the first node. handled=false sends the caller to the
// per-node path (the sandbox is held by another node or runs another image).
func (c *acpProvider) openSharedSandbox(ctx context.Context, req NodeReq) (sb *sandbox.Sandbox, acp *sandbox.ACPClient, home string, handled bool, err error) {
	lock := c.shared.runLock(req.RunID)
	lock.Lock()
	defer lock.Unlock()
	key := reactKey(req)
	image := resolveProviderImage(c.opts, c.backend)
	if ss := c.shared.get(req.RunID); ss != nil {
		if ss.owner != "" && ss.owner != key {
			log.Info().Str("run", req.RunID).Str("node", req.NodeID).Str("holder", ss.owner).
				Msg("shared sandbox held by a parked session; node uses its own sandbox")
			return nil, nil, "", false, nil
		}
		if ss.image != image {
			log.Info().Str("run", req.RunID).Str("node", req.NodeID).Str("image", image).
				Msg("node needs another sandbox image; node uses its own sandbox")
			return nil, nil, "", false, nil
		}
		sb, acp, err := c.reuseSharedSandbox(ctx, req, ss)
		if err == nil {
			return sb, acp, "", true, nil
		}
		if errors.Is(err, errSharedIncompatible) {
			log.Info().Str("run", req.RunID).Str("node", req.NodeID).
				Msg("node needs another workspace dir; node uses its own sandbox")
			return nil, nil, "", false, nil
		}
		if errors.Is(err, errSandboxConfig) {
			return nil, nil, "", true, err
		}
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
			Msg("shared sandbox unusable; replacing it")
		c.dropSharedSandbox(req.RunID, ss)
	}
	sb, acp, home, spec, err := c.openNodeSandbox(ctx, req)
	if err != nil {
		return nil, nil, "", true, err
	}
	c.shared.put(req.RunID, &sharedRunSandbox{
		sb: sb, home: home, image: image,
		fingerprint: sandbox.SpecFingerprint(spec), envKeys: sandbox.ReprimeEnvKeys(spec), owner: key,
	})
	c.registerSharedRunSandbox(req, sb, home)
	log.Info().Str("run", req.RunID).Str("node", req.NodeID).Str("sandbox", sb.Name).
		Msg("created shared run sandbox")
	// The shared sandbox owns home; callers must not remove it per node.
	return sb, acp, "", true, nil
}

// reuseSharedSandbox re-primes the shared sandbox for req when needed and
// connects a fresh agent session for it.
func (c *acpProvider) reuseSharedSandbox(ctx context.Context, req NodeReq, ss *sharedRunSandbox) (*sandbox.Sandbox, *sandbox.ACPClient, error) {
	sb := ss.sb
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, sharedReadyWait); err != nil {
		return nil, nil, fmt.Errorf("%w: shared sandbox not ready: %v", errSandboxSetup, err)
	}
	spec, err := c.spec(req)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		return nil, nil, fmt.Errorf("%w: %v", errSandboxConfig, err)
	}
	// Everything the sandbox needs from it is packed into the re-prime bundle.
	defer removeHome(spec.ConfigHome)
	if spec.WorkspaceDir != "" && sb.WorkspaceDir != "" && spec.WorkspaceDir != sb.WorkspaceDir {
		c.forgetCodexLogin(codexLoginKey(req))
		return nil, nil, errSharedIncompatible
	}
	fp := sandbox.SpecFingerprint(spec)
	reprime := fp == "" || fp != ss.fingerprint
	if reprime {
		rp, ok := c.mgr.(sandboxReprimer)
		if !ok {
			c.forgetCodexLogin(codexLoginKey(req))
			return nil, nil, fmt.Errorf("%w: sandbox manager cannot re-prime", errSandboxSetup)
		}
		if err := rp.Reprime(ctx, sb, spec, ss.envKeys); err != nil {
			c.forgetCodexLogin(codexLoginKey(req))
			return nil, nil, fmt.Errorf("%w: %v", errSandboxSetup, err)
		}
		if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, reprimeReadyWait); err != nil {
			c.forgetCodexLogin(codexLoginKey(req))
			return nil, nil, fmt.Errorf("%w: acp not ready after re-prime: %v", errSandboxSetup, err)
		}
		c.shared.update(ss, func(s *sharedRunSandbox) {
			s.fingerprint, s.envKeys = fp, sandbox.ReprimeEnvKeys(spec)
		})
	}
	c.syncSharedBranches(ctx, sb, req)
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeoutFunc(c.agentIdle).
		WithBridgeModel(spec.Env["ACP_BRIDGE_MODEL"]).
		WithExecution(c.executionForEnvironment(req, spec.Env))
	if err := acp.Connect(ctx); err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		acp.Close()
		return nil, nil, fmt.Errorf("%w: acp connect: %v", errSandboxSetup, err)
	}
	c.shared.update(ss, func(s *sharedRunSandbox) { s.owner = reactKey(req) })
	c.registerSharedRunSandbox(req, sb, ss.home)
	log.Info().Str("run", req.RunID).Str("node", req.NodeID).Str("sandbox", sb.Name).
		Bool("reprimed", reprime).Msg("node reuses shared run sandbox")
	return sb, acp, nil
}

// syncSharedBranches checks out the branch each repo should be on for this
// node (vars.branches), as a fresh per-node sandbox would have cloned it.
func (c *acpProvider) syncSharedBranches(ctx context.Context, sb *sandbox.Sandbox, req NodeReq) {
	for _, r := range resolveRepos(req) {
		if r.Branch == "" {
			continue
		}
		note, err := sb.SyncBranch(ctx, repoWorkspacePath(r.Name), r.Branch)
		if err != nil || note == "dirty" || note == "absent" {
			log.Info().Err(err).Str("run", req.RunID).Str("node", req.NodeID).Str("repo", r.Name).
				Str("branch", r.Branch).Str("result", note).Msg("shared sandbox branch left as is")
		}
	}
}

// adoptSharedSandbox records a shared sandbox reattached after a control-plane
// restart. Its priming is unknown, so the next other node re-primes it.
func (c *acpProvider) adoptSharedSandbox(req NodeReq, sb *sandbox.Sandbox) {
	if c.shared == nil || req.SandboxMode != models.SandboxModeShared || sb == nil {
		return
	}
	if c.shared.get(req.RunID) != nil {
		return
	}
	c.shared.put(req.RunID, &sharedRunSandbox{
		sb: sb, image: resolveProviderImage(c.opts, c.backend), owner: reactKey(req),
	})
}

// releaseSharedSandbox ends a node's use of the shared sandbox: its session
// closes, the container stays for the next node. False when sb is not shared.
func (c *acpProvider) releaseSharedSandbox(sb *sandbox.Sandbox, acp *sandbox.ACPClient) bool {
	if c.shared == nil || sb == nil || !c.shared.release(sb.Name) {
		return false
	}
	if acp != nil {
		acp.Close()
	}
	return true
}

// dropSharedSandbox destroys a broken shared sandbox.
func (c *acpProvider) dropSharedSandbox(runID string, ss *sharedRunSandbox) {
	if c.shared.take(runID, ss) == nil {
		return
	}
	c.deregisterRunSandbox(ss.sb.Name)
	ss.sb.Destroy(context.Background())
	removeHome(ss.home)
}

// retireSharedRunSandbox keeps a finished run's shared sandbox for the debug
// TTL, like a per-node sandbox at the end of its node.
func (c *acpProvider) retireSharedRunSandbox(runID string) {
	if c.shared == nil {
		return
	}
	ss := c.shared.forget(runID)
	if ss == nil || ss.sb == nil {
		return
	}
	if r, ok := c.registry.(RunSandboxRetirer); ok {
		r.RetireRunSandbox(ss.sb.Name)
		return
	}
	c.deregisterRunSandbox(ss.sb.Name)
	ss.sb.Destroy(context.Background())
	removeHome(ss.home)
}

// registerSharedRunSandbox points the shared sandbox's store row at the node
// now using it. No-op when no registry is wired.
func (c *acpProvider) registerSharedRunSandbox(req NodeReq, sb *sandbox.Sandbox, home string) {
	if c.registry == nil || sb == nil {
		return
	}
	c.registry.RegisterRunSandbox(RunSandboxInfo{
		Name:           sb.Name,
		Profile:        models.AgentProfile(req.Config),
		RunID:          req.RunID,
		WorkflowID:     req.WorkflowID,
		WorkflowName:   req.WorkflowName,
		NodeID:         req.NodeID,
		Host:           sb.Host,
		ACPPort:        sb.Port,
		CodeServerPort: sb.CodeServerPort,
		RepoURL:        firstRepoURL(req),
		HomeDir:        home,
		Token:          req.Token,
		Shared:         true,
	})
}
