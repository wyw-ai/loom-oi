# Loom Integration and Migration Plan

**Status:** Proposed  
**Scope:** Integration of the independent loom-oi project into Loom and evolution of the existing daemon.

## 1. Objective

Loom integrates OI as its controlled-resource management subsystem while retaining ownership of Agent and collaboration workflows.

- loom-server owns Agent / Actor identity, Channel, Thread, Message, Task, Run, collaboration and execution routing.
- OIS owns controlled endpoint records, capabilities, bindings, policy, configuration and observed state.
- OIC provides controlled-side identity, connectivity, capability exposure, dispatch and extension hosting.
- The Loom integration adapts existing Loom identity and transport mechanisms to OI contracts.

The OI implementation lives in the independent `wyw-ai/loom-oi` repository. Loom depends on stable OI protocol / SDK interfaces rather than copying OI domain implementation into its source tree.

## 2. Current Loom codebase

| Current path | Current responsibility | Planned treatment |
|---|---|---|
| `crates/server` | WebSocket server, journal, handlers, machine commands and state | Modularize by domain; add Loom-to-OI integration |
| `crates/cli` | Loom CLI and daemon implementation | Separate CLI entry, OIC integration and Agent Runtime Host |
| `crates/cli/src/cmd/daemon.rs` | Machine identity, inventory, machine command loop, service host and Agent host startup | Extract OIC-compatible responsibilities; retain compatibility during migration |
| `crates/cli/src/cmd/agent_serve.rs` | Agent workers, Agent connections and Run interaction | Move behind Agent Runtime Host / official extension boundary |
| `crates/cli/src/daemon_ipc.rs` | Local daemon IPC and proxy | Retain or adapt as local OIC management interface |
| `crates/agent-runtime` | ACP, Command, Interactive, Provider and Discovery | Retain as reusable Agent execution engine |
| `crates/proto` | JSON-RPC methods and shared types | Maintain Loom protocol; add or adapt OI integration protocol |
| `crates/loom-platform` | OS, process, file, IPC and path facilities | Retain as platform library |
| `crates/gui`, `apps/gui-web` | Tauri and React GUI | Retain; add controlled-resource management UI |

This mapping is a target plan. It does not imply that the current code has already been moved or that every listed module needs a new Crate.

## 3. Target Loom Workspace structure

The following is a proposed target structure. Existing Cargo packages can be reorganized gradually; application entry points do not need to move before internal boundaries are established.

~~~text
loom/
├── apps/                              # Executable entry points (gradual extraction)
│   ├── loom-server/
│   ├── loom-cli/
│   └── oic/                           # Optional OIC launcher if bundled with Loom
│
├── crates/
│   ├── server/                        # Existing Server, modularized
│   │   └── src/
│   │       ├── bootstrap/
│   │       ├── api/
│   │       ├── modules/
│   │       │   ├── identity/
│   │       │   ├── agent/
│   │       │   ├── collaboration/
│   │       │   ├── execution/
│   │       │   └── oi_integration/    # Loom adapter to OIS
│   │       └── infrastructure/
│   ├── loom-oi-client/                # Optional pinned OI SDK dependency
│   ├── loom-integration/              # Loom subject / transport adapter
│   ├── agent-runtime/                 # Existing reusable Agent engine
│   ├── protocol/                      # Existing proto package, gradually organized
│   ├── platform/                      # Existing loom-platform, optional rename
│   ├── gui/
│   └── loom-shell/
│
├── apps/gui-web/                      # Existing React app; retain initially
└── Cargo.toml
~~~

The tree is illustrative. If OIC is shipped as a separate binary from the loom-oi repository, Loom does not need an `apps/oic` package. Avoid duplicate copies of OIC Core or OIS logic inside the Loom repository.

## 4. OIS deployment options

Repository ownership and process deployment are separate decisions. OIS implementation belongs to loom-oi; it may be embedded by a host platform or run as a standalone service.

### Option A: OIS embedded in loom-server

Loom links or embeds the OIS core through a stable integration interface.

Advantages:
- Fewer services to deploy and operate.
- Direct access to Loom identity references through an adapter.
- Suitable for an initial Loom integration.

Constraints:
- Requires a clear storage and lifecycle boundary.
- OIS implementation must not depend on Loom internal domain types.
- Extraction to a standalone service must remain possible.

### Option B: Standalone OIS service

OIS runs as a separate service and loom-server calls its API.

Advantages:
- Independent deployment and scaling.
- Easier reuse by multiple Agent platforms.
- Clear process and security boundary.

Trade-offs:
- Service authentication and availability management.
- Cross-service failure handling.
- API compatibility and operational overhead.

The implementation should keep both options open. Repository independence does not require immediate standalone deployment.

## 5. Daemon evolution

The existing daemon is the starting point for OIC integration, not a disposable component.

