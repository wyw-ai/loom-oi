# loom-oi Architecture

**Status:** Draft  
**Scope:** OIC / OIS architecture and integration boundary with Loom

## 1. Purpose

loom-oi manages the controlled side of Loom. It provides a consistent way to enroll controlled endpoints, describe the capabilities they expose, configure and govern those capabilities, bind them to authorized Agents, and observe whether the controlled configuration is in effect.

loom-oi is not a second Agent platform, collaboration server, or general task orchestration engine.

The responsibility boundary is:

- **loom-server manages Agents and their interactions.**
- **loom-oi manages controlled resources and the rules under which they can be controlled.**
- **OIC extensions implement the actual resource-specific business operations.**

## 2. Two components: OIS and OIC

### 2.1 OIS — Loom OI Server

OIS is the controlled-resource management plane.

Its responsibilities are:

1. Enroll and identify OIC clients.
2. Maintain the registry of controlled endpoints and their reported capabilities.
3. Manage capability configuration and exposure.
4. Maintain bindings between Loom control subjects (such as Agent references) and controlled resources.
5. Define and distribute authorization constraints.
6. Track desired configuration versus observed/applied state.
7. Report resource availability and configuration status to loom-server and management clients.

OIS does not own Agent identity, channels, threads, messages, tasks, runs, or Agent execution sessions. Agent references are supplied by loom-server; OIS stores only the references and authorization relationships it needs.

### 2.2 OIC — Loom OI Client

OIC is the controlled-side SDK / CLI and extension host.

It can be deployed in different places:

- Installed as a CLI on a user's machine.
- Embedded as an SDK in a user's own application or service.
- Run alongside a controlled resource.
- Hosted inside OIS when OIS itself needs to expose a connector to a remote HTTP service.

OIC Core is responsible for:

- OIS enrollment and client identity.
- Secure connection establishment to OIS.
- NAT traversal / connection negotiation and relay fallback.
- Capability registration and status reporting.
- Receiving authorized control requests.
- Dispatching requests to local extensions.
- Applying local authorization constraints.
- Extension loading and lifecycle.
- Reporting operation outcome and resource state.

OIC does not need to understand Agents, Channels, Threads, or Loom collaboration semantics.

## 3. OIC Core and extensions

OIC Core should remain small and stable. It provides the common substrate that every controlled endpoint needs, while extensions provide resource-specific behavior.

### 3.1 OIC Core

| Core function | Responsibility |
|---|---|
| Identity | OIC identity, enrollment and authentication |
| Connectivity | Persistent secure connection to OIS |
| NAT traversal | Connection negotiation and direct-connect attempts |
| Relay | Fallback when direct connectivity is unavailable |
| Capability exposure | Publish extension-provided capabilities |
| Invocation dispatch | Route authorized requests to the correct extension |
| Policy enforcement | Enforce the effective local control scope |
| Lifecycle | Start, stop, reconnect and report status |
| Extension host | Load and supervise extensions |

The primary infrastructure value of OIC is its secure, reliable connection to OIS and its common extension contract. It should not accumulate application-specific business logic.

### 3.2 Extensions

Extensions implement the actual operations on controlled resources.

| Extension | Example capabilities |
|---|---|
| Shell | Bash, PowerShell, command execution |
| Computer Use | Screenshot, mouse, keyboard, desktop interaction |
| Application automation | WeChat, QQ and other desktop applications |
| Browser | Browser control and automation |
| HTTP | REST API and remote HTTP service access |
| Magpie | Access to a Magpie gateway |
| Docker | Container operations |
| Kubernetes | Cluster operations |
| Custom | User-defined application or device integration |

A user may integrate the OIC SDK into an application and register capabilities implemented by that application. A community extension may package the same integration for reuse.

OIC defines the extension contract; it does not prescribe the internal implementation language or framework of every extension.

## 4. Deployment models

### 4.1 OIC on a user's machine

~~~text
User Machine
  |
  +-- OIC CLI / SDK
        |
        +-- OIC Core
        +-- Shell Extension
        +-- Computer Use Extension
        +-- Application Extension
        |
        +==== secure connection ==== OIS
~~~

This model allows the user's machine to expose local resources such as shell execution, desktop control, local files, and installed applications. Each capability must be explicitly enabled and scoped.

