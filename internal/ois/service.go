package ois

import (
	"fmt"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type ServiceAction struct {
	Name        string         `yaml:"name,omitempty" json:"name"`
	Source      Source         `yaml:"source" json:"source"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	InputSchema map[string]any `yaml:"input_schema,omitempty" json:"input_schema"`
}

type Publish struct {
	MCP bool `yaml:"mcp" json:"mcp"`
}

type Service struct {
	Actions []ServiceAction `yaml:"actions" json:"actions"`
	Access  Access          `yaml:"access" json:"access"`
	Publish Publish         `yaml:"publish" json:"publish"`
}

func (svc Service) defaultNames() {
	for i := range svc.Actions {
		if svc.Actions[i].Name == "" {
			svc.Actions[i].Name = svc.Actions[i].Source.Action
		}
	}
}

func validateService(id string, svc Service, channels map[string]Channel, server ServerConfig) error {
	if !validID(id) || svc.Actions == nil {
		return fmt.Errorf("service requires a valid ID and actions array")
	}
	if svc.Access.RequireHTTPS && server.httpsURL() == "" {
		return fmt.Errorf("service %s requires HTTPS but no HTTPS listener is configured", id)
	}
	seen := make(map[string]bool)
	for _, member := range svc.Actions {
		if !validID(member.Name) || seen[member.Name] {
			return fmt.Errorf("service %s requires distinct valid member names", id)
		}
		if _, ok := channels[member.Source.Channel]; !ok || !validID(member.Source.App) || !validID(member.Source.Action) {
			return fmt.Errorf("service %s member %s requires a known channel and valid app/action", id, member.Name)
		}
		seen[member.Name] = true
	}
	return nil
}

// Keep beta management writes effective while a migrated service is still
// managed through the old Tool and vMCP resources. A native PUT takes ownership
// of that service; from then on global Tool edits cannot change its members.
func (st *persisted) syncLegacyServices() {
	for id, vm := range st.VMCPS {
		svc := Service{Actions: make([]ServiceAction, 0, len(vm.Tools)), Access: vm.Access, Publish: Publish{MCP: true}}
		for _, name := range vm.Tools {
			t := st.Tools[name]
			svc.Actions = append(svc.Actions, ServiceAction{Name: name, Source: t.Source, Description: t.Description, InputSchema: t.InputSchema})
		}
		st.Services[id] = svc
	}
}

// Unqualified contracts from bootstrap YAML can be resolved after the OIC
// registers. Explicit input schemas (including migrated beta schemas) win.
func (s *Server) resolveMemberLocked(member ServiceAction) ServiceAction {
	if member.InputSchema == nil {
		action, _ := s.catalogActionLocked(member.Source)
		member.InputSchema = action.InputSchema
		if member.Description == "" {
			member.Description = action.Description
		}
	}
	return member
}

func (s *Server) serviceViewLocked(id string, svc Service) map[string]any {
	actions := make([]ServiceAction, 0, len(svc.Actions))
	for _, member := range svc.Actions {
		actions = append(actions, s.resolveMemberLocked(member))
	}
	view := map[string]any{"id": id, "actions": actions, "publish": svc.Publish, "access": svc.Access}
	if svc.Publish.MCP {
		view["endpoint"] = s.serviceEndpoint(id, svc.Access)
	}
	return view
}

func (s *Server) accessedServiceLocked(id, bearer string, secure, mcp bool) (Service, string, *protocol.Error) {
	svc, ok := s.state.Services[id]
	if !ok || (mcp && !svc.Publish.MCP) {
		return Service{}, "", protocol.Err("NOT_FOUND", "service does not exist at this endpoint")
	}
	identity, err := authorizeAccess(id, svc.Access, bearer, secure)
	return svc, identity, err
}
