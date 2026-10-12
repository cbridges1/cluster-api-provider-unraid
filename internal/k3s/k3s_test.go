package k3s

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/cbridges1/cluster-api-provider-unraid/api/v1alpha1"
)

func TestRunCommandMatchesTheHyveModule(t *testing.T) {
	spec := &v1alpha1.UnraidControlPlaneSpec{Version: "v1.34.8", ContainerName: "unraid-k3s", Disable: []string{"traefik"}, DockerSocket: true}
	got := RunCommand(spec, "192.168.50.100")
	for _, want := range []string{
		"docker run -d --name 'unraid-k3s' --restart=unless-stopped --privileged --cgroupns=host --network host --tmpfs /run --tmpfs /var/run",
		"-v /mnt/user:/mnt/user",
		"-v '/mnt/cache/appdata/unraid-k3s:/var/lib/rancher/k3s'",
		"-v '/mnt/cache/appdata/unraid-k3s-etc-rancher:/etc/rancher/k3s'",
		"-v /lib/modules:/lib/modules:ro -v /sys/fs/cgroup:/sys/fs/cgroup:rw",
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-e K3S_KUBECONFIG_MODE=644 'rancher/k3s:v1.34.8-k3s1' server --disable 'traefik' --write-kubeconfig-mode 644 --tls-san '192.168.50.100'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	off := false
	spec.MountShares, spec.DockerSocket = &off, false
	got = RunCommand(spec, "h")
	if strings.Contains(got, "/mnt/user") || strings.Contains(got, "docker.sock") {
		t.Errorf("mounts not turned off: %s", got)
	}
}

func TestImage(t *testing.T) {
	for in, want := range map[string]string{"v1.34.8": "rancher/k3s:v1.34.8-k3s1", "1.33.4": "rancher/k3s:v1.33.4-k3s1", "v1.34.8+k3s2": "rancher/k3s:v1.34.8-k3s2"} {
		if got := Image(&v1alpha1.UnraidControlPlaneSpec{Version: in}); got != want {
			t.Errorf("Image(%s) = %s, want %s", in, got, want)
		}
	}
	if got := Image(&v1alpha1.UnraidControlPlaneSpec{Version: "v1", Image: "my/k3s:x"}); got != "my/k3s:x" {
		t.Errorf("override ignored: %s", got)
	}
}

func TestQuoteSurvivesSingleQuotes(t *testing.T) {
	if got := Quote("a'b"); got != `'a'"'"'b'` {
		t.Errorf("Quote = %s", got)
	}
}

func TestAllReady(t *testing.T) {
	if AllReady("") || AllReady("n1 NotReady") || !AllReady("n1 Ready\n") || AllReady("n1 Ready\nn2 NotReady") {
		t.Error("AllReady wrong")
	}
}

func TestKubeconfigRewrite(t *testing.T) {
	raw := `apiVersion: v1
clusters:
- cluster: {certificate-authority-data: Zm9v, server: "https://127.0.0.1:6443"}
  name: default
contexts:
- context: {cluster: default, user: default}
  name: default
current-context: default
kind: Config
users:
- name: default
  user: {client-certificate-data: YQ==, client-key-data: Yg==}
`
	out, err := Kubeconfig(raw, "192.168.50.100", "media")
	if err != nil {
		t.Fatal(err)
	}
	var kc map[string]any
	_ = yaml.Unmarshal(out, &kc)
	cl := kc["clusters"].([]any)[0].(map[string]any)
	if cl["name"] != "media" || cl["cluster"].(map[string]any)["server"] != "https://192.168.50.100:6443" || kc["current-context"] != "media" {
		t.Errorf("kubeconfig = %s", out)
	}
	if ctx := kc["contexts"].([]any)[0].(map[string]any)["context"].(map[string]any); ctx["cluster"] != "media" || ctx["user"] != "media" {
		t.Errorf("context = %v", ctx)
	}
}

func TestRegistriesYAML(t *testing.T) {
	out, _ := RegistriesYAML(&v1alpha1.UnraidControlPlaneSpec{RegistryMirrors: map[string][]string{"192.168.50.100:30002": {"http://192.168.50.100:30002"}}})
	if !strings.Contains(out, "mirrors:") || !strings.Contains(out, "endpoint:") || !strings.Contains(out, "http://192.168.50.100:30002") {
		t.Errorf("registries.yaml = %s", out)
	}
}
