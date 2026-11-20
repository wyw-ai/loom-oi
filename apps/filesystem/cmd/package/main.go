// Command package builds a self-contained distribution from the module directory.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type obj = map[string]any

func expand(v any, defs obj) any {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			return expand(defs[strings.TrimPrefix(ref, "#/$defs/")], defs)
		}
		out := obj{}
		for k, val := range x {
			out[k] = expand(val, defs)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = expand(v, defs)
		}
		return out
	default:
		return v
	}
}
func configs(command string) (obj, obj, error) {
	var schemas obj
	data, err := os.ReadFile("contract/schemas.json")
	if err != nil {
		return nil, nil, err
	}
	if err = json.Unmarshal(data, &schemas); err != nil {
		return nil, nil, err
	}
	var actions []map[string]string
	data, err = os.ReadFile("contract/actions.json")
	if err != nil {
		return nil, nil, err
	}
	if err = json.Unmarshal(data, &actions); err != nil {
		return nil, nil, err
	}
	defs := schemas["$defs"].(map[string]any)
	manifestActions := obj{}
	tools := obj{}
	for _, action := range actions {
		name := action["name"]
		schema := expand(defs[name+"_input"], defs)
		meta := obj{"description": action["description"], "input_schema": schema}
		if format := action["result_format"]; format != "" {
			meta["result_format"] = format
		}
		manifestActions[name] = meta
		tools["filesystem_"+name] = obj{"description": action["description"], "input_schema": schema, "source": obj{"channel": "local-workstation", "app": "filesystem", "action": name}}
	}
	return obj{"version": 1, "app": obj{"code": "filesystem"}, "runtime": obj{"command": []string{command}}, "actions": manifestActions}, obj{"tools": tools}, nil
}
func writeYAML(name string, value any) error {
	b, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(name, b, 0644)
}
func copyFile(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, 0644)
}
func runGo(target, arch string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0", "GOOS="+target, "GOARCH="+arch)
	cmd.Stderr = os.Stderr
	b, err := cmd.Output()
	return string(b), err
}
func build(target, arch, out string) error {
	if !slices.Contains([]string{"linux/amd64", "linux/arm64", "windows/amd64"}, target+"/"+arch) {
		return fmt.Errorf("unsupported target %s/%s", target, arch)
	}
	for _, sub := range []string{"runtime", "examples", "LICENSES"} {
		if err := os.MkdirAll(filepath.Join(out, sub), 0755); err != nil {
			return err
		}
	}
	binary := "oiapp-filesystem"
	if target == "windows" {
		binary += ".exe"
	}
	manifest, tools, err := configs("./runtime/" + binary)
	if err != nil {
		return err
	}
	if err = writeYAML(filepath.Join(out, "app.yaml"), manifest); err != nil {
		return err
	}
	if err = writeYAML(filepath.Join(out, "examples", "ois.tools.yaml"), tools); err != nil {
		return err
	}
	if err = copyFile("examples/oic."+target+".yaml", filepath.Join(out, "examples", "oic."+target+".yaml")); err != nil {
		return err
	}
	if err = copyFile("README.md", filepath.Join(out, "README.md")); err != nil {
		return err
	}
	if _, err = runGo(target, arch, "build", "-trimpath", "-o", filepath.Join(out, "runtime", binary), "./cmd/worker"); err != nil {
		return err
	}
	if err = copyFile(filepath.Join(runtime.GOROOT(), "LICENSE"), filepath.Join(out, "LICENSES", "Go-LICENSE")); err != nil {
		return err
	}
	list, err := runGo(target, arch, "list", "-deps", "-f", "{{if and .Module (not .Module.Main)}}{{.Module.Path}}@{{.Module.Version}}|{{.Module.Dir}}{{end}}", "./cmd/worker")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var modules []string
	for line := range strings.SplitSeq(list, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(fields) != 2 || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		modules = append(modules, fields[0])
		entries, err := os.ReadDir(fields[1])
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := strings.ToUpper(e.Name())
			if !e.IsDir() && (strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "COPYING") || strings.HasPrefix(name, "NOTICE")) {
				if err := copyFile(filepath.Join(fields[1], e.Name()), filepath.Join(out, "LICENSES", strings.ReplaceAll(fields[0], "/", "_")+"-"+e.Name())); err != nil {
					return err
				}
			}
		}
	}
	sort.Strings(modules)
	if err = os.WriteFile(filepath.Join(out, "modules.txt"), []byte(strings.Join(modules, "\n")+"\n"), 0644); err != nil {
		return err
	}
	var sums []string
	err = filepath.WalkDir(out, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || name == filepath.Join(out, "checksums.txt") {
			return nil
		}
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(out, name)
		sums = append(sums, fmt.Sprintf("%x  %s", sha256.Sum256(b), filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(sums)
	return os.WriteFile(filepath.Join(out, "checksums.txt"), []byte(strings.Join(sums, "\n")+"\n"), 0644)
}
func main() {
	target := flag.String("os", runtime.GOOS, "target operating system")
	arch := flag.String("arch", runtime.GOARCH, "target architecture")
	out := flag.String("out", "", "output directory")
	flag.Parse()
	if *out == "" {
		*out = filepath.Join("dist", "filesystem-"+*target+"-"+*arch)
	}
	if err := build(*target, *arch, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
