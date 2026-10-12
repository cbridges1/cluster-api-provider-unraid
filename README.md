# cluster-api-provider-unraid

A [Cluster API](https://cluster-api.sigs.k8s.io) provider for k3s on an
[Unraid](https://unraid.net) server. Over SSH it runs one privileged,
host-network `rancher/k3s` container with the server's shares mounted in —
the same container [hyve-unraid-k3s-module](https://github.com/cbridges1/hyve-unraid-k3s-module)
creates — and publishes the cluster's kubeconfig for Cluster API. Works with
[Rancher Turtles](https://turtles.docs.rancher.com) and [herd](https://github.com/cbridges1/herd).

One cluster per Unraid server: the container uses the host network, so a
second k3s would collide on its ports. The provider refuses to start one
when port 6443 is already in use.

## Kinds (`infrastructure.cluster.x-k8s.io/v1alpha1`)

| Kind | What it is |
|---|---|
| `UnraidCluster` | The server: `host`, `sshPort`, `credentialsRef` (a Secret with `username` and `password` or `privateKey`), an optional pinned `hostKey`. Without one, the key seen first is recorded in `status.hostKey` and must match afterwards. |
| `UnraidControlPlane` | The k3s container: `version` (from the Cluster's topology → `rancher/k3s:<version>-k3s1`), `containerName`, `dataPath` (`/mnt/cache/appdata`), `mountShares` (`/mnt/user`), `dockerSocket`, `disable` (`traefik`), `extraArgs`, `registryMirrors`, `deletePolicy` (`Keep` the data, or `Delete` it). |

Both have `…Template` kinds for ClusterClasses; see
[templates/clusterclass-unraid.yaml](templates/clusterclass-unraid.yaml).
A version change replaces the container with the new image on the same data.

## Install

With Rancher Turtles:

```yaml
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: CAPIProvider
metadata: {name: unraid, namespace: capu-system}
spec:
  name: unraid
  type: infrastructure
  version: v0.1.0
  fetchConfig:
    url: https://github.com/cbridges1/cluster-api-provider-unraid/releases/v0.1.0/infrastructure-components.yaml
```

Then a Secret and a cluster (with herd):

```bash
kubectl -n capi-clusters create secret generic unraid-ssh --from-literal=username=root --from-literal=password=...
kubectl -n capi-clusters apply -f templates/clusterclass-unraid.yaml
herd cluster create media --class unraid --set host=192.168.50.20
```

## Develop

`go test ./...`; `hack/components.py IMAGE` writes the release artifacts to `out/`.
`test/fake-unraid` is a stand-in server (Docker-in-Docker + password SSH) for
end-to-end runs; give its `/mnt/cache/appdata` a real volume (k3s's overlayfs
snapshotter can't sit on Docker's overlay).
