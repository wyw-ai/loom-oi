# loom-oi Architecture

**Status:** Proposed  
**Scope:** OIS, OIC, extension model and platform integrations

## 1. Purpose

loom-oi provides infrastructure for managing controlled endpoints and the capabilities they expose. A controlled endpoint may be a machine, application, process, runtime host or remote service. It does not have to run an Agent and does not have to map one-to-one to a physical machine.

The system separates controlled-resource management from Agent collaboration and task orchestration.

- OIS manages controlled endpoints and their configuration, capabilities, authorization and state.
- OIC connects a controlled endpoint to OIS and exposes local or reachable resources.
- Extensions implement resource-specific operations.
- Integrations connect OI to external platforms and identity/transport systems.

## 2. Architecture overview

~~~text
       Management Platform / Agent Platform
                     |
              Integration API
                     |
              +------+------+
              |     OIS     |
              | Endpoint    |
              | Capability  |
              | Binding     |
              | Policy      |
              | Reconcile   |
              +------+------+
                     |
             OIC Control Protocol
                     |
              +------+------+
              |     OIC     |
              | Identity    |
              | Connectivity|
              | Dispatcher  |
              | Policy      |
              | Extension  |
              | Host        |
              +------+------+
                     |
                 Extensions
~~~

This is a logical architecture. OIS may be deployed as a service or embedded by a host platform. OIC may run as a CLI process or be embedded through an SDK.

## 3. OIS

OIS is the controlled-resource management plane.

Responsibilities:

- Enroll and identify OIC clients and controlled endpoints.
- Maintain endpoint metadata and lifecycle state.
- Maintain capability declarations, versions, readiness and availability.
- Manage bindings between external control subjects and endpoint capabilities.
- Store desired configuration and authorization policy.
- Track observed/applied configuration reported by OIC.
- Expose management and resource-view APIs to integrating platforms.

OIS does not own Agent identity, channels, messages, tasks, runs or Agent execution sessions. External subject identities are represented by issuer-qualified references.

## 4. OIC

OIC is the controlled-side SDK / CLI / runtime.

Responsibilities:

- OIC identity, enrollment and authentication.
- Secure connection to OIS.
- Connection negotiation and direct-connect attempts where supported.
- Relay fallback when direct connectivity is unavailable.
- Capability registration and status reporting.
- Receiving control requests and dispatching them to extensions.
- Applying local authorization constraints.
- Extension lifecycle and health reporting.

OIC must be usable without an Agent Runtime. It can run on a user's machine, on a server, alongside an existing application, or embedded in a host application through an SDK.

## 5. Extension model

Extensions implement concrete resource operations. OIC Core provides the extension contract and invocation substrate, not application-specific business logic.

An extension manifest should identify:

- Extension ID and version.
- Declared capabilities and schemas.
- Required permissions and resource scopes.
- Runtime and platform requirements.
- Lifecycle and health behavior.

Initial extension examples:

| Extension | Example capabilities |
|---|---|
| Agent Runtime | Host and supervise Loom-compatible Agent workers |
| Shell | Bash, PowerShell and command execution |
| Computer Use | Screenshot, mouse and keyboard |
| Application Automation | Control locally installed applications such as WeChat or QQ |
| HTTP | Access configured HTTP endpoints |
| Magpie | Expose authorized Magpie gateway operations |
| OpenConnector | Reuse supported Provider / Action integrations |
| Custom | User-defined application, device or service operations |

High-impact extensions must be explicitly enabled and constrained by local permissions.

## 6. Core management model

### 6.1 Endpoint

An endpoint is a managed logical target associated with an OIC identity or an OIC-hosted connector. It has an owner, metadata, connection state, configuration and observed status.

A physical host may expose multiple logical endpoints. An endpoint may also represent a remote service reachable from the OIC host.

### 6.2 Capability

A capability is a versioned contract for an operation or operation set. It includes an identifier, description, input/output schema, required scopes, readiness and relevant constraints.