### 4.2 OIC embedded in a user application

~~~text
User Application
  |
  +-- OIC SDK
        |
        +-- application-defined capabilities
        |
        +==== secure connection ==== OIS
~~~

The application controls its own implementation and exposes selected operations through the OIC extension contract. OIC does not require the application to run a separate agent runtime.

### 4.3 OIC hosted inside OIS

~~~text
OIS
  |
  +-- OIC Core
        |
        +-- HTTP Connector
        +-- Remote Service Connector
        |
        +---- outbound HTTP / HTTPS ----> Remote Service
~~~

This model is useful when the controlled resource is a remote HTTP endpoint that OIS can reach directly. The embedded OIC instance exposes that endpoint through the same controlled-resource model.

The three deployment models share the same OIC identity, capability and invocation semantics. Their physical placement and available extensions differ.

## 5. Core management model

The initial public model should remain deliberately small.

### 5.1 Controlled endpoint

A controlled endpoint represents an OIC instance or a managed logical resource exposed by it.

It has an identity, owner, connection state, metadata, configuration and reported status. It is not required to map one-to-one to a physical machine.

One host may run multiple logical OIC endpoints, and one OIC endpoint may expose multiple capabilities.

### 5.2 Capability

A capability describes an operation or operation set exposed by an OIC extension.

A capability declaration should include:

- Stable identifier and version.
- Human-readable name and description.
- Input and output schema.
- Supported operations.
- Required permission scopes.
- Availability and readiness.
- Relevant resource constraints.

Capability describes what the controlled side can do. It does not describe how an Agent should plan or sequence a task.

### 5.3 Binding

A Binding defines which Loom control subject may use which capability on which controlled endpoint, and under what constraints.

Conceptually:

~~~yaml
binding:
  subject:
    agentRef: agent-a

  target:
    endpointRef: desktop-01

  allow:
    - desktop.screenshot
    - desktop.mouse.click

  constraints:
    requireOwnerApproval: true
~~~

The Agent identity remains owned by loom-server. OIS stores the reference and manages the controlled-resource authorization relationship.

### 5.4 Desired and observed state

OIS should distinguish between:

- **Desired state:** the configuration and authorization the administrator wants.
- **Observed state:** what OIC and its extensions report as actually applied and available.

A configuration update is not complete merely because OIS saved it. OIS should expose whether the controlled endpoint acknowledged and applied the change.

## 6. OIC–OIS connectivity

OIC's connection to OIS is a foundational responsibility.

The connection mechanism should support:

1. Outbound connection establishment from OIC, so a controlled endpoint does not need a public inbound port.
2. Secure client identity and authentication.
3. Connection negotiation and direct-connect attempts where possible.
4. Relay fallback when direct connectivity fails.
5. Reconnection and state resynchronization.
6. Multiplexing of management and control messages over the OIC–OIS connection.

Direct connectivity and relay are transport choices. They do not change the capability model or authorization semantics.

OIC–OIS connectivity is separate from loom-server's Agent, channel and collaboration connections. loom-server integrates with OIS through its management API; it does not need to become the OIC connection coordinator.

## 7. Integration with Loom

### 7.1 loom-server

loom-server remains the authority for:

- Agent and Actor identity.
- Channels, Threads and Messages.
- Tasks, Runs and collaboration state.
- Agent execution and routing.
- Existing daemon and actor connection management.

loom-server integrates with OIS to:

- Query the controlled resources available to a particular Agent.
- Read capability and availability information.
- Create, update and revoke Agent-to-resource bindings through OIS.
- Obtain configuration application and resource status.
- Provide the resulting controlled-resource view to Agents and clients.

OIS must not create a parallel Agent registry or duplicate the collaboration journal.

### 7.2 loom-gui and loom-cli

GUI and CLI remain the user-facing entry points.

They should present a coherent Loom experience while keeping ownership clear:

- Agent, Channel, Thread and Run operations go to loom-server.
- Controlled endpoint, capability and binding management go to OIS, normally through the Loom integration surface.
- GUI and CLI should not independently reimplement OIS policy or resource reconciliation.

### 7.3 loom-daemon

Where the existing loom-daemon is deployed on a controlled host, OIC functionality may be integrated into it or run alongside it.

The implementation should reuse suitable existing process management and host integration, but preserve a clear module boundary:

