// channelprobe exercises authenticated OIC connections against a real OIS.
// It creates only uniquely named temporary resources and never starts commands.
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type probe struct {
	base                                      string
	client                                    *http.Client
	dialer                                    websocket.Dialer
	admin, consumer, channelToken, wrongToken string
	id                                        string
	conns                                     []*websocket.Conn
	evidence                                  []map[string]any
}
type response struct {
	Status int
	Body   map[string]any
	Err    error
}

func main() {
	base := flag.String("url", "https://121.199.175.151:25100", "OIS HTTPS base URL")
	ca := flag.String("ca", "", "trusted CA PEM file")
	credentials := flag.String("credentials", "", "local credentials JSON file (tokens are never printed)")
	flag.Parse()
	if err := run(*base, *ca, *credentials); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(base, caPath, credentialsPath string) error {
	if !strings.HasPrefix(base, "https://") {
		return errors.New("probe requires HTTPS")
	}
	certs, err := os.ReadFile(caPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certs) {
		return errors.New("invalid CA PEM")
	}
	b, err := os.ReadFile(credentialsPath)
	if err != nil {
		return err
	}
	var creds map[string]string
	if err = json.Unmarshal(b, &creds); err != nil {
		return err
	}
	for _, k := range []string{"OIS_ADMIN_TOKEN", "WORKBENCH_TOKEN", "LINUX_CHANNEL_TOKEN", "WINDOWS_CHANNEL_TOKEN"} {
		if creds[k] == "" {
			return fmt.Errorf("missing credential reference %s", k)
		}
	}
	tlsConfig := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	p := &probe{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}, dialer: websocket.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: 10 * time.Second}, admin: creds["OIS_ADMIN_TOKEN"], consumer: creds["WORKBENCH_TOKEN"], channelToken: creds["LINUX_CHANNEL_TOKEN"], wrongToken: creds["WINDOWS_CHANNEL_TOKEN"], id: protocol.ID("probe_")}
	defer p.client.CloseIdleConnections()
	defer p.cleanup()
	if err := p.exercise(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"case": "A13", "status": "PASS", "probe": p.id, "evidence": p.evidence})
}

func (p *probe) http(method, path, token string, body any) response {
	b, err := json.Marshal(body)
	if err != nil {
		return response{Err: err}
	}
	r, err := http.NewRequest(method, p.base+path, bytes.NewReader(b))
	if err != nil {
		return response{Err: err}
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	res, err := p.client.Do(r)
	if err != nil {
		return response{Err: err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, protocol.MaxFrameBytes))
	if err != nil {
		return response{Err: err}
	}
	var value map[string]any
	if len(data) > 0 {
		if err = json.Unmarshal(data, &value); err != nil {
			return response{Err: err}
		}
	}
	return response{Status: res.StatusCode, Body: value}
}
func check(r response, status int, code string) error {
	if r.Err != nil {
		return r.Err
	}
	if r.Status != status {
		return fmt.Errorf("HTTP status %d; expected %d (body %v)", r.Status, status, r.Body)
	}
	if code != "" {
		e, _ := r.Body["error"].(map[string]any)
		if e["code"] != code {
			return fmt.Errorf("expected error %s; received %v", code, r.Body)
		}
	}
	return nil
}
func (p *probe) record(step string, detail any) {
	p.evidence = append(p.evidence, map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "step": step, "detail": detail})
}
func (p *probe) channelBody(host string) map[string]any {
	return map[string]any{"description": "temporary OIC acceptance probe", "expected_host_id": host, "credential_env": "LINUX_CHANNEL_TOKEN"}
}
func (p *probe) vmBody(tools []string) map[string]any {
	return map[string]any{"tools": tools, "access": map[string]any{"credential_env": "WORKBENCH_TOKEN"}}
}
func (p *probe) actionPath() string { return "/api/v1/channels/" + p.id + "/apps/probe/actions/echo" }
func (p *probe) exec() <-chan response {
	done := make(chan response, 1)
	go func() {
		done <- p.http("POST", "/api/v1/exec", p.consumer, map[string]any{"vmcp": p.id, "tool": p.id, "arguments": map[string]any{"marker": p.id}})
	}()
	return done
}