Capability describes what a resource can do, not how an Agent should plan a task.

### 6.3 Binding

A binding grants an external subject permission to use selected capabilities on a target endpoint under specified constraints.

Example:

~~~yaml
binding:
  subject:
    issuer: loom
    type: agent
    id: agent-001
  target:
    endpoint: desktop-01
  allow:
    - desktop.screenshot
    - desktop.mouse.click
  constraints:
    require_owner_approval: true
~~~

OIS stores the external subject reference; it does not become the identity authority for the external platform.

### 6.4 Desired and observed state

OIS distinguishes desired configuration from observed/applied state. Saving a policy or configuration does not imply that the endpoint has applied it. OIC reports application results and current status.

## 7. Connectivity

OIC connectivity is a core infrastructure concern.

The connection layer should support:

1. Outbound enrollment and connection from OIC.
2. Strong client authentication and encrypted transport.
3. Connection negotiation and direct-connect attempts where available.
4. Relay fallback for restrictive NAT or firewall environments.
5. Reconnection and state resynchronization.
6. Multiplexing management and invocation traffic where appropriate.

Direct connectivity is an optimization, not an availability guarantee. Relay is required for environments where direct connectivity cannot be established.

Connectivity and authorization remain separate: a reachable endpoint is not necessarily authorized for a given operation.

## 8. Integration model

OI is platform-neutral. Integrations adapt external identity, management APIs and transport systems without making those systems dependencies of OIS or OIC Core.

### 8.1 Loom integration

Loom is the first integration:

- loom-server remains authoritative for Agent / Actor identities and collaboration objects.
- OIS stores issuer-qualified references to Loom subjects for bindings.
- The Loom integration maps existing Machine / daemon transport and command delivery to OIC control messages.
- OIC can initially reuse suitable Loom connectivity through an adapter.
- Standalone OIC deployments use OI-native connectivity without requiring loom-server.
- Loom GUI and CLI expose OI management through the Loom integration surface.

### 8.2 Other integrations

Other Agent platforms or management systems can integrate by implementing the OI management API and subject-reference contract. They do not need to adopt Loom's Actor, Channel or Message models.

## 9. Security

- OIC authenticates as a controlled client, not as an Agent.
- OIS authorizes operations using issuer-qualified subject references and explicit bindings.
- Network reachability does not imply permission.
- OIC enforces applicable local constraints at invocation time.
- Revocation must prevent subsequent unauthorized operations at the controlled endpoint.
- Extensions receive only the permissions required for their declared capabilities.
- Shell, desktop input and application automation are high-impact capabilities and should be disabled by default or require explicit consent.
- Provider credentials and application secrets remain with the component that owns them; OI does not create a shared credential pool.

## 10. Non-goals

OI does not provide Agent creation or lifecycle management, Channel / Thread / Message management, Task / Run / Agent Session orchestration, general workflow orchestration, or a replacement for an Agent Runtime. It does not require a built-in implementation of every business integration.

## 11. Initial implementation scope

The first end-to-end implementation should prove:

1. OIC enrollment and authenticated connection to OIS.
2. Capability declaration by an extension.
3. Endpoint and capability registration.
4. Binding an external subject to selected capabilities.
5. Policy/configuration delivery to OIC.
6. Local enforcement by OIC.
7. Desired/observed state reporting.
8. Invocation and result delivery through an integration adapter.

Start with one OIC endpoint and one low-risk extension. Add Shell and Computer Use only after identity, authorization, revocation and audit behavior is validated.

## 12. Summary

OIS manages controlled endpoints, capabilities, bindings, configuration and state. OIC connects endpoints to OIS and dispatches authorized operations to extensions. Extensions implement concrete business capabilities. Integrations connect OI to Agent platforms and existing transport systems.

**loom-oi is controlled-endpoint infrastructure, not an Agent collaboration or orchestration platform.**
