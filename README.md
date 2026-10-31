# loom-oi

**loom-oi is infrastructure for managing and connecting controlled endpoints.**

It provides a consistent model for enrolling OIC clients, exposing capabilities, managing authorization bindings, applying configuration, and observing endpoint state across network boundaries.

## Components

- **OIS — Loom OI Server:** the control plane for OIC clients and the resources they expose.
- **OIC — Loom OI Client:** a controlled-side SDK / CLI / runtime that connects to OIS, publishes capabilities, enforces local policy, and dispatches requests to extensions.
- **Extensions:** resource-specific implementations such as shell execution, desktop automation, application automation, HTTP connectors, and runtime hosting.
- **Integrations:** adapters between OI and external platforms, identity systems and transport mechanisms.

## Responsibility boundaries

| Component | Responsibility |
|---|---|
| OIS | Endpoint enrollment, capability inventory, bindings, desired configuration, policy and observed state |
| OIC | Client identity, secure connectivity, capability exposure, local dispatch, extension lifecycle and local enforcement |
| Extensions | Concrete operations against machines, applications, runtimes and services |
| Integrations | Adapt external identity, APIs and transport systems, including Loom |

Loom is the first integration platform:

- **loom-server** remains responsible for Agents, Actors, Channels, Threads, Messages, Tasks, Runs and collaboration/execution flows.
- The Loom integration maps Loom subjects and existing transport mechanisms to OIS/OIC concepts.
- Existing Loom daemon functionality can be migrated into OIC Core and the official Agent Runtime Extension.

## Design principles

1. Keep OIC Core small; put resource-specific behavior in extensions.
2. Keep OIS independent of any single Agent platform or collaboration model.
3. Use explicit capability contracts and authorization bindings.
4. Separate desired configuration from observed/applied state.
5. Prefer outbound OIC connectivity; support direct connectivity where possible and relay fallback where required.
6. Treat network reachability and authorization as separate concerns.
7. Allow OIC to run standalone or be embedded into a host application.

## Documentation

- [Architecture](docs/architecture.md)
- [OI Technical Review](docs/oi-technical-review.md)
- [Loom Integration and Migration](docs/loom-architecture-v2.md)
