# loom-oi

**loom-oi is the controlled-resource management layer for Loom.**

It manages the controlled side: how resources join the system, what they expose, who may control them, how their configuration is applied, and whether they are currently available.

loom-oi has two parts:

- **OIS — Loom OI Server:** the management plane for OIC clients and the resources they expose.
- **OIC — Loom OI Client:** the controlled-side SDK / CLI that connects to OIS, exposes local or remote capabilities, and dispatches authorized operations to extensions.

loom-server remains responsible for Agents and collaboration: actors, channels, threads, messages, tasks, runs, and the existing Agent execution flow. It integrates with OIS to obtain the resource view and manage which resources an Agent may control.

## Responsibility boundary

| Component | Owns |
|---|---|
| loom-server | Agent and collaboration model, channels, messages, tasks, runs, existing Agent execution and routing |
| OIS | OIC enrollment, controlled-resource registry, capability declarations, Agent-to-resource bindings, desired configuration, policy and observed status |
| OIC | OIS identity and connectivity, capability exposure, local dispatch, extension lifecycle, local policy enforcement |
| OIC extensions | Actual business/resource operations such as shell, desktop automation, application automation, HTTP and service-specific integrations |
| loom-gui / loom-cli | User-facing management and operation entry points, integrated through Loom's existing server/API surface |

## Core idea

**loom-server manages what Agents do. loom-oi manages what Agents are allowed to control.**

OIC is deliberately small at its core. Its defining responsibility is secure connectivity to OIS—including connection establishment across NAT and relay fallback—plus a stable extension contract. Business-specific capabilities are supplied by extensions and the community rather than being built into OIC Core.

See [Architecture](docs/architecture.md) for the system model, interactions, and lifecycle.
