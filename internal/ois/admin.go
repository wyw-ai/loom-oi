package ois

import (
	"net/http"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func (s *Server) admin(w http.ResponseWriter, r *http.Request) {
	if !matchesEnv(token(r), s.cfg.Admin.TokenEnv) {
		writeError(w, 401, "", protocol.Err("UNAUTHENTICATED", "management credentials are required"))
		return
	}
	p := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	if r.Method == http.MethodGet {
		s.adminGet(w, r, p)
		return
	}
	if len(p) != 2 || !validID(p[1]) || (p[0] != "channels" && p[0] != "tools" && p[0] != "vmcps" && p[0] != "services") {
		writeError(w, 404, "", protocol.Err("NOT_FOUND", "management resource does not exist"))
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.WriteHeader(405)
		return
	}
	var ch Channel
	var tool Tool
	var vm VMCP
	var svc Service
	if r.Method == http.MethodPut {
		var dst any
		switch p[0] {
		case "channels":
			dst = &ch
		case "tools":
			dst = &tool
		case "services":
			dst = &svc
		case "vmcps":
			dst = &vm
		}
		if err := readJSON(w, r, dst); err != nil {
			writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "invalid resource JSON"))
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	id := p[1]
	status := 200
	fail := func(code, msg string) { e := protocol.Err(code, msg); writeError(w, errorStatus(e), "", e) }
	var response any
	switch p[0] {
	case "channels":
		old, exists := next.Channels[id]
		if r.Method == http.MethodDelete {
			if !exists {
				fail("NOT_FOUND", "channel does not exist")
				return
			}
			for _, t := range next.Tools {
				if t.Source.Channel == id {
					fail("RESOURCE_IN_USE", "channel is referenced by a tool")
					return
				}
			}
			for _, service := range next.Services {
				for _, member := range service.Actions {
					if member.Source.Channel == id {
						fail("RESOURCE_IN_USE", "channel is referenced by a service")
						return
					}
				}
			}
			if s.live[id] != nil {
				fail("CHANNEL_CONNECTED", "stop the connected OIC before deleting its channel")
				return
			}
			delete(next.Channels, id)
		} else {
			if exists && s.live[id] != nil && (old.CredentialEnv != ch.CredentialEnv || old.ExpectedHostID != ch.ExpectedHostID) {
				fail("CHANNEL_CONNECTED", "identity cannot change while the channel is connected")
				return
			}
			if !exists {
				status = 201
			}
			next.Channels[id] = ch
			response = map[string]any{"id": id, "description": ch.Description, "expected_host_id": ch.ExpectedHostID, "credential_env": ch.CredentialEnv}
		}
	case "tools":
		old, exists := next.Tools[id]
		if r.Method == http.MethodDelete {
			if !exists {
				fail("NOT_FOUND", "tool does not exist")
				return
			}
			for _, v := range next.VMCPS {
				for _, name := range v.Tools {
					if name == id {
						fail("RESOURCE_IN_USE", "tool is referenced by a vMCP")
						return
					}
				}
			}
			delete(next.Tools, id)
		} else {
			if exists && old.Source != tool.Source {
				fail("BINDING_IMMUTABLE", "create a new tool instead of changing its source")
				return
			}
			if !exists {
				status = 201
			}
			next.Tools[id] = tool
			response = map[string]any{"name": id, "description": tool.Description, "input_schema": tool.InputSchema, "source": tool.Source}
		}
	case "services":
		_, exists := next.Services[id]
		if r.Method == http.MethodDelete {
			if !exists {
				fail("NOT_FOUND", "service does not exist")
				return
			}
			delete(next.Services, id)
		} else {
			svc.defaultNames()
			for i, member := range svc.Actions {
				svc.Actions[i] = s.resolveMemberLocked(member)
			}
			if !exists {
				status = 201
			}
			next.Services[id] = svc
			response = s.serviceViewLocked(id, svc)
		}
		delete(next.VMCPS, id)
	case "vmcps":
		_, exists := next.VMCPS[id]
		if r.Method == http.MethodDelete {
			if !exists {
				fail("NOT_FOUND", "vMCP does not exist")
				return
			}
			delete(next.VMCPS, id)
			delete(next.Services, id)
		} else {
			if _, native := next.Services[id]; native && !exists {
				fail("RESOURCE_IN_USE", "identifier is owned by a service; update it through /services")
				return
			}
			if !exists {
				status = 201
			}
			next.VMCPS[id] = vm
			response = map[string]any{"id": id, "tools": vm.Tools, "access": vm.Access, "endpoint": s.vmcpEndpoint(id, vm)}
		}
	}
	next.syncLegacyServices()
	if err := validateState(next, s.cfg.Server); err != nil {
		fail("INVALID_REQUEST", err.Error())
		return
	}
	if err := writeState(s.cfg.Server.DataFile, next); err != nil {
		fail("INTERNAL_ERROR", "cannot persist configuration")
		return
	}
	if p[0] == "channels" {
		old := s.state.Channels[id]
		if r.Method == http.MethodDelete || old.ExpectedHostID != ch.ExpectedHostID || old.CredentialEnv != ch.CredentialEnv {
			delete(s.snapshots, id)
		}
	}
	s.state = next
	if r.Method == http.MethodDelete {
		w.WriteHeader(204)
		return
	}
	writeJSON(w, status, response)
}

func (s *Server) adminGet(w http.ResponseWriter, r *http.Request, p []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	notFound := func() { writeError(w, 404, "", protocol.Err("NOT_FOUND", "resource does not exist")) }
	items := make([]any, 0)
	if len(p) == 1 && p[0] == "status" {
		available := 0
		registeredApps := 0
		availableApps := 0
		registeredActions := 0
		availableActions := 0
		for _, t := range s.state.Tools {
			if s.availableLocked(t.Source) {
				available++
			}
		}
		hostOnline := make(map[string]bool)
		for ch, snap := range s.snapshots {
			live := s.live[ch] != nil
			hostOnline[snap.Host.ID] = hostOnline[snap.Host.ID] || live
			for _, app := range snap.Apps {
				registeredApps++
				if live && s.live[ch].registered && app.Ready {
					availableApps++
				}
				for _, action := range app.Actions {
					registeredActions++
					if s.availableLocked(Source{ch, app.App, action.Name}) {
						availableActions++
					}
				}
			}
		}
		onlineHosts := 0
		for _, online := range hostOnline {
			if online {
				onlineHosts++
			}
		}
		writeJSON(w, 200, map[string]any{
			"status": "running", "hosts": map[string]int{"registered": len(hostOnline), "online": onlineHosts},
			"channels": map[string]int{"configured": len(s.state.Channels), "online": len(s.live)},
			"tools":    map[string]int{"configured": len(s.state.Tools), "available": available},
			"apps":     map[string]int{"registered": registeredApps, "available": availableApps},
			"actions":  map[string]int{"registered": registeredActions, "available": availableActions},
			"vmcps":    map[string]int{"configured": len(s.state.VMCPS)},
			"services": map[string]int{"configured": len(s.state.Services)},
		})
		return
	}
	switch p[0] {
	case "channels":
		if len(p) == 1 {
			for _, id := range keys(s.state.Channels) {
				items = append(items, s.channelViewLocked(id))
			}
			break
		}
		if _, ok := s.state.Channels[p[1]]; !ok {
			notFound()
			return
		}
		if len(p) == 2 {
			writeJSON(w, 200, s.channelViewLocked(p[1]))
			return
		}
		if len(p) == 3 && p[2] == "apps" {
			writeJSON(w, 200, map[string]any{"channel": p[1], "online": s.live[p[1]] != nil, "items": s.appViewsLocked("", p[1])})
			return
		}
		if (len(p) == 5 || len(p) == 6) && p[2] == "apps" && p[4] == "actions" {
			actions := s.actionViewsLocked("", p[1], p[3])
			if len(p) == 5 {
				writeJSON(w, 200, map[string]any{"items": actions})
				return
			}
			for _, a := range actions {
				if a["action"] == p[5] {
					writeJSON(w, 200, a)
					return
				}
			}
		}
		notFound()
		return
	case "tools":
		view := func(id string) any {
			t := s.state.Tools[id]
			return map[string]any{"name": id, "description": t.Description, "input_schema": t.InputSchema, "source": t.Source, "available": s.availableLocked(t.Source)}
		}
		if len(p) == 1 {
			for _, id := range keys(s.state.Tools) {
				items = append(items, view(id))
			}
			break
		}
		if len(p) == 2 {
			if _, ok := s.state.Tools[p[1]]; ok {
				writeJSON(w, 200, view(p[1]))
				return
			}
		}
		notFound()
		return
	case "services":
		if len(p) == 1 {
			for _, id := range keys(s.state.Services) {
				items = append(items, s.serviceViewLocked(id, s.state.Services[id]))
			}
			break
		}
		if len(p) == 2 {
			if svc, ok := s.state.Services[p[1]]; ok {
				writeJSON(w, 200, s.serviceViewLocked(p[1], svc))
				return
			}
		}
		notFound()
		return
	case "vmcps":
		view := func(id string) any {
			v := s.state.VMCPS[id]
			return map[string]any{"id": id, "tools": v.Tools, "access": v.Access, "endpoint": s.vmcpEndpoint(id, v)}
		}
		if len(p) == 1 {
			for _, id := range keys(s.state.VMCPS) {
				items = append(items, view(id))
			}
			break
		}
		if len(p) == 2 {
			if _, ok := s.state.VMCPS[p[1]]; ok {
				writeJSON(w, 200, view(p[1]))
				return
			}
		}
		notFound()
		return
	case "hosts":
		hosts := s.hostViewsLocked()
		if len(p) == 1 {
			for _, id := range keys(hosts) {
				items = append(items, hosts[id])
			}
			break
		}
		if _, ok := hosts[p[1]]; !ok {
			notFound()
			return
		}
		if len(p) == 2 {
			writeJSON(w, 200, hosts[p[1]])
			return
		}
		if len(p) == 3 && p[2] == "apps" {
			writeJSON(w, 200, map[string]any{"items": s.appViewsLocked(p[1], "")})
			return
		}
		if len(p) == 3 && p[2] == "actions" {
			writeJSON(w, 200, map[string]any{"items": s.actionViewsLocked(p[1], "", "")})
			return
		}
		notFound()
		return
	case "actions":
		if len(p) != 1 {
			notFound()
			return
		}
		writeJSON(w, 200, map[string]any{"items": s.actionViewsLocked(r.URL.Query().Get("host_id"), r.URL.Query().Get("channel"), r.URL.Query().Get("app"))})
		return
	default:
		notFound()
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) channelViewLocked(id string) any {
	c := s.state.Channels[id]
	return map[string]any{"id": id, "description": c.Description, "expected_host_id": c.ExpectedHostID, "credential_env": c.CredentialEnv, "online": s.live[id] != nil}
}

func (s *Server) hostViewsLocked() map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, ch := range keys(s.snapshots) {
		snap := s.snapshots[ch]
		v := result[snap.Host.ID]
		if v == nil {
			v = map[string]any{"id": snap.Host.ID, "name": snap.Host.Name, "os": snap.Host.OS, "arch": snap.Host.Arch, "online": false, "channels": []any{}}
			result[snap.Host.ID] = v
		}
		online := s.live[ch] != nil
		v["online"] = v["online"].(bool) || online
		v["channels"] = append(v["channels"].([]any), map[string]any{"id": ch, "online": online})
	}
	return result
}

func (s *Server) appViewsLocked(host, channel string) []map[string]any {
	result := make([]map[string]any, 0)
	for _, ch := range keys(s.snapshots) {
		snap := s.snapshots[ch]
		if (host != "" && host != snap.Host.ID) || (channel != "" && channel != ch) {
			continue
		}
		for _, app := range snap.Apps {
			live := s.live[ch] != nil && s.live[ch].registered
			result = append(result, map[string]any{"host_id": snap.Host.ID, "channel": ch, "app": app.App, "ready": app.Ready, "available": live && app.Ready, "stale": !live, "actions": app.Actions})
		}
	}
	return result
}

func (s *Server) actionViewsLocked(host, channel, appCode string) []map[string]any {
	result := make([]map[string]any, 0)
	for _, ch := range keys(s.snapshots) {
		snap := s.snapshots[ch]
		if (host != "" && host != snap.Host.ID) || (channel != "" && channel != ch) {
			continue
		}
		for _, app := range snap.Apps {
			if appCode != "" && app.App != appCode {
				continue
			}
			for _, a := range app.Actions {
				live := s.live[ch] != nil && s.live[ch].registered
				view := map[string]any{"host_id": snap.Host.ID, "channel": ch, "app": app.App, "action": a.Name, "description": a.Description, "input_schema": a.InputSchema, "ready": app.Ready && a.Ready, "available": live && app.Ready && a.Ready, "stale": !live}
				if a.ResultFormat != "" {
					view["result_format"] = a.ResultFormat
				}
				result = append(result, view)
			}
		}
	}
	return result
}