func (p *probe) dial(token, host string) (*websocket.Conn, protocol.Frame, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	c, res, err := p.dialer.Dial("wss"+strings.TrimPrefix(p.base, "https")+"/api/v1/oic/connect", h)
	if err != nil {
		if res != nil {
			res.Body.Close()
		}
		return nil, protocol.Frame{}, err
	}
	p.conns = append(p.conns, c)
	if err = c.WriteJSON(protocol.Frame{Type: "bind", Protocol: protocol.Version, HostID: host, ChannelID: p.id}); err != nil {
		return c, protocol.Frame{}, err
	}
	frame, err := read(c)
	return c, frame, err
}
func read(c *websocket.Conn) (protocol.Frame, error) {
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		var f protocol.Frame
		err := c.ReadJSON(&f)
		if err != nil {
			return f, err
		}
		if f.Type == "ping" {
			if err = c.WriteJSON(protocol.Frame{Type: "pong"}); err != nil {
				return f, err
			}
			continue
		}
		return f, nil
	}
}
func (p *probe) register(c *websocket.Conn, description string) error {
	f := protocol.Frame{Type: "register", Host: &protocol.HostInfo{ID: p.id, Name: "acceptance probe", OS: "probe", Arch: "probe"}, Apps: []protocol.AppInfo{{App: "probe", Ready: true, Actions: []protocol.ActionInfo{{Name: "echo", Ready: true, Description: description, InputSchema: map[string]any{"type": "object"}}}}}}
	if err := c.WriteJSON(f); err != nil {
		return err
	}
	ack, err := read(c)
	if err != nil {
		return err
	}
	if ack.Type != "registered" {
		return fmt.Errorf("registration rejected: %+v", ack)
	}
	return nil
}

