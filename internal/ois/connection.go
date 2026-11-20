package ois

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type connection struct {
	s          *Server
	ws         *websocket.Conn
	channel    string
	hostID     string
	writeMu    sync.Mutex
	closeOnce  sync.Once
	done       chan struct{}
	registered bool     // protected by Server.mu
	formats    []string // immutable after bind
}

func (c *connection) send(frame protocol.Frame) error {
	data, err := protocol.MarshalFrame(frame, false)
	if err != nil {
		return err
	}
	return c.sendMessage(data)
}

func (c *connection) sendMessage(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

func (c *connection) close() {
	c.closeOnce.Do(func() { close(c.done); _ = c.ws.Close() })
}

func readFrame(ws *websocket.Conn) (protocol.Frame, error) {
	var frame protocol.Frame
	typ, data, err := ws.ReadMessage()
	if err != nil {
		return frame, err
	}
	if typ != websocket.TextMessage {
		return frame, protocol.Err("INVALID_REQUEST", "expected a JSON text frame")
	}
	if err := protocol.ValidateJSON(data); err != nil {
		return frame, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	err = d.Decode(&frame)
	return frame, err
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	t := token(r)
	s.mu.RLock()
	allowed := false
	for _, c := range s.state.Channels {
		if matchesEnv(t, c.CredentialEnv) {
			allowed = true
			break
		}
	}
	s.mu.RUnlock()
	if !allowed {
		writeError(w, 401, "", protocol.Err("UNAUTHENTICATED", "valid OIC credentials are required"))
		return
	}
	up := websocket.Upgrader{CheckOrigin: s.validOrigin}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(protocol.MaxFrameBytes)
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	first, err := readFrame(ws)
	if err != nil {
		_ = ws.Close()
		return
	}
	c := &connection{s: s, ws: ws, channel: first.ChannelID, hostID: first.HostID, done: make(chan struct{})}
	reject := func(code, message string) {
		_ = c.send(protocol.Frame{Type: "error", Error: protocol.Err(code, message)})
		c.close()
	}
	if first.Type != "" && first.Type != "bind" && first.Type != "hello" {
		reject("INVALID_REQUEST", "first frame must bind the channel and host")
		return
	}
	if first.Protocol != 0 && first.Protocol != protocol.Version {
		reject("UNSUPPORTED_PROTOCOL", "unsupported OIC protocol")
		return
	}
	c.formats = protocol.NegotiateFormats(first.ResultFormats)
	if len(c.formats) == 0 {
		reject("RESULT_FORMAT_UNSUPPORTED", "no common result format")
		return
	}
	s.mu.Lock()
	config, exists := s.state.Channels[c.channel]
	if s.closed || !exists || !matchesEnv(t, config.CredentialEnv) || config.ExpectedHostID != c.hostID {
		s.mu.Unlock()
		reject("FORBIDDEN", "channel and Host do not match the credential binding")
		return
	}
	if s.live[c.channel] != nil {
		s.mu.Unlock()
		reject("CHANNEL_CONNECTED", "channel already has an active OIC")
		return
	}
	s.live[c.channel] = c
	if _, ok := s.snapshots[c.channel]; !ok {
		s.snapshots[c.channel] = snapshot{Host: protocol.HostInfo{ID: c.hostID}, Apps: []protocol.AppInfo{}}
	}
	s.mu.Unlock()
	defer func() {
		c.close()
		s.mu.Lock()
		if s.live[c.channel] == c {
			delete(s.live, c.channel)
		}
		for _, p := range s.pending {
			if p.conn == c {
				p.complete(outcome{err: protocol.Err("CALL_RESULT_UNKNOWN", "OIC connection was lost after the call was accepted")})
			}
		}
		s.mu.Unlock()
	}()
	bound := protocol.Frame{Type: "bound", Protocol: protocol.Version, HostID: c.hostID, ChannelID: c.channel}
	if first.ResultFormats != nil {
		bound.ResultFormats = c.formats
	}
	if err := c.send(bound); err != nil {
		return
	}
	go c.heartbeat()
	for {
		_ = ws.SetReadDeadline(time.Now().Add(duration(s.cfg.Server.HeartbeatTimeoutMS)))
		frame, err := readFrame(ws)
		if err != nil {
			return
		}
		switch frame.Type {
		case "ping":
			if err := c.send(protocol.Frame{Type: "pong"}); err != nil {
				return
			}
		case "pong":
		case "register":
			if err := c.register(frame); err != nil {
				reject("INVALID_REQUEST", err.Error())
				return
			}
			if err := c.send(protocol.Frame{Type: "registered"}); err != nil {
				return
			}
		case "result", "error":
			if frame.CallID == "" {
				reject("INVALID_REQUEST", "response has no call_id")
				return
			}
			s.mu.RLock()
			pending := s.pending[frame.CallID]
			current := s.live[c.channel] == c
			s.mu.RUnlock()
			if !current {
				return
			}
			if pending == nil || pending.conn != c {
				continue
			}
			if frame.Type == "error" {
				if frame.Error == nil {
					pending.complete(outcome{err: protocol.Err("EXECUTION_ERROR", "OIC returned an invalid error")})
				} else {
					pending.complete(outcome{err: frame.Error})
				}
			} else {
				pending.complete(pending.result(frame))
			}
		default:
			reject("INVALID_REQUEST", "unsupported OIC frame type")
			return
		}
	}
}

func (c *connection) register(frame protocol.Frame) error {
	host := protocol.HostInfo{ID: c.hostID}
	if frame.Host != nil {
		host = *frame.Host
		if host.ID == "" {
			host.ID = c.hostID
		}
	}
	if host.ID != c.hostID {
		return protocol.Err("FORBIDDEN", "Host registration does not match the authenticated binding")
	}
	apps := frame.Apps
	if apps == nil {
		apps = []protocol.AppInfo{}
	}
	seen := make(map[string]bool)
	for _, app := range apps {
		if !validID(app.App) || seen[app.App] {
			return protocol.Err("INVALID_REQUEST", "App names must be valid and unique within the channel")
		}
		seen[app.App] = true
		actions := make(map[string]bool)
		for index, action := range app.Actions {
			if !validID(action.Name) || actions[action.Name] || action.InputSchema == nil {
				return protocol.Err("INVALID_REQUEST", "Action registration requires a unique name and local schema")
			}
			actions[action.Name] = true
			if !protocol.SupportsFormat(c.formats, action.ResultFormat) {
				app.Actions[index].Ready = false
			}
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].App < apps[j].App })
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if c.s.live[c.channel] != c {
		return protocol.Err("FORBIDDEN", "connection is no longer active")
	}
	if frame.Host == nil {
		host = c.s.snapshots[c.channel].Host
	}
	c.s.snapshots[c.channel] = snapshot{Host: host, Apps: apps}
	c.registered = true
	return nil
}

func (c *connection) heartbeat() {
	ticker := time.NewTicker(duration(c.s.cfg.Server.HeartbeatIntervalMS))
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.send(protocol.Frame{Type: "ping"}); err != nil {
				c.close()
				return
			}
		}
	}
}