- Existing daemon responsibilities continue to serve Loom Agent runtime hosting.
- OIC responsibilities serve controlled-resource enrollment, capability exposure, local dispatch and OIS connectivity.

A separate OIC process is also valid when the deployment requires independent lifecycle, permissions, or SDK embedding. The architecture should not force every OIC deployment to depend on loom-daemon.

## 8. End-to-end interaction

### 8.1 Enrollment and capability publication

1. An administrator installs or embeds OIC.
2. OIC authenticates to OIS and enrolls.
3. OIC loads enabled extensions.
4. Extensions declare capabilities and schemas.
5. OIS records the endpoint and its reported capabilities.
6. OIS tracks connection, health and applied configuration.

### 8.2 Binding an Agent to a resource

1. A user selects an existing Agent in Loom.
2. loom-server supplies the Agent reference to the OIS integration.
3. The user selects a controlled endpoint and allowed capabilities.
4. OIS stores the Binding and desired policy.
5. OIS distributes the relevant policy to OIC.
6. OIC applies the local restrictions and reports the observed state.
7. loom-server can query the effective resource view for that Agent.

### 8.3 Agent uses a controlled resource

1. Agent asks loom-server for its available controlled-resource view.
2. loom-server obtains the view from OIS.
3. Agent selects a capability and issues an operation through Loom's existing execution flow.
4. The request is routed to the corresponding OIC through the OIC–OIS control path.
5. OIC validates the current authorization and dispatches to the extension.
6. The extension performs the resource-specific operation.
7. OIC returns the result through OIS; loom-server delivers it to the Agent and records the collaboration result.

The exact data path may be optimized later. Direct OIC communication can be introduced where useful, but OIS remains the authority for endpoint identity, capability policy and binding state.

## 9. Security boundary

- OIC authenticates as a controlled client, not as an Agent.
- OIS authorizes controlled-resource access using Agent references issued by loom-server.
- A network connection does not imply permission to invoke a capability.
- OIC enforces local constraints even if a request was previously visible to loom-server.
- Revocation must invalidate subsequent operations at the controlled side.
- Extensions receive only the permissions needed for their declared capabilities.
- Shell, desktop input, application automation and other high-impact capabilities should be disabled by default or require explicit enablement.
- Provider credentials and application secrets remain with the system that owns them; OIC does not turn them into a shared credential pool.

## 10. Explicit non-goals

The following remain outside loom-oi's responsibility:

- Agent creation, identity and lifecycle.
- Channel, Thread and Message management.
- Task, Run and Agent Session orchestration.
- Agent-to-Agent collaboration and handoff.
- General workflow orchestration.
- A second connection system for Loom actors and channels.
- A built-in catalogue of all business-specific tools.
- Reimplementation of Magpie, Computer Use, shell tools or application automation.

loom-oi manages the controlled side and exposes a stable way to govern it. Business behavior belongs in OIC extensions and the systems that consume them.

## 11. Initial implementation focus

The first implementation should validate the smallest complete management loop:

1. OIC can enroll and establish a secure connection to OIS.
2. OIC can publish a capability provided by an extension.
3. OIS can register the endpoint and capability.
4. An Agent reference from loom-server can be bound to that capability.
5. OIS can distribute the binding policy to OIC.
6. OIC enforces the policy locally.
7. OIS reports desired and observed state.
8. loom-server can query the effective resource view.

A minimal proof should use one OIC endpoint and one simple extension first. Shell execution and Computer Use should be separate opt-in extensions, not mandatory OIC Core features.

## 12. Architectural summary

**OIC is the controlled-side SDK / CLI and connectivity runtime. OIS is the controlled-resource management plane. loom-server remains the Agent and collaboration platform.**

OIC Core solves identity, secure connectivity, connection establishment, capability exposure and local dispatch. OIC extensions implement shell, Computer Use, WeChat / QQ automation, HTTP connectors, Magpie access and other resource-specific operations.

OIS manages endpoint enrollment, capability inventory, bindings, policy, configuration and observed state. loom-server integrates with OIS so Agents can discover and use only the resources they are authorized to control.

The defining boundary is:

**loom-server manages Agents and interactions; OIS manages controlled resources; OIC connects to OIS and makes those resources controllable.**
