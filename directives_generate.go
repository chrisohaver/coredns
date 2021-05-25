//+build ignore

package main

import (
	"bufio"
	"go/format"
	yaml "gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

func main() {
	mi := make(map[string]string, 0)
	md := []string{}

	// get external plugins
	extPlugInserts := externalPlugins()

	// process plugin.cfg
	file, err := os.Open(pluginFile)
	if err != nil {
		log.Fatalf("Failed to open %s: %q", pluginFile, err)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}

		items := strings.Split(line, ":")
		if len(items) != 2 {
			// ignore empty lines
			continue
		}
		name, repo := items[0], items[1]

		if insert, ok := extPlugInserts[name]; ok {
			if insert.after {
				mi, md = addPlugin(mi, md, name, repo)
				mi, md = addPlugin(mi, md, insert.name, insert.repo)
				continue
			}
			mi, md = addPlugin(mi, md, insert.name, insert.repo)
		}
		mi, md = addPlugin(mi, md, name, repo)
	}

	genImports("core/plugin/zplugin.go", "plugin", mi)
	genDirectives("core/dnsserver/zdirectives.go", "dnsserver", md)
}

func addPlugin(mi map[string]string, md []string, name, repo string) (map[string]string, []string) {
	if _, ok := mi[name]; ok {
		log.Fatalf("Duplicate entry %q", name)
	}

	md = append(md, name)
	mi[name] = pluginPath + repo // Default, unless overridden by 3rd arg

	if _, err := os.Stat(pluginFSPath + repo); err != nil { // External package has been given
		mi[name] = repo
	}
	return mi, md
}

func genImports(file, pack string, mi map[string]string) {
	outs := header + "package " + pack + "\n\n" + "import ("

	if len(mi) > 0 {
		outs += "\n"
	}

	outs += "// Include all plugins.\n"
	for _, v := range mi {
		outs += `_ "` + v + `"` + "\n"
	}
	outs += ")\n"

	if err := formatAndWrite(file, outs); err != nil {
		log.Fatalf("Failed to format and write: %q", err)
	}
}

func genDirectives(file, pack string, md []string) {

	outs := header + "package " + pack + "\n\n"
	outs += `
// Directives are registered in the order they should be
// executed.
//
// Ordering is VERY important. Every plugin will
// feel the effects of all other plugin below
// (after) them during a request, but they must not
// care what plugin above them are doing.
var Directives = []string{
`

	for i := range md {
		outs += `"` + md[i] + `",` + "\n"
	}

	outs += "}\n"

	if err := formatAndWrite(file, outs); err != nil {
		log.Fatalf("Failed to format and write: %q", err)
	}
}

func formatAndWrite(file string, data string) error {
	res, err := format.Source([]byte(data))
	if err != nil {
		return err
	}

	if err = ioutil.WriteFile(file, res, 0644); err != nil {
		return err
	}
	return nil
}
// externalPlugins extracts the plugin chain placement def (plugin.cfg.yaml) from each external plugin listed in the
// COREDNS_ADD_PLUGINS environment variable, and returns a map to be used to insert the plugins into Directives in
// the correct order.
func externalPlugins() map[string]extPlugInsert {
	extPlugInserts := make(map[string]extPlugInsert)
	env := os.Getenv(envAddPlugins)
	xplugs := strings.Split(env, " ")
	gopath := os.Getenv("GOPATH")

	for _, xplug := range xplugs {
		if xplug == "" {
			continue
		}
		xplugParts := strings.Split(xplug, "/")
		if len(xplugParts) < 3 {
			log.Fatalf("Invalid external plugin repo '%s'", xplug)
		}
		repo := strings.Join(xplugParts[0:3], "/")
		moduleName := ""
		if len(xplugParts) > 3 {
			moduleName = "/" + strings.Join(xplugParts[3:], "/")
		}
		// go get the package
		cmd := exec.Command("go", "get", repo)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("Could not go get '%s': %s %q", repo, out, err)
		}
		// get the cached module version of the package (may not be same as tag used to get it)
		cmd = exec.Command("go", "list", "-m", repo)
		out, err = cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("Could not go list '%s': %s %q", repo, out, err)
		}
		modVer := strings.TrimSpace(string(out))
		// get the plugin.cfg.yaml from the module cache
		goMod := strings.Replace(modVer, " ", "@", 1)
		yamlPath := gopath + "/pkg/mod/" + goMod + moduleName + "/" + pluginCfgYaml
		println(modVer)
		if parts := strings.Split(goMod, " "); len(parts) > 2 {
			// use local replace path instead
			yamlPath = parts[2]+ "/" + pluginCfgYaml
		}
		yamlBytes, err := ioutil.ReadFile(yamlPath)
		if err != nil {
			log.Fatalf("Could not read file '%s': %q", yamlPath, err)
		}
		// unmarshal the yaml and add the repo to the external plugin map
		var cfg PluginCfg
		err = yaml.Unmarshal(yamlBytes, &cfg)
		if err != nil {
			log.Fatalf("Invalid plugin.cfg.yaml from %s: %q", repo, err)
		}
		repoName := strings.Split(xplug, "@")[0]
		extPlugInserts[cfg.Landmark] = extPlugInsert{after: cfg.After, name: cfg.Name, repo: repoName}
	}
	return extPlugInserts
}

type extPlugInsert struct {
	after      bool
	name, repo string
}

type PluginCfg struct {
	Name     string
	After    bool
	Landmark string
}

const (
	pluginPath    = "github.com/coredns/coredns/plugin/"
	pluginFile    = "plugin.cfg"
	pluginFSPath  = "plugin/" // Where the plugins are located on the file system
	header        = "// generated by directives_generate.go; DO NOT EDIT\n\n"
	envAddPlugins = "COREDNS_ADD_PLUGINS"
	//pluginCfgYaml = "/master/plugin.cfg.yaml" // assume master branch
	pluginCfgYaml = "plugin.cfg.yaml"
)