| Existing daemon responsibility | Target responsibility |
|---|---|
| Machine identity and enrollment | OIC identity / endpoint enrollment |
| Existing Loom connection | Loom connectivity adapter; standalone OIC can use OI-native connectivity |
| Machine inventory | OIC capability publication and endpoint status |
| Machine command handling | OIC invocation / control adapter |
| AgentSpec management | Agent Runtime Host |
| Provider discovery | Agent Runtime Host using agent-runtime |
| Agent Worker Supervisor | Agent Runtime Host |
| ACP / Command adapters | Reusable agent-runtime |
| Service Host | Classify by resource type; expose through an extension where appropriate |
| Local daemon IPC | OIC local management API or compatibility layer |

### 5.1 Agent Runtime extension

Agent Runtime remains an optional capability hosted by OIC or by a Loom-specific host integration. It is not part of OIC Core. A controlled endpoint must be able to run OIC without starting an Agent Worker.

The existing Agent Runtime engine remains reusable; the extension/host layer owns its configuration, lifecycle and capability exposure.

### 5.2 Command compatibility

During migration, preserve the existing `loom-daemon` command and configuration as compatibility surfaces. New documentation and interfaces should use OIC terminology. Remove legacy names only after supported scripts, configuration and deployments have migrated.

## 6. Loom Server domain modularization

The Loom server should be reorganized by domain without redesigning Agent or Collaboration business semantics as part of the OI project.

Suggested domains:

- `identity`: authentication and principal resolution.
- `agent`: Agent definitions and configuration.
- `collaboration`: Actor, Channel, Thread, Message and membership.
- `execution`: Task, Run, Delivery and Artifact lifecycle.
- `oi_integration`: OI API integration and Loom-specific subject / transport mapping.

Within complex domains, use domain, application, infrastructure and API boundaries as appropriate. Do not create empty layers mechanically.

Rules:
1. Domain logic does not depend on infrastructure implementations.
2. A domain does not directly access another domain's internal repository or tables.
3. Cross-domain interactions use explicit application interfaces, ports or events.
4. RPC handlers translate protocol requests; they do not own business rules.
5. Keep module internals private and expose stable interfaces.
6. Do not turn shared kernel or common modules into catch-all utility packages.

Agent and Collaboration semantics should remain stable during this refactor. Their code may be reorganized, but business-model redesign is outside the OI migration scope.

## 7. Migration markers

| Area | Change type | Target |
|---|---|---|
| Loom server domain layout | Refactor | Split large handlers and stores by domain |
| OI integration | New | Add adapter to OIS APIs and OI protocol |
| OIC Core | External dependency / integration | Consume OIC SDK or CLI from loom-oi |
| Agent Runtime Host | Refactor | Extract from daemon responsibilities |
| Machine Inventory | Migrate | Map to endpoint and capability views |
| Machine Command | Adapt | Preserve compatibility while mapping to OIC invocation/configuration |
| GUI / CLI | Extend | Add endpoint, capability and binding management |
| Agent / Channel / Thread / Message | Structural refactor only | Preserve existing behavior and semantics |

## 8. Implementation sequence

### P0 — Integration contract
- Agree OI protocol and SDK boundaries.
- Define Loom subject references and transport adapter responsibilities.
- Define compatibility requirements for Machine ID, configuration and commands.

### P1 — OI integration
- Add the Loom integration adapter.
- Map existing Machine management to OI endpoint concepts.
- Add capability and binding management surfaces.
- Keep existing transport and command paths where practical.

### P2 — daemon evolution
- Extract reusable OIC responsibilities to the OIC implementation in loom-oi.
- Move Agent Worker and AgentSpec management behind Agent Runtime Host / extension boundaries.
- Preserve the old daemon command during transition.
- Validate an OIC deployment without Agent Runtime.

### P3 — management clients
- Add endpoint, capability and binding views to GUI / CLI.
- Keep Agent, Channel and collaboration operations on existing Loom APIs.
- Avoid duplicating OIS policy logic in clients.

### P4 — gradual codebase modularization
- Split large handlers and services by domain.
- Preserve behavior through tests and compatibility adapters.
- Remove legacy paths only after consumers have migrated.

## 9. Acceptance criteria

1. Loom can query endpoints and capabilities managed by OIS.
2. Loom Agent identities can be represented as issuer-qualified subjects without duplicating Agent records in OIS.
3. An Agent can be bound to selected capabilities on a controlled endpoint.
4. OIC receives and enforces the applicable policy.
5. OIS exposes desired and observed state.
6. Agent operations continue through Loom's execution and collaboration system.
7. Existing Agent Runtime hosting remains functional during migration.
8. A standalone OIC can connect without requiring a Loom Agent Runtime.

## 10. Summary

Loom consumes OI as an independent controlled-resource infrastructure project. loom-server retains Agent and collaboration ownership; OIS manages endpoints, capabilities, bindings and state; OIC provides controlled-side connectivity and dispatch; Extensions implement concrete operations.

The migration should preserve existing Loom behavior, reuse suitable Machine / daemon mechanisms, and introduce clear integration boundaries before extracting or replacing existing components.