func (p *probe) exercise() error {
	if err := check(p.http("PUT", "/api/v1/channels/"+p.id, p.admin, p.channelBody(p.id)), 201, ""); err != nil {
		return err
	}
	tool := map[string]any{"description": "temporary probe", "input_schema": map[string]any{"type": "object"}, "source": map[string]any{"channel": p.id, "app": "probe", "action": "echo"}}
	if err := check(p.http("PUT", "/api/v1/tools/"+p.id, p.admin, tool), 201, ""); err != nil {
		return err
	}
	if err := check(p.http("PUT", "/api/v1/vmcps/"+p.id, p.admin, p.vmBody([]string{p.id})), 201, ""); err != nil {
		return err
	}
	// Authentication is checked at the HTTP handshake, before any bind frame.
	h := http.Header{}
	h.Set("Authorization", "Bearer invalid-probe-credential")
	c, res, err := p.dialer.Dial("wss"+strings.TrimPrefix(p.base, "https")+"/api/v1/oic/connect", h)
	if c != nil {
		c.Close()
	}
	if err == nil || res == nil || res.StatusCode != 401 {
		return errors.New("invalid credential was not rejected at handshake")
	}
	res.Body.Close()
	p.record("invalid_credential", 401)
	for _, tc := range []struct{ name, token, host string }{{"wrong_host", p.channelToken, p.id + "_wrong"}, {"wrong_channel_credential", p.wrongToken, p.id}} {
		conn, frame, err := p.dial(tc.token, tc.host)
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			return err
		}
		if frame.Type != "error" || frame.Error == nil || frame.Error.Code != "FORBIDDEN" {
			return fmt.Errorf("%s unexpectedly accepted", tc.name)
		}
		p.record(tc.name, frame.Error.Code)
	}
	active, frame, err := p.dial(p.channelToken, p.id)
	if err != nil {
		return err
	}
	if frame.Type != "bound" {
		return fmt.Errorf("valid binding rejected: %+v", frame)
	}
	if err = p.register(active, "old generation"); err != nil {
		return err
	}
	r := p.http("GET", p.actionPath(), p.admin, nil)
	if err = check(r, 200, ""); err != nil {
		return err
	}
	if r.Body["available"] != true {
		return errors.New("registered Action is not available")
	}
	p.record("valid_identity_and_registration", r.Body)
	dup, frame, err := p.dial(p.channelToken, p.id)
	if dup != nil {
		dup.Close()
	}
	if err != nil {
		return err
	}
	if frame.Error == nil || frame.Error.Code != "CHANNEL_CONNECTED" {
		return errors.New("duplicate active connection was accepted")
	}
	p.record("duplicate_active", frame.Error.Code)
	r = p.http("PUT", "/api/v1/channels/"+p.id, p.admin, p.channelBody(p.id+"_wrong"))
	if err = check(r, 409, "CHANNEL_CONNECTED"); err != nil {
		return err
	}
	p.record("active_identity_change", 409)
	// Use the probe's private vMCP to verify configuration acceptance snapshots.
	pending := p.exec()
	call, err := read(active)
	if err != nil {
		return err
	}
	if call.Type != "invoke" {
		return errors.New("expected invocation")
	}
	if err = check(p.http("PUT", "/api/v1/vmcps/"+p.id, p.admin, p.vmBody([]string{})), 200, ""); err != nil {
		return err
	}
	if err = check(<-p.exec(), 404, "NOT_FOUND"); err != nil {
		return err
	}
	if err = active.WriteJSON(protocol.ResultFrame(call.CallID, map[string]any{"output": "accepted before configuration change", "exit_code": 0})); err != nil {
		return err
	}
	r = <-pending
	if err = check(r, 200, ""); err != nil {
		return err
	}
	p.record("A12_inflight_configuration_snapshot", r.Body)
	if err = check(p.http("PUT", "/api/v1/vmcps/"+p.id, p.admin, p.vmBody([]string{p.id})), 200, ""); err != nil {
		return err
	}
	// Once delivered, losing the only connection leaves an unknown outcome.
	pending = p.exec()
	lost, err := read(active)
	if err != nil {
		return err
	}
	if lost.Type != "invoke" {
		return errors.New("expected invocation before disconnect")
	}
	active.Close()
	r = <-pending
	if err = check(r, 504, "CALL_RESULT_UNKNOWN"); err != nil {
		return err
	}
	p.record("delivered_disconnect_unknown", r.Body)
	r = p.http("GET", p.actionPath(), p.admin, nil)
	if err = check(r, 200, ""); err != nil {
		return err
	}
	if r.Body["available"] != false || r.Body["stale"] != true {
		return errors.New("disconnected snapshot was still executable")
	}
	p.record("stale_snapshot", r.Body)
	if err = check(<-p.exec(), 503, "CHANNEL_UNAVAILABLE"); err != nil {
		return err
	}
	replacement, frame, err := p.dial(p.channelToken, p.id)
	if err != nil {
		return err
	}
	if frame.Type != "bound" {
		return errors.New("replacement connection did not bind")
	}
	if err = p.register(replacement, "new generation"); err != nil {
		return err
	}
	pending = p.exec()
	fresh, err := read(replacement)
	if err != nil {
		return err
	}
	if fresh.Type != "invoke" || fresh.CallID == lost.CallID {
		return errors.New("old call was automatically replayed")
	}
	// A delayed response from the lost generation, even carried by the new
	// authenticated connection, cannot complete its different pending request.
	if err = replacement.WriteJSON(protocol.ResultFrame(lost.CallID, map[string]any{"output": "obsolete", "exit_code": 99})); err != nil {
		return err
	}
	if err = replacement.WriteJSON(protocol.ResultFrame(fresh.CallID, map[string]any{"output": "fresh", "exit_code": 0})); err != nil {
		return err
	}
	r = <-pending
	if err = check(r, 200, ""); err != nil {
		return err
	}
	result, _ := r.Body["result"].(map[string]any)
	if result["output"] != "fresh" {
		return errors.New("late old response replaced current result")
	}
	p.record("late_response_ignored_no_replay", r.Body)
	oldWriteErr := active.WriteJSON(protocol.Frame{Type: "register", Apps: []protocol.AppInfo{}})
	if oldWriteErr == nil {
		return errors.New("closed connection unexpectedly accepted a write")
	}
	r = p.http("GET", p.actionPath(), p.admin, nil)
	if err = check(r, 200, ""); err != nil {
		return err
	}
	if r.Body["description"] != "new generation" || r.Body["available"] != true {
		return errors.New("old connection replaced the new registration")
	}
	p.record("new_registration_survived_old_connection", r.Body)
	replacement.Close()
	return nil
}

func (p *probe) cleanup() {
	for _, c := range p.conns {
		c.Close()
	}
	// These names are generated by this process; no deployment resource is used.
	for _, kind := range []string{"vmcps", "tools"} {
		r := p.http("DELETE", "/api/v1/"+kind+"/"+p.id, p.admin, nil)
		if r.Err != nil || (r.Status != 204 && r.Status != 404) {
			fmt.Fprintf(os.Stderr, "probe cleanup %s failed\n", kind)
		}
	}
	for i := 0; i < 40; i++ {
		r := p.http("DELETE", "/api/v1/channels/"+p.id, p.admin, nil)
		if r.Status == 204 || r.Status == 404 {
			return
		}
		if r.Err != nil || r.Status != 409 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "probe cleanup channel failed; inspect the probe ID in the report")
}
