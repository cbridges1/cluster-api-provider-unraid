// Package k3s builds the shell commands that run a k3s server as a Docker
// container on an Unraid server — the same container hyve-unraid-k3s-module
// creates.
package k3s

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/cbridges1/cluster-api-provider-unraid/api/v1alpha1"
)

// APIPort is k3s's API server port (host network, so on the server's IP).
const APIPort = 6443

// Quote single-quotes s for a POSIX shell.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Image returns the k3s image for a control plane.
func Image(spec *v1alpha1.UnraidControlPlaneSpec) string {
	if spec.Image != "" {
		return spec.Image
	}
	v := spec.Version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !strings.Contains(v, "k3s") {
		v += "-k3s1"
	}
	return "rancher/k3s:" + strings.ReplaceAll(v, "+", "-")
}

// Paths returns the server-side state and config directories.
func Paths(spec *v1alpha1.UnraidControlPlaneSpec) (data, etc string) {
	base := strings.TrimRight(spec.DataPath, "/")
	if base == "" {
		base = "/mnt/cache/appdata"
	}
	name := ContainerName(spec)
	return base + "/" + name, base + "/" + name + "-etc-rancher"
}

// ContainerName returns the container's name.
func ContainerName(spec *v1alpha1.UnraidControlPlaneSpec) string {
	if spec.ContainerName != "" {
		return spec.ContainerName
	}
	return "k3s"
}

// RegistriesYAML renders k3s's registries.yaml, or "" when there are no
// mirrors.
func RegistriesYAML(spec *v1alpha1.UnraidControlPlaneSpec) (string, error) {
	if len(spec.RegistryMirrors) == 0 {
		return "", nil
	}
	mirrors := map[string]any{}
	for reg, eps := range spec.RegistryMirrors {
		mirrors[reg] = map[string]any{"endpoint": eps}
	}
	b, err := yaml.Marshal(map[string]any{"mirrors": mirrors})
	return string(b), err
}

// RunCommand is the `docker run` for the k3s server.
func RunCommand(spec *v1alpha1.UnraidControlPlaneSpec, host string) string {
	data, etc := Paths(spec)
	args := []string{
		"docker", "run", "-d",
		"--name", Quote(ContainerName(spec)),
		"--restart=unless-stopped",
		"--privileged",
		"--cgroupns=host",
		"--network", "host",
		"--tmpfs", "/run",
		"--tmpfs", "/var/run",
		"--label", "io.cluster-api.unraid/managed=true",
	}
	if spec.MountShares == nil || *spec.MountShares {
		args = append(args, "-v", "/mnt/user:/mnt/user")
	}
	args = append(args,
		"-v", Quote(data+":/var/lib/rancher/k3s"),
		"-v", Quote(etc+":/etc/rancher/k3s"),
		"-v", "/lib/modules:/lib/modules:ro",
		"-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw",
	)
	if spec.DockerSocket {
		args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock")
	}
	args = append(args, "-e", "K3S_KUBECONFIG_MODE=644", Quote(Image(spec)), "server")
	disable := append([]string(nil), spec.Disable...)
	sort.Strings(disable)
	for _, d := range disable {
		args = append(args, "--disable", Quote(d))
	}
	args = append(args, "--write-kubeconfig-mode", "644", "--tls-san", Quote(host))
	for _, a := range spec.ExtraArgs {
		args = append(args, Quote(a))
	}
	return strings.Join(args, " ")
}

// CleanStaleCgroups removes the empty /kubepods cgroups a previous k3s
// container leaves behind; a new kubelet hangs on them otherwise (seen on
// unraid-k3s recreates).
const CleanStaleCgroups = `if [ -d /sys/fs/cgroup/kubepods ]; then find /sys/fs/cgroup/kubepods -mindepth 1 -depth -type d -exec rmdir {} \; 2>/dev/null; fi; true`

// InspectCommand prints the container's image and state, or nothing when
// it doesn't exist.
func InspectCommand(name string) string {
	return fmt.Sprintf("docker inspect --format '{{.Config.Image}} {{.State.Status}}' %s 2>/dev/null || true", Quote(name))
}

// PortBusyCommand exits 0 when something listens on the API port.
const PortBusyCommand = "(command -v ss >/dev/null && ss -ltn | grep -q ':6443 ') || (netstat -ltn 2>/dev/null | grep -q ':6443 ')"

// NodesCommand lists the cluster's nodes ("name status" per line).
func NodesCommand(name string) string {
	return fmt.Sprintf("docker exec %s kubectl get nodes --no-headers 2>/dev/null | awk '{print $1, $2}'", Quote(name))
}

// KubeconfigCommand prints the cluster's admin kubeconfig.
func KubeconfigCommand(name string) string {
	return fmt.Sprintf("docker exec %s cat /etc/rancher/k3s/k3s.yaml", Quote(name))
}

// AllReady reports whether every node line is "<name> Ready".
func AllReady(nodes string) bool {
	any := false
	for _, l := range strings.Split(strings.TrimSpace(nodes), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		any = true
		if f[1] != "Ready" {
			return false
		}
	}
	return any
}

// Kubeconfig rewrites k3s's kubeconfig (server 127.0.0.1) to the server's
// address, with context, cluster and user named after the cluster.
func Kubeconfig(raw, host, cluster string) ([]byte, error) {
	var kc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &kc); err != nil {
		return nil, err
	}
	rename := func(list string, field string) {
		items, _ := kc[list].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				m["name"] = cluster
				if inner, ok := m[field].(map[string]any); ok && list == "clusters" {
					inner["server"] = fmt.Sprintf("https://%s:%d", host, APIPort)
				}
				if inner, ok := m[field].(map[string]any); ok && list == "contexts" {
					inner["cluster"], inner["user"] = cluster, cluster
				}
			}
		}
	}
	rename("clusters", "cluster")
	rename("users", "user")
	rename("contexts", "context")
	kc["current-context"] = cluster
	return yaml.Marshal(kc)
}
